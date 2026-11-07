// Package application hosts the scheduler jobs: monthly invoice generation
// and grace-period isolation, plus the ticker runner and status tracker.
package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	scheddomain "boilerplate/api/internal/scheduler/domain"
)

// pageSize bounds one scheduler database page.
const pageSize = 200

// dueDaysAfterIssue is the invoice due date offset for generated invoices.
const dueDaysAfterIssue = 7

// Deps wires a Service. All fields are required.
type Deps struct {
	Store   scheddomain.Store
	Billing scheddomain.Billing
	PPPoE   scheddomain.PPPoE
	Notify  scheddomain.Notifier
	CoA     scheddomain.Disconnecter
	Events  scheddomain.Emitter
	Config  scheddomain.ConfigReader
}

// Service runs the billing jobs. Jobs are idempotent and safe to re-run
// after downtime (catch-up): they always derive work from current state,
// never from a persisted cursor.
type Service struct {
	deps    Deps
	tracker *Tracker
	now     func() time.Time
}

// NewService builds a Service.
func NewService(d Deps) *Service {
	return &Service{deps: d, tracker: NewTracker(), now: time.Now}
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

// Status returns the last-run record per job.
func (s *Service) Status() []scheddomain.Status { return s.tracker.Snapshot() }

// RunOnce executes generate then isolate, recording each job's status.
// Per-account failures never stop the run; the first error is returned with
// a failure count so status shows the job needs attention.
func (s *Service) RunOnce(ctx context.Context) error {
	if err := s.runGenerate(ctx); err != nil {
		s.runIsolate(ctx)
		return err
	}
	return s.runIsolate(ctx)
}

func (s *Service) runGenerate(ctx context.Context) error {
	runAt := s.now().UTC()
	n, err := s.Generate(ctx)
	s.tracker.Record(scheddomain.JobGenerate, runAt, err)
	if err != nil {
		return fmt.Errorf("generate: created %d: %w", n, err)
	}
	return nil
}

func (s *Service) runIsolate(ctx context.Context) error {
	runAt := s.now().UTC()
	isolated, reminded, err := s.IsolateOverdue(ctx)
	s.tracker.Record(scheddomain.JobIsolate, runAt, err)
	if err != nil {
		return fmt.Errorf("isolate: isolated %d reminded %d: %w", isolated, reminded, err)
	}
	return nil
}

// Generate creates one monthly invoice per active/isolated account that has
// no unpaid invoice and no invoice in the current period. Full periods only;
// proration is out of scope (Fase 3).
func (s *Service) Generate(ctx context.Context) (int, error) {
	cfg, err := s.deps.Config.BillingConfig(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now().UTC()
	dueDate := now.AddDate(0, 0, dueDaysAfterIssue).Format("2006-01-02")
	created := 0
	var firstErr error
	failed := 0
	for offset := 0; ; offset += pageSize {
		accounts, err := s.deps.Store.ListBillable(ctx, pageSize, offset)
		if err != nil {
			return created, err
		}
		if len(accounts) == 0 {
			break
		}
		for _, acct := range accounts {
			n, err := s.generateOne(ctx, cfg, acct, dueDate, now)
			created += n
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("account %s: %w", acct.ID, err)
				}
			}
		}
		if len(accounts) < pageSize {
			break
		}
	}
	return created, summarize(firstErr, failed)
}

// generateOne invoices a single account, returning 1 when an invoice was
// created and 0 when the account was skipped.
func (s *Service) generateOne(ctx context.Context, cfg scheddomain.BillingConfig, acct scheddomain.BillableAccount, dueDate string, now time.Time) (int, error) {
	hasUnpaid, err := s.deps.Store.HasUnpaid(ctx, acct.ID)
	if err != nil {
		return 0, err
	}
	if hasUnpaid {
		return 0, nil
	}
	latest, err := s.deps.Store.LatestIssuedAt(ctx, acct.ID)
	if err != nil {
		return 0, err
	}
	periodStart := scheddomain.PeriodStart(cfg.Mode, cfg.Day, acct.CreatedAt, now)
	if latest != nil && !latest.Before(periodStart) {
		return 0, nil
	}
	pkg, err := s.deps.Store.GetPackage(ctx, acct.PackageID)
	if err != nil {
		return 0, err
	}
	if pkg.Price <= 0 {
		return 0, nil
	}
	cust, err := s.deps.Store.GetCustomer(ctx, acct.CustomerID)
	if err != nil {
		return 0, err
	}
	desc := "Tagihan bulanan " + pkg.Name + " periode " + scheddomain.PeriodLabel(periodStart)
	inv, err := s.deps.Billing.Create(ctx, acct.ID,
		[]scheddomain.Item{{Description: desc, Qty: 1, Price: pkg.Price}}, dueDate)
	if err != nil {
		return 0, err
	}
	s.deps.Events.Emit(ctx, "pppoe", "upsert", acct.ID)
	data := map[string]string{
		"Nama": cust.Name, "Nomor": inv.Number, "Total": scheddomain.FormatIDR(inv.Total),
		"JatuhTempo": inv.DueDate, "Username": acct.Username, "Perusahaan": cfg.Company,
	}
	if err := s.deps.Notify.InvoiceIssued(ctx, cust.Phone, acct.ID, inv.ID, data); err != nil {
		return 1, fmt.Errorf("invoice %s created but notify failed: %w", inv.ID, err)
	}
	return 1, nil
}

