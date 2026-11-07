package application_test

import (
	"context"
	"testing"
	"time"

	topupapp "boilerplate/api/internal/topup/application"
	topupdomain "boilerplate/api/internal/topup/domain"
)

func i64(v int64) *int64 { return &v }

type fakeAccounts struct {
	accts map[string]*topupdomain.Account
	pkgs  map[string]*topupdomain.Package
}

func (f *fakeAccounts) GetAccount(_ context.Context, id string) (*topupdomain.Account, error) {
	a, ok := f.accts[id]
	if !ok {
		return nil, topupdomain.ErrAccountNotFound
	}
	return a, nil
}

func (f *fakeAccounts) GetPackage(_ context.Context, id string) (*topupdomain.Package, error) {
	p, ok := f.pkgs[id]
	if !ok {
		return nil, topupdomain.ErrAccountNotFound
	}
	return p, nil
}

type fakePrices struct{ price int64 }

func (f *fakePrices) PricePerGB(_ context.Context) (int64, error) { return f.price, nil }

type fakeTopups struct {
	byInvoice map[string]*topupdomain.Topup
	inserted  []*topupdomain.Topup
}

func (f *fakeTopups) FindByInvoice(_ context.Context, invoiceID string) (*topupdomain.Topup, error) {
	return f.byInvoice[invoiceID], nil
}

func (f *fakeTopups) Insert(_ context.Context, t *topupdomain.Topup) error {
	f.byInvoice[t.InvoiceID] = t
	f.inserted = append(f.inserted, t)
	return nil
}

