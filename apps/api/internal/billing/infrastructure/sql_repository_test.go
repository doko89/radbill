package infrastructure

import (
	"context"
	"testing"
	"time"

	"boilerplate/api/internal/billing/domain"
	"boilerplate/api/internal/network/notify"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

func openTestDB(t *testing.T) (*sqlcgen.Queries, *SQLRepository) {
	t.Helper()
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		pool.Close()
		t.Skipf("migrate unavailable: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	q := sqlcgen.New(pool)
	return q, NewSQLRepository(pool, q, notify.New(pool, "sqlite"))
}

func seedPPPoE(t *testing.T, q *sqlcgen.Queries, tag, status string) (pppoeID, customerID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	customerID = "cust-pay-" + tag
	if _, err := q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID: customerID, Name: "Pay " + tag, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if _, err := q.CreateBillingPackage(ctx, sqlcgen.CreateBillingPackageParams{
		ID: "pkg-pay-" + tag, Name: "Paket " + tag, Price: 50000, Period: "monthly",
		QuotaReset: "none", OnExhausted: "reject", IsPublic: 1, AllowSelfService: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed package: %v", err)
	}
	pppoeID = "ppp-pay-" + tag
	if _, err := q.CreateBillingPPPoE(ctx, sqlcgen.CreateBillingPPPoEParams{
		ID: pppoeID, CustomerID: customerID, PackageID: "pkg-pay-" + tag,
		Username: "payuser-" + tag, Password: "secret1", Status: status,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed pppoe: %v", err)
	}
	return pppoeID, customerID
}

func TestPayManualExtendsAndReactivates(t *testing.T) {
	q, repo := openTestDB(t)
	ctx := context.Background()
	pppoeID, customerID := seedPPPoE(t, q, "isolated", "isolated")
	now := time.Now().UTC().Truncate(time.Second)

	inv, err := domain.NewInvoice("inv-pay-1", "INV-PAY-1", pppoeID, customerID, 50000, "2026-11-14", now)
	if err != nil {
		t.Fatalf("new invoice: %v", err)
	}
	item, err := domain.NewItem("item-pay-1", inv.ID, "Monthly", 1, 50000)
	if err != nil {
		t.Fatalf("new item: %v", err)
	}
	if err := repo.CreateInvoice(ctx, inv, []*domain.InvoiceItem{item}); err != nil {
		t.Fatalf("create: %v", err)
	}
	wantExp := now.Add(30 * 24 * time.Hour)
	paid, err := repo.PayManual(ctx, inv.ID, 50000, wantExp, now)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	if paid.Status != domain.StatusPaid || paid.PaidAt == nil {
		t.Fatalf("invoice not paid: %+v", paid)
	}
	payments, err := repo.ListPayments(ctx, inv.ID)
	if err != nil || len(payments) != 1 || payments[0].Amount != 50000 || payments[0].Method != domain.MethodManual {
		t.Fatalf("payments: %+v %v", payments, err)
	}
	pppoe, err := q.GetBillingPPPoE(ctx, pppoeID)
	if err != nil {
		t.Fatalf("get pppoe: %v", err)
	}
	if pppoe.Status != "active" {
		t.Fatalf("pppoe status = %q, want active", pppoe.Status)
	}
	if !pppoe.ExpiresAt.Valid || pppoe.ExpiresAt.String != wantExp.Format(time.RFC3339) {
		t.Fatalf("expires_at = %+v, want %s", pppoe.ExpiresAt, wantExp.Format(time.RFC3339))
	}
	if _, err := repo.PayManual(ctx, inv.ID, 50000, wantExp, now); err != domain.ErrAlreadyPaid {
		t.Fatalf("repay = %v, want ErrAlreadyPaid", err)
	}
}

func TestVoidInvoice(t *testing.T) {
	q, repo := openTestDB(t)
	ctx := context.Background()
	pppoeID, customerID := seedPPPoE(t, q, "void", "active")
	now := time.Now().UTC().Truncate(time.Second)

	inv, _ := domain.NewInvoice("inv-void-1", "INV-VOID-1", pppoeID, customerID, 10000, "2026-11-14", now)
	item, _ := domain.NewItem("item-void-1", inv.ID, "Fee", 1, 10000)
	if err := repo.CreateInvoice(ctx, inv, []*domain.InvoiceItem{item}); err != nil {
		t.Fatalf("create: %v", err)
	}
	voided, err := repo.VoidInvoice(ctx, inv.ID, now)
	if err != nil || voided.Status != domain.StatusVoid {
		t.Fatalf("void: %+v %v", voided, err)
	}
	if _, err := repo.VoidInvoice(ctx, inv.ID, now); err != nil {
		t.Fatalf("void idempotent: %v", err)
	}
	if _, err := repo.PayManual(ctx, inv.ID, 10000, now, now); err != domain.ErrInvoiceVoid {
		t.Fatalf("pay void = %v, want ErrInvoiceVoid", err)
	}
}