// IsolateOverdue reminds accounts inside the grace period and isolates those
// past it. CoA disconnects are best-effort and never fail the job.
func (s *Service) IsolateOverdue(ctx context.Context) (isolated, reminded int, err error) {
	cfg, err := s.deps.Config.BillingConfig(ctx)
	if err != nil {
		return 0, 0, err
	}
	now := s.now().UTC()
	today := now.Format("2006-01-02")
	dayStart := today + "T00:00:00Z"
	var firstErr error
	failed := 0
	for offset := 0; ; offset += pageSize {
		invoices, err := s.deps.Store.ListOverdue(ctx, today, pageSize, offset)
		if err != nil {
			return isolated, reminded, err
		}
		if len(invoices) == 0 {
			break
		}
		for _, inv := range invoices {
			iso, rem, err := s.overdueOne(ctx, cfg, inv, today, dayStart, now)
			isolated += iso
			reminded += rem
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("invoice %s: %w", inv.ID, err)
				}
			}
		}
		if len(invoices) < pageSize {
			break
		}
	}
	return isolated, reminded, summarize(firstErr, failed)
}

func (s *Service) overdueOne(ctx context.Context, cfg scheddomain.BillingConfig, inv scheddomain.InvoiceInfo, today, dayStart string, now time.Time) (int, int, error) {
	due, err := time.Parse("2006-01-02", inv.DueDate)
	if err != nil {
		return 0, 0, fmt.Errorf("bad due_date %q: %w", inv.DueDate, err)
	}
	acct, err := s.deps.Store.GetAccount(ctx, inv.PPPoEAccountID)
	if err != nil {
		return 0, 0, err
	}
	cust, err := s.deps.Store.GetCustomer(ctx, inv.CustomerID)
	if err != nil {
		return 0, 0, err
	}
	data := map[string]string{
		"Nama": cust.Name, "Nomor": inv.Number, "Total": scheddomain.FormatIDR(inv.Total),
		"JatuhTempo": inv.DueDate, "Username": acct.Username, "Perusahaan": cfg.Company,
	}
	if today <= due.AddDate(0, 0, cfg.Grace).Format("2006-01-02") {
		already, err := s.deps.Store.RemindedSince(ctx, inv.ID, dayStart)
		if err != nil {
			return 0, 0, err
		}
		if already {
			return 0, 0, nil
		}
		if err := s.deps.Notify.Reminder(ctx, cust.Phone, acct.ID, inv.ID, data); err != nil {
			return 0, 0, err
		}
		return 0, 1, nil
	}
	if acct.Status != "active" {
		return 0, 0, nil
	}
	if err := s.deps.PPPoE.Isolate(ctx, acct.ID, now); err != nil {
		return 0, 0, err
	}
	_, _, _ = s.deps.CoA.DisconnectAll(ctx, acct.ID)
	if err := s.deps.Notify.Isolated(ctx, cust.Phone, acct.ID, inv.ID, data); err != nil {
		return 1, 0, fmt.Errorf("isolated but notify failed: %w", err)
	}
	return 1, 0, nil
}

func summarize(firstErr error, failed int) error {
	if firstErr == nil {
		return nil
	}
	if failed > 1 {
		return fmt.Errorf("%v (and %d more failures)", firstErr, failed-1)
	}
	return firstErr
}

// Tracker records the last run per job for the status endpoint.
type Tracker struct {
	mu sync.Mutex
	m  map[string]*scheddomain.Status
}

// NewTracker builds an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{m: map[string]*scheddomain.Status{}}
}

// Record stores one job run outcome.
func (t *Tracker) Record(job string, runAt time.Time, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.m[job]
	if st == nil {
		st = &scheddomain.Status{Job: job}
		t.m[job] = st
	}
	st.LastRun = runAt
	st.LastOK = err == nil
	st.Runs++
	if err != nil {
		st.LastError = err.Error()
	} else {
		st.LastError = ""
	}
}

// Snapshot returns the job statuses in fixed job order.
func (t *Tracker) Snapshot() []scheddomain.Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]scheddomain.Status, 0, 2)
	for _, job := range []string{scheddomain.JobGenerate, scheddomain.JobIsolate} {
		if st, ok := t.m[job]; ok {
			out = append(out, *st)
		} else {
			out = append(out, scheddomain.Status{Job: job})
		}
	}
	return out
}