func (f *fakeTopups) SumByAccountPeriod(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

type fakeInvoices struct {
	invs  map[string]*topupdomain.Invoice
	items map[string][]topupdomain.Item
}

func (f *fakeInvoices) GetInvoice(_ context.Context, id string) (*topupdomain.Invoice, error) {
	inv, ok := f.invs[id]
	if !ok {
		return nil, topupdomain.ErrInvoiceNotFound
	}
	return inv, nil
}

func (f *fakeInvoices) ListItems(_ context.Context, invoiceID string) ([]topupdomain.Item, error) {
	return f.items[invoiceID], nil
}

type fakeBilling struct {
	created []topupdomain.CreatedInvoice
	inputs  [][]topupdomain.ItemInput
}

func (f *fakeBilling) Create(_ context.Context, pppoeID string, items []topupdomain.ItemInput, _ string) (topupdomain.CreatedInvoice, error) {
	var total int64
	for _, it := range items {
		total += it.Qty * it.Price
	}
	inv := topupdomain.CreatedInvoice{ID: "inv-" + pppoeID, Number: "INV-1", Total: total}
	f.created = append(f.created, inv)
	f.inputs = append(f.inputs, items)
	return inv, nil
}

type fakeEvents struct{ ids []string }

func (f *fakeEvents) Emit(_ context.Context, typ, action, id string) {
	f.ids = append(f.ids, typ+"/"+action+"/"+id)
}

func testDeps() (topupapp.Deps, *fakeTopups, *fakeBilling, *fakeEvents) {
	topups := &fakeTopups{byInvoice: map[string]*topupdomain.Topup{}}
	billing := &fakeBilling{}
	events := &fakeEvents{}
	return topupapp.Deps{
		Accounts: &fakeAccounts{
			accts: map[string]*topupdomain.Account{
				"acc1": {ID: "acc1", CustomerID: "cust1", PackageID: "pkg-q", CreatedAt: "2026-10-05T00:00:00Z"},
				"acc2": {ID: "acc2", CustomerID: "cust2", PackageID: "pkg-q", CreatedAt: "2026-10-05T00:00:00Z"},
				"acc3": {ID: "acc3", CustomerID: "cust1", PackageID: "pkg-flat", CreatedAt: "2026-10-05T00:00:00Z"},
			},
			pkgs: map[string]*topupdomain.Package{
				"pkg-q":    {ID: "pkg-q", QuotaBytes: i64(10 * topupdomain.GB), QuotaReset: "monthly"},
				"pkg-flat": {ID: "pkg-flat", QuotaReset: "none"},
			},
		},
		Prices:   &fakePrices{price: 10000},
		Topups:   topups,
		Invoices: &fakeInvoices{invs: map[string]*topupdomain.Invoice{}, items: map[string][]topupdomain.Item{}},
		Billing:  billing,
		Events:   events,
	}, topups, billing, events
}

func TestCreateInvoiceOK(t *testing.T) {
	deps, _, billing, _ := testDeps()
	svc := topupapp.NewService(deps)
	inv, err := svc.CreateInvoice(context.Background(), "cust1", "acc1", 5*topupdomain.GB)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total != 50000 {
		t.Fatalf("total = %d", inv.Total)
	}
	if got := billing.inputs[0][0].Description; got != "Top-up kuota 5 GB" {
		t.Fatalf("description = %q", got)
	}
}

func TestCreateInvoiceRejects(t *testing.T) {
	deps, _, _, _ := testDeps()
	svc := topupapp.NewService(deps)
	ctx := context.Background()
	if _, err := svc.CreateInvoice(ctx, "cust1", "acc1", 2*topupdomain.GB); err != topupdomain.ErrBadPreset {
		t.Fatalf("non-preset = %v", err)
	}
	if _, err := svc.CreateInvoice(ctx, "cust1", "acc2", 5*topupdomain.GB); err != topupdomain.ErrAccountNotFound {
		t.Fatalf("other customer = %v", err)
	}
	if _, err := svc.CreateInvoice(ctx, "", "ghost", 5*topupdomain.GB); err != topupdomain.ErrAccountNotFound {
		t.Fatalf("unknown = %v", err)
	}
	if _, err := svc.CreateInvoice(ctx, "cust1", "acc3", 5*topupdomain.GB); err != topupdomain.ErrNoQuotaPackage {
		t.Fatalf("no-quota package = %v", err)
	}
}

func TestApplyOnInvoicePaid(t *testing.T) {
	deps, topups, _, events := testDeps()
	invs := deps.Invoices.(*fakeInvoices)
	invs.invs["inv-paid"] = &topupdomain.Invoice{ID: "inv-paid", PPPoEAccountID: "acc1", Status: "paid", Paid: true}
	invs.items["inv-paid"] = []topupdomain.Item{{Description: "Top-up kuota 5 GB", Qty: 1, Price: 50000}}
	invs.invs["inv-unpaid"] = &topupdomain.Invoice{ID: "inv-unpaid", PPPoEAccountID: "acc1", Status: "unpaid"}
	invs.items["inv-unpaid"] = []topupdomain.Item{{Description: "Top-up kuota 5 GB", Qty: 1, Price: 50000}}
	invs.invs["inv-monthly"] = &topupdomain.Invoice{ID: "inv-monthly", PPPoEAccountID: "acc1", Status: "paid", Paid: true}
	invs.items["inv-monthly"] = []topupdomain.Item{{Description: "Tagihan bulanan", Qty: 1, Price: 50000}}

	svc := topupapp.NewService(deps)
	svc.SetNow(func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) })
	ctx := context.Background()

	if err := svc.ApplyOnInvoicePaid(ctx, "inv-paid"); err != nil {
		t.Fatalf("paid: %v", err)
	}
	if len(topups.inserted) != 1 {
		t.Fatalf("inserted = %d", len(topups.inserted))
	}
	got := topups.inserted[0]
	if got.Bytes != 5*topupdomain.GB || got.PPPoEAccountID != "acc1" || got.InvoiceID != "inv-paid" {
		t.Fatalf("topup = %+v", got)
	}
	if got.PeriodStart != "2026-11-01T00:00:00Z" {
		t.Fatalf("period = %q", got.PeriodStart)
	}
	if len(events.ids) != 1 || events.ids[0] != "pppoe/upsert/acc1" {
		t.Fatalf("events = %v", events.ids)
	}

	// Idempotent replay.
	if err := svc.ApplyOnInvoicePaid(ctx, "inv-paid"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(topups.inserted) != 1 || len(events.ids) != 1 {
		t.Fatalf("replay inserted/emitted: %d %d", len(topups.inserted), len(events.ids))
	}
	// Unpaid and non-top-up invoices are silent no-ops.
	if err := svc.ApplyOnInvoicePaid(ctx, "inv-unpaid"); err != nil {
		t.Fatalf("unpaid: %v", err)
	}
	if err := svc.ApplyOnInvoicePaid(ctx, "inv-monthly"); err != nil {
		t.Fatalf("monthly: %v", err)
	}
	if len(topups.inserted) != 1 {
		t.Fatalf("inserted = %d", len(topups.inserted))
	}
	if err := svc.ApplyOnInvoicePaid(ctx, "ghost"); err != topupdomain.ErrInvoiceNotFound {
		t.Fatalf("ghost = %v", err)
	}
}
