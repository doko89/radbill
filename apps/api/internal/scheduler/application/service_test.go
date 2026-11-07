package application_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	billapp "boilerplate/api/internal/billing/application"
	billinfra "boilerplate/api/internal/billing/infrastructure"
	"boilerplate/api/internal/network/notify"
	notifydomain "boilerplate/api/internal/notify/domain"
	notifyinfra "boilerplate/api/internal/notify/infrastructure"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	pppoeinfra "boilerplate/api/internal/pppoe/infrastructure"
	schedapp "boilerplate/api/internal/scheduler/application"
	scheddomain "boilerplate/api/internal/scheduler/domain"
	schedinfra "boilerplate/api/internal/scheduler/infrastructure"
	"boilerplate/api/internal/shared/id"
)

// fixedNow freezes the clock mid-month at noon UTC so period math never
// flaps on month or day boundaries.
func fixedNow() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 15, 12, 0, 0, 0, time.UTC)
}

type notifyCall struct {
	kind      string
	target    string
	pppoeID   string
	invoiceID string
	data      map[string]string
}

type fakeNotifier struct {
	logs  *notifyinfra.SQLLogRepository
	calls []notifyCall
}

func (f *fakeNotifier) record(kind, target, pppoeID, invoiceID string, data map[string]string) error {
	f.calls = append(f.calls, notifyCall{kind, target, pppoeID, invoiceID, data})
	_, err := f.logs.Create(context.Background(), notifydomain.Entry{
		ID: id.New(), Kind: kind, Target: target, PPPoEAccountID: pppoeID,
		InvoiceID: invoiceID, Status: notifydomain.StatusSent, CreatedAt: fixedNow(),
	})
	return err
}

func (f *fakeNotifier) InvoiceIssued(_ context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return f.record(notifydomain.KindInvoiceIssued, target, pppoeID, invoiceID, data)
}

func (f *fakeNotifier) Reminder(_ context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return f.record(notifydomain.KindReminder, target, pppoeID, invoiceID, data)
}

func (f *fakeNotifier) Isolated(_ context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return f.record(notifydomain.KindIsolated, target, pppoeID, invoiceID, data)
}

func (f *fakeNotifier) Paid(_ context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return f.record(notifydomain.KindPaid, target, pppoeID, invoiceID, data)
}

type fakeCoA struct {
	ids []string
}

func (f *fakeCoA) DisconnectAll(_ context.Context, pppoeID string) (int, int, error) {
	f.ids = append(f.ids, pppoeID)
	return 0, 0, nil
}

type fakeConfig struct {
	cfg scheddomain.BillingConfig
	err error
}

func (f *fakeConfig) BillingConfig(_ context.Context) (scheddomain.BillingConfig, error) {
	if f.err != nil {
		return scheddomain.BillingConfig{}, f.err
	}
	return f.cfg, nil
}

type harness struct {
	pool    *sql.DB
	q       *sqlcgen.Queries
	svc     *schedapp.Service
	notif   *fakeNotifier
	coa     *fakeCoA
	config  *fakeConfig
	now     time.Time
	stamp40 string // 40 days ago, RFC3339
}

