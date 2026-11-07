// Package application hosts plan-change use cases: self-service request,
// cancel, lists, and the scheduler's due-downgrade applier.
package application

import (
	"context"
	"fmt"
	"time"

	planchangesdomain "boilerplate/api/internal/planchanges/domain"
	"boilerplate/api/internal/shared/id"
)

// dueDaysAfterIssue is the prorata invoice due offset.
const dueDaysAfterIssue = 7

// duePageSize bounds one ListDue page in ApplyDue.
const duePageSize = 200

// Deps wires a Service. Disconnect may be nil (no live sessions to drop
// in tests); every other field is required.
type Deps struct {
	Accounts planchangesdomain.AccountStore
	Plans    planchangesdomain.PlanStore
	Billing  planchangesdomain.Billing
	Disco    planchangesdomain.Disconnecter
}

// Service implements plan-change use cases.
type Service struct {
	deps Deps
	now  func() time.Time
}

// NewService builds a Service.
func NewService(d Deps) *Service {
	return &Service{deps: d, now: time.Now}
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

// Result is the outcome of a Request: the recorded change plus the
// prorata invoice of immediate upgrades (nil otherwise).
type Result struct {
	Change           *planchangesdomain.PlanChange
	ProrataInvoiceID *string
}

// Request records a self-service package move (PRD §6.5 rules):
//
//   - target must be allow_self_service, in the same upgrade_group, and
//     different from the current package;
//   - blocked (409) when the account has an unpaid invoice;
//   - at most one pending+applied change per UTC calendar month
//     (frequency keys on the request month, i.e. created_at);
//   - upgrade (target price >= current, equal counts as upgrade with no
//     charge) applies immediately: the package switches (pppoe/upsert),
//     a rounded-rupiah prorata invoice covers the remaining days of the
//     month, and live sessions disconnect best-effort;
//   - downgrade (target price lower) stays pending until the first of
//     next month UTC, applied by the scheduler job.
func (s *Service) Request(ctx context.Context, customerID, accountID, toPackageID string) (Result, error) {
	acct, err := s.deps.Accounts.GetAccount(ctx, accountID)
	if err != nil {
		return Result{}, err
	}
	if customerID != "" && acct.CustomerID != customerID {
		return Result{}, planchangesdomain.ErrAccountNotFound
	}
	to, err := s.deps.Accounts.GetPackage(ctx, toPackageID)
	if err != nil {
		return Result{}, err
	}
	from, err := s.deps.Accounts.GetPackage(ctx, acct.PackageID)
	if err != nil {
		return Result{}, err
	}
	if to.ID == from.ID {
		return Result{}, planchangesdomain.ErrSamePackage
	}
	if !to.AllowSelfService {
		return Result{}, planchangesdomain.ErrNotSelfService
	}
	if to.UpgradeGroup != from.UpgradeGroup {
		return Result{}, planchangesdomain.ErrGroupMismatch
	}
	if has, err := s.deps.Accounts.HasUnpaid(ctx, accountID); err != nil {
		return Result{}, err
	} else if has {
		return Result{}, planchangesdomain.ErrHasUnpaid
	}
	now := s.now().UTC()
	since := planchangesdomain.MonthStart(now).Format(time.RFC3339)
	n, err := s.deps.Plans.CountActiveSince(ctx, accountID, since)
	if err != nil {
		return Result{}, err
	}
	if n >= 1 {
		return Result{}, planchangesdomain.ErrFrequencyExceeded
	}

	if to.Price >= from.Price {
		return s.applyUpgrade(ctx, acct, from, to, now)
	}
	return s.queueDowngrade(ctx, acct, from, to, now)
}

func (s *Service) applyUpgrade(ctx context.Context, acct *planchangesdomain.Account, from, to *planchangesdomain.Package, now time.Time) (Result, error) {
	days, remaining := planchangesdomain.MonthBounds(now)
	var prorataID *string
	if diff := to.Price - from.Price; diff > 0 {
		if amount := planchangesdomain.Prorata(diff, remaining, days); amount > 0 {
			due := now.AddDate(0, 0, dueDaysAfterIssue).Format("2006-01-02")
			inv, err := s.deps.Billing.Create(ctx, acct.ID, []planchangesdomain.ItemInput{
				{
					Description: fmt.Sprintf("Selisih upgrade paket %s ke %s (prorata %d/%d hari)", from.Name, to.Name, remaining, days),
					Qty:         1,
					Price:       amount,
				},
			}, due)
			if err != nil {
				return Result{}, err
			}
			prorataID = &inv.ID
		}
	}
	if err := s.deps.Accounts.SetPackage(ctx, acct.ID, to.ID, now); err != nil {
		return Result{}, err
	}
	s.disconnectBestEffort(ctx, acct.ID)
	change, err := planchangesdomain.NewPlanChange(
		id.New(), acct.ID, from.ID, to.ID, now, planchangesdomain.StatusApplied, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.deps.Plans.Insert(ctx, change); err != nil {
		return Result{}, err
	}
	return Result{Change: change, ProrataInvoiceID: prorataID}, nil
}

func (s *Service) queueDowngrade(ctx context.Context, acct *planchangesdomain.Account, from, to *planchangesdomain.Package, now time.Time) (Result, error) {
	effective := planchangesdomain.NextMonthStart(now)
	change, err := planchangesdomain.NewPlanChange(
		id.New(), acct.ID, from.ID, to.ID, effective, planchangesdomain.StatusPending, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.deps.Plans.Insert(ctx, change); err != nil {
		return Result{}, err
	}
	return Result{Change: change}, nil
}

// Cancel voids a pending change of one own account.
func (s *Service) Cancel(ctx context.Context, customerID, changeID string) (*planchangesdomain.PlanChange, error) {
	change, err := s.deps.Plans.Get(ctx, changeID)
	if err != nil {
		return nil, err
	}
	acct, err := s.deps.Accounts.GetAccount(ctx, change.PPPoEAccountID)
	if err != nil {
		return nil, err
	}
	if customerID != "" && acct.CustomerID != customerID {
		return nil, planchangesdomain.ErrChangeNotFound
	}
	if change.Status != planchangesdomain.StatusPending {
		return nil, planchangesdomain.ErrNotPending
	}
	return s.deps.Plans.SetStatus(ctx, changeID, planchangesdomain.StatusCancelled)
}

// ListMine returns the customer's own changes, newest first.
func (s *Service) ListMine(ctx context.Context, customerID string, limit, offset int) ([]*planchangesdomain.PlanChange, int64, error) {
	rows, err := s.deps.Plans.ListMine(ctx, customerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.deps.Plans.CountMine(ctx, customerID)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListAll returns every change, newest first (admin).
func (s *Service) ListAll(ctx context.Context, limit, offset int) ([]*planchangesdomain.PlanChange, int64, error) {
	rows, err := s.deps.Plans.ListAll(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.deps.Plans.CountAll(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ApplyDue applies every pending change whose effective_at passed: switch
// the package (pppoe/upsert), disconnect best-effort, mark applied. One
// bad row never stops the rest; the first error is returned with its row
// so the scheduler log can point at it.
func (s *Service) ApplyDue(ctx context.Context, now time.Time) (int, error) {
	at := now.UTC().Format(time.RFC3339)
	applied := 0
	var firstErr error
	for {
		due, err := s.deps.Plans.ListDue(ctx, at, duePageSize)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		if len(due) == 0 {
			break
		}
		before := applied
		for _, c := range due {
			if err := s.applyOne(ctx, c, now.UTC()); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("plan change %s: %w", c.ID, err)
				}
				continue
			}
			applied++
		}
		// Full pages re-query (applied rows drop out); stop on a short
		// page or when a pass made no progress (failing rows stay
		// pending for the next tick instead of looping forever).
		if len(due) < duePageSize || applied == before {
			break
		}
	}
	return applied, firstErr
}

func (s *Service) applyOne(ctx context.Context, c *planchangesdomain.PlanChange, now time.Time) error {
	if err := s.deps.Accounts.SetPackage(ctx, c.PPPoEAccountID, c.ToPackage, now); err != nil {
		return err
	}
	s.disconnectBestEffort(ctx, c.PPPoEAccountID)
	_, err := s.deps.Plans.SetStatus(ctx, c.ID, planchangesdomain.StatusApplied)
	return err
}

func (s *Service) disconnectBestEffort(ctx context.Context, accountID string) {
	if s.deps.Disco == nil {
		return
	}
	_, _, _ = s.deps.Disco.DisconnectAll(ctx, accountID)
}
