// Package infrastructure implements planchanges ports on sqlc, the pppoe
// repository (package switches with pppoe/upsert), and billing (prorata).
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"time"

	billapp "boilerplate/api/internal/billing/application"
	planchangesdomain "boilerplate/api/internal/planchanges/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
	pppoedomain "boilerplate/api/internal/pppoe/domain"
	pppoeinfra "boilerplate/api/internal/pppoe/infrastructure"
)

// Store implements AccountStore and PlanStore.
type Store struct {
	q     *sqlcgen.Queries
	pppoe *pppoeinfra.SQLRepository
}

// NewStore builds a Store over the shared queries and pppoe repository.
func NewStore(q *sqlcgen.Queries, pppoe *pppoeinfra.SQLRepository) *Store {
	return &Store{q: q, pppoe: pppoe}
}

// GetAccount returns the plan-change slice of a PPPoE account.
func (s *Store) GetAccount(ctx context.Context, id string) (*planchangesdomain.Account, error) {
	p, err := s.pppoe.Get(ctx, id)
	if err != nil {
		if errors.Is(err, pppoedomain.ErrPPPoENotFound) {
			return nil, planchangesdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return &planchangesdomain.Account{ID: p.ID, CustomerID: p.CustomerID, PackageID: p.PackageID}, nil
}

// GetPackage returns the plan-change slice of a package.
func (s *Store) GetPackage(ctx context.Context, id string) (*planchangesdomain.Package, error) {
	row, err := s.q.GetBillingPackage(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, planchangesdomain.ErrPackageNotFound
		}
		return nil, err
	}
	return &planchangesdomain.Package{
		ID: row.ID, Name: row.Name, Price: row.Price,
		AllowSelfService: row.AllowSelfService != 0,
		UpgradeGroup:     row.UpgradeGroup.String,
	}, nil
}

// SetPackage switches the account package via the pppoe repository, which
// emits the pppoe/upsert cache-refresh event.
func (s *Store) SetPackage(ctx context.Context, accountID, packageID string, now time.Time) error {
	p, err := s.pppoe.Get(ctx, accountID)
	if err != nil {
		if errors.Is(err, pppoedomain.ErrPPPoENotFound) {
			return planchangesdomain.ErrAccountNotFound
		}
		return err
	}
	if !s.pppoe.PackageExists(ctx, packageID) {
		return planchangesdomain.ErrPackageNotFound
	}
	if err := p.ChangePackage(packageID, now.UTC()); err != nil {
		return err
	}
	return s.pppoe.Update(ctx, p)
}

// HasUnpaid reports whether the account has an unpaid invoice.
func (s *Store) HasUnpaid(ctx context.Context, accountID string) (bool, error) {
	_, err := s.q.GetUnpaidInvoiceByPPPoE(ctx, accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Insert persists a plan_changes row.
func (s *Store) Insert(ctx context.Context, c *planchangesdomain.PlanChange) error {
	_, err := s.q.CreatePlanChange(ctx, sqlcgen.CreatePlanChangeParams{
		ID:             c.ID,
		PppoeAccountID: c.PPPoEAccountID,
		FromPackage:    c.FromPackage,
		ToPackage:      c.ToPackage,
		EffectiveAt:    c.EffectiveAt.UTC().Format(time.RFC3339),
		Status:         c.Status,
		CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// Get returns one change or ErrChangeNotFound.
func (s *Store) Get(ctx context.Context, id string) (*planchangesdomain.PlanChange, error) {
	row, err := s.q.GetPlanChange(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, planchangesdomain.ErrChangeNotFound
		}
		return nil, err
	}
	return toChange(row)
}

// ListByAccount returns one account's changes, newest first.
func (s *Store) ListByAccount(ctx context.Context, accountID string, limit, offset int) ([]*planchangesdomain.PlanChange, error) {
	rows, err := s.q.ListPlanChangesByAccount(ctx, sqlcgen.ListPlanChangesByAccountParams{
		PppoeAccountID: accountID,
		Limit:          int32(limit),
		Offset:         int32(offset),
	})
	if err != nil {
		return nil, err
	}
	return toChanges(rows)
}

// ListMine returns the customer's changes, newest first.
func (s *Store) ListMine(ctx context.Context, customerID string, limit, offset int) ([]*planchangesdomain.PlanChange, error) {
	rows, err := s.q.ListPlanChangesByCustomer(ctx, sqlcgen.ListPlanChangesByCustomerParams{
		CustomerID: customerID,
		Limit:      int32(limit),
		Offset:     int32(offset),
	})
	if err != nil {
		return nil, err
	}
	return toChanges(rows)
}

// CountMine counts the customer's changes.
func (s *Store) CountMine(ctx context.Context, customerID string) (int64, error) {
	return s.q.CountPlanChangesByCustomer(ctx, customerID)
}

// ListAll returns every change, newest first.
func (s *Store) ListAll(ctx context.Context, limit, offset int) ([]*planchangesdomain.PlanChange, error) {
	rows, err := s.q.ListPlanChanges(ctx, sqlcgen.ListPlanChangesParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	return toChanges(rows)
}

// CountAll counts every change.
func (s *Store) CountAll(ctx context.Context) (int64, error) {
	return s.q.CountPlanChanges(ctx)
}

// CountActiveSince counts pending+applied rows created at or after since.
func (s *Store) CountActiveSince(ctx context.Context, accountID, since string) (int64, error) {
	return s.q.CountActivePlanChangesByAccountSince(ctx, sqlcgen.CountActivePlanChangesByAccountSinceParams{
		PppoeAccountID: accountID,
		CreatedAt:      since,
	})
}

// ListDue returns pending rows with effective_at <= at.
func (s *Store) ListDue(ctx context.Context, at string, limit int) ([]*planchangesdomain.PlanChange, error) {
	rows, err := s.q.ListDuePlanChanges(ctx, sqlcgen.ListDuePlanChangesParams{
		EffectiveAt: at,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return toChanges(rows)
}

// SetStatus moves a change to a new status.
func (s *Store) SetStatus(ctx context.Context, id, status string) (*planchangesdomain.PlanChange, error) {
	row, err := s.q.UpdatePlanChangeStatus(ctx, sqlcgen.UpdatePlanChangeStatusParams{
		ID:     id,
		Status: status,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, planchangesdomain.ErrChangeNotFound
		}
		return nil, err
	}
	return toChange(row)
}

func toChanges(rows []sqlcgen.PlanChange) ([]*planchangesdomain.PlanChange, error) {
	out := make([]*planchangesdomain.PlanChange, 0, len(rows))
	for _, row := range rows {
		c, err := toChange(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func toChange(row sqlcgen.PlanChange) (*planchangesdomain.PlanChange, error) {
	effective, err := time.Parse(time.RFC3339, row.EffectiveAt)
	if err != nil {
		return nil, err
	}
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &planchangesdomain.PlanChange{
		ID: row.ID, PPPoEAccountID: row.PppoeAccountID,
		FromPackage: row.FromPackage, ToPackage: row.ToPackage,
		EffectiveAt: effective, Status: row.Status, CreatedAt: created,
	}, nil
}

// BillingAdapter implements Billing on the billing service, reusing its
// numbering, validation, and totals.
type BillingAdapter struct {
	svc *billapp.Service
}

// NewBillingAdapter builds a BillingAdapter.
func NewBillingAdapter(svc *billapp.Service) *BillingAdapter {
	return &BillingAdapter{svc: svc}
}

// Create builds an unpaid invoice for the account.
func (a *BillingAdapter) Create(ctx context.Context, pppoeID string, items []planchangesdomain.ItemInput, dueDate string) (planchangesdomain.CreatedInvoice, error) {
	in := make([]billapp.ItemInput, 0, len(items))
	for _, it := range items {
		in = append(in, billapp.ItemInput{Description: it.Description, Qty: it.Qty, Price: it.Price})
	}
	dto, err := a.svc.Create(ctx, pppoeID, in, dueDate)
	if err != nil {
		return planchangesdomain.CreatedInvoice{}, err
	}
	return planchangesdomain.CreatedInvoice{ID: dto.ID, Number: dto.Number, Total: dto.Total}, nil
}

// PackageCatalog lists public packages for the portal plan-picker UI.
type PackageCatalog struct {
	q *sqlcgen.Queries
}

// NewPackageCatalog builds a PackageCatalog.
func NewPackageCatalog(q *sqlcgen.Queries) *PackageCatalog {
	return &PackageCatalog{q: q}
}

// PublicPackage is the portal-safe package shape: catalog specs only,
// no secrets (packages hold no credentials by design).
type PublicPackage struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Price              int64   `json:"price"`
	Period             string  `json:"period"`
	RateLimit          *string `json:"rate_limit"`
	AddressPool        *string `json:"address_pool"`
	ValiditySeconds    *int64  `json:"validity_seconds"`
	OnlineSecondsLimit *int64  `json:"online_seconds_limit"`
	QuotaBytes         *int64  `json:"quota_bytes"`
	QuotaReset         string  `json:"quota_reset"`
	FUPThresholdBytes  *int64  `json:"fup_threshold_bytes"`
	FUPRateLimit       *string `json:"fup_rate_limit"`
	OnExhausted        string  `json:"on_exhausted"`
	AllowSelfService   bool    `json:"allow_self_service"`
	UpgradeGroup       *string `json:"upgrade_group"`
}

// ListPublic returns is_public packages ordered by price.
func (c *PackageCatalog) ListPublic(ctx context.Context) ([]PublicPackage, error) {
	rows, err := c.q.ListPublicPackages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PublicPackage, 0, len(rows))
	for _, row := range rows {
		out = append(out, PublicPackage{
			ID: row.ID, Name: row.Name, Price: row.Price, Period: row.Period,
			RateLimit:          nullStr(row.RateLimit),
			AddressPool:        nullStr(row.AddressPool),
			ValiditySeconds:    nullInt(row.ValiditySeconds),
			OnlineSecondsLimit: nullInt(row.OnlineSecondsLimit),
			QuotaBytes:         nullInt(row.QuotaBytes),
			QuotaReset:         row.QuotaReset,
			FUPThresholdBytes:  nullInt(row.FupThresholdBytes),
			FUPRateLimit:       nullStr(row.FupRateLimit),
			OnExhausted:        row.OnExhausted,
			AllowSelfService:   row.AllowSelfService != 0,
			UpgradeGroup:       nullStr(row.UpgradeGroup),
		})
	}
	return out, nil
}

func nullStr(v sql.NullString) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