func newHarness(t *testing.T, cfg scheddomain.BillingConfig) *harness {
	t.Helper()
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Fatalf("sqlite unavailable: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	q := sqlcgen.New(pool)
	events := notify.New(pool, "sqlite")
	billRepo := billinfra.NewSQLRepository(pool, q, events)
	billSvc := billapp.NewService(billRepo, billRepo, billRepo)
	notif := &fakeNotifier{logs: notifyinfra.NewSQLLogRepository(q)}
	coa := &fakeCoA{}
	fcfg := &fakeConfig{cfg: cfg}
	now := fixedNow()
	svc := schedapp.NewService(schedapp.Deps{
		Store:   schedinfra.NewStore(q),
		Billing: schedinfra.NewBillingAdapter(billSvc),
		PPPoE:   schedinfra.NewPPPoEAdapter(pppoeinfra.NewSQLRepository(q, events)),
		Notify:  notif,
		CoA:     coa,
		Events:  events,
		Config:  fcfg,
	})
	svc.SetNow(func() time.Time { return now })
	return &harness{pool: pool, q: q, svc: svc, notif: notif, coa: coa, config: fcfg, now: now,
		stamp40: now.AddDate(0, 0, -40).Format(time.RFC3339)}
}

func (h *harness) seedPackage(t *testing.T, tag string, price int64) string {
	t.Helper()
	pid := "pkg-" + tag
	_, err := h.q.CreateBillingPackage(context.Background(), sqlcgen.CreateBillingPackageParams{
		ID: pid, Name: "Paket " + tag, Price: price, Period: "monthly",
		QuotaReset: "none", OnExhausted: "reject", IsPublic: 1, AllowSelfService: 1,
		CreatedAt: h.now.Format(time.RFC3339), UpdatedAt: h.now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("seed package: %v", err)
	}
	return pid
}

func (h *harness) seedCustomer(t *testing.T, tag, phone string) string {
	t.Helper()
	cid := "cust-" + tag
	_, err := h.q.CreateBillingCustomer(context.Background(), sqlcgen.CreateBillingCustomerParams{
		ID: cid, Name: "Customer " + tag,
		Phone:     sql.NullString{String: phone, Valid: phone != ""},
		CreatedAt: h.now.Format(time.RFC3339), UpdatedAt: h.now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return cid
}

func (h *harness) seedPPPoE(t *testing.T, tag, customerID, packageID, status, createdAt string) string {
	t.Helper()
	pid := "ppp-" + tag
	_, err := h.q.CreateBillingPPPoE(context.Background(), sqlcgen.CreateBillingPPPoEParams{
		ID: pid, CustomerID: customerID, PackageID: packageID,
		Username: "user-" + tag, Password: "secret1", Status: status,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("seed pppoe: %v", err)
	}
	return pid
}

// seedInvoice inserts an invoice directly with explicit issued/due dates.
func (h *harness) seedInvoice(t *testing.T, tag, pppoeID, customerID string, total int64, status, issuedAt, dueDate string) string {
	t.Helper()
	iid := "inv-" + tag
	_, err := h.q.CreateBillingInvoice(context.Background(), sqlcgen.CreateBillingInvoiceParams{
		ID: iid, Number: "INV-" + tag, PppoeAccountID: pppoeID, CustomerID: customerID,
		Total: total, DueDate: dueDate, Status: status,
		IssuedAt: issuedAt, CreatedAt: issuedAt, UpdatedAt: issuedAt,
	})
	if err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	return iid
}

func countInvoices(t *testing.T, h *harness) int64 {
	t.Helper()
	n, err := h.q.CountBillingInvoices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func activationCfg() scheddomain.BillingConfig {
	return scheddomain.BillingConfig{Mode: "activation", Day: 1, Grace: 3, Company: "Radbill"}
}

func TestGenerateCreatesAndIsIdempotent(t *testing.T) {
	h := newHarness(t, activationCfg())
	ctx := context.Background()
	pkg := h.seedPackage(t, "gen", 1500000)
	cust := h.seedCustomer(t, "gen", "0812")
	h.seedPPPoE(t, "gen-a", cust, pkg, "active", h.stamp40)
	h.seedPPPoE(t, "gen-b", cust, pkg, "isolated", h.stamp40)

	n, err := h.svc.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || countInvoices(t, h) != 2 {
		t.Fatalf("created = %d, total = %d, want 2/2", n, countInvoices(t, h))
	}
	if len(h.notif.calls) != 2 || h.notif.calls[0].kind != notifydomain.KindInvoiceIssued {
		t.Fatalf("notify calls = %+v", h.notif.calls)
	}
	if h.notif.calls[0].data["Total"] != "1.500.000" {
		t.Fatalf("total formatted = %q", h.notif.calls[0].data["Total"])
	}

	n, err = h.svc.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || countInvoices(t, h) != 2 {
		t.Fatalf("second run created = %d, total = %d, want 0/2", n, countInvoices(t, h))
	}
}

func TestGenerateSkipsUnpaidAndCurrentPeriod(t *testing.T) {
	h := newHarness(t, activationCfg())
	ctx := context.Background()
	pkg := h.seedPackage(t, "skip", 50000)
	cust := h.seedCustomer(t, "skip", "0813")
	recent := h.now.AddDate(0, 0, -2).Format(time.RFC3339)
	due := h.now.AddDate(0, 0, 5).Format("2006-01-02")

	// Unpaid invoice exists: skip via HasUnpaid.
	pppUnpaid := h.seedPPPoE(t, "skip-u", cust, pkg, "active", h.stamp40)
	h.seedInvoice(t, "skip-u", pppUnpaid, cust, 50000, "unpaid", recent, due)
	// Paid invoice issued inside the current period: skip via LatestIssuedAt.
	pppPaid := h.seedPPPoE(t, "skip-p", cust, pkg, "active", h.stamp40)
	h.seedInvoice(t, "skip-p", pppPaid, cust, 50000, "paid", recent, due)
	// Free package: never invoiced.
	free := h.seedPackage(t, "skip-free", 0)
	h.seedPPPoE(t, "skip-f", cust, free, "active", h.stamp40)

	n, err := h.svc.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || countInvoices(t, h) != 2 {
		t.Fatalf("created = %d, total = %d, want 0/2", n, countInvoices(t, h))
	}
	if len(h.notif.calls) != 0 {
		t.Fatalf("unexpected notifications: %+v", h.notif.calls)
	}
}

func TestGenerateAfterPreviousPeriod(t *testing.T) {
	h := newHarness(t, activationCfg())
	ctx := context.Background()
	for _, mode := range []string{"activation", "fixed"} {
		h.config.cfg = scheddomain.BillingConfig{Mode: mode, Day: 1, Grace: 3, Company: "Radbill"}
		pkg := h.seedPackage(t, "prev-"+mode, 75000)
		cust := h.seedCustomer(t, "prev-"+mode, "0814")
		ppp := h.seedPPPoE(t, "prev-"+mode, cust, pkg, "active", h.stamp40)
		old := h.now.AddDate(0, 0, -40)
		h.seedInvoice(t, "prev-"+mode, ppp, cust, 75000, "paid",
			old.Format(time.RFC3339), old.AddDate(0, 0, 7).Format("2006-01-02"))

		n, err := h.svc.Generate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("mode %s: created = %d, want 1", mode, n)
		}
	}
	if total := countInvoices(t, h); total != 4 {
		t.Fatalf("total = %d, want 4 (2 seeds + 2 generated)", total)
	}
}

func TestIsolateOverdue(t *testing.T) {
	h := newHarness(t, activationCfg())
	ctx := context.Background()
	pkg := h.seedPackage(t, "iso", 50000)
	cust := h.seedCustomer(t, "iso", "0815")
	issued := h.now.AddDate(0, 0, -20).Format(time.RFC3339)

	// Past grace (due 10 days ago, grace 3): isolate.
	pppPast := h.seedPPPoE(t, "iso-past", cust, pkg, "active", h.stamp40)
	h.seedInvoice(t, "iso-past", pppPast, cust, 50000, "unpaid", issued,
		h.now.AddDate(0, 0, -10).Format("2006-01-02"))
	// Inside grace (due yesterday): remind only.
	pppGrace := h.seedPPPoE(t, "iso-grace", cust, pkg, "active", h.stamp40)
	h.seedInvoice(t, "iso-grace", pppGrace, cust, 50000, "unpaid", issued,
		h.now.AddDate(0, 0, -1).Format("2006-01-02"))
	// Not yet due: untouched.
	pppFuture := h.seedPPPoE(t, "iso-future", cust, pkg, "active", h.stamp40)
	h.seedInvoice(t, "iso-future", pppFuture, cust, 50000, "unpaid", issued,
		h.now.AddDate(0, 0, 5).Format("2006-01-02"))
	// Past grace but already isolated: skipped.
	pppIso := h.seedPPPoE(t, "iso-already", cust, pkg, "isolated", h.stamp40)
	h.seedInvoice(t, "iso-already", pppIso, cust, 50000, "unpaid", issued,
		h.now.AddDate(0, 0, -10).Format("2006-01-02"))

	isolated, reminded, err := h.svc.IsolateOverdue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if isolated != 1 || reminded != 1 {
		t.Fatalf("isolated=%d reminded=%d, want 1/1", isolated, reminded)
	}
	row, err := h.q.GetBillingPPPoE(ctx, pppPast)
	if err != nil || row.Status != "isolated" {
		t.Fatalf("past-grace status = %+v,%v", row.Status, err)
	}
	row, err = h.q.GetBillingPPPoE(ctx, pppGrace)
	if err != nil || row.Status != "active" {
		t.Fatalf("in-grace status = %+v,%v", row.Status, err)
	}
	if len(h.coa.ids) != 1 || h.coa.ids[0] != pppPast {
		t.Fatalf("coa disconnects = %v", h.coa.ids)
	}

	// Second run the same day: no duplicate reminder; nothing new to isolate.
	isolated, reminded, err = h.svc.IsolateOverdue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if isolated != 0 || reminded != 0 {
		t.Fatalf("second run isolated=%d reminded=%d, want 0/0", isolated, reminded)
	}
	reminders := 0
	for _, c := range h.notif.calls {
		if c.kind == notifydomain.KindReminder {
			reminders++
		}
	}
	if reminders != 1 {
		t.Fatalf("reminder notifications = %d, want 1", reminders)
	}
}

func TestRunOnceTracksStatus(t *testing.T) {
	h := newHarness(t, activationCfg())
	if err := h.svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := h.svc.Status()
	if len(st) != 2 || st[0].Job != scheddomain.JobGenerate || st[1].Job != scheddomain.JobIsolate {
		t.Fatalf("status = %+v", st)
	}
	for _, s := range st {
		if s.Runs != 1 || !s.LastOK || s.LastRun.IsZero() {
			t.Fatalf("job %s status = %+v", s.Job, s)
		}
	}
}

func TestRunOnceRecordsFailure(t *testing.T) {
	h := newHarness(t, activationCfg())
	h.svc = schedapp.NewService(schedapp.Deps{
		Store:   schedinfra.NewStore(h.q),
		Billing: nil, PPPoE: nil, Notify: h.notif, CoA: h.coa,
		Events: nil,
		Config: &fakeConfig{err: errors.New("settings down")},
	})
	h.svc.SetNow(func() time.Time { return h.now })
	if err := h.svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error from failing config")
	}
	for _, s := range h.svc.Status() {
		if s.Runs != 1 || s.LastOK || s.LastError == "" {
			t.Fatalf("job %s status = %+v", s.Job, s)
		}
	}
}
