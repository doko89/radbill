package infrastructure_test

import (
	"context"
	"testing"
	"time"

	billapp "boilerplate/api/internal/billing/application"
	billinfra "boilerplate/api/internal/billing/infrastructure"
	"boilerplate/api/internal/network/notify"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	topupapp "boilerplate/api/internal/topup/application"
	topupdomain "boilerplate/api/internal/topup/domain"
	topupinfra "boilerplate/api/internal/topup/infrastructure"
)

// TestManualPaymentCreditsTopupEndToEnd pins the C1 paid-hook path with
// real storage: top-up invoice -> manual pay -> fanout -> quota_topups
// row, while a monthly invoice stays a silent no-op.
func TestManualPaymentCreditsTopupEndToEnd(t *testing.T) {
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Fatalf("sqlite unavailable: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	seedTopupBase(t, pool)

	ctx := context.Background()
	q := sqlcgen.New(pool)
	events := notify.New(pool, "sqlite")
	billRepo := billinfra.NewSQLRepository(pool, q, events)
	billSvc := billapp.NewService(billRepo, billRepo, billRepo)
	store := topupinfra.NewStore(q)
	topupSvc := topupapp.NewService(topupapp.Deps{
		Accounts: store, Prices: store, Topups: store, Invoices: store,
		Billing: topupinfra.NewBillingAdapter(billSvc), Events: events,
	})
	topupSvc.SetNow(func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) })
	billSvc.SetPaidNotifier(billapp.FanoutNotifiers(topupinfra.NewPaidHook(topupSvc)))

	inv, err := topupSvc.CreateInvoice(ctx, "cust1", "acc1", 5*topupdomain.GB)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total != 50000 {
		t.Fatalf("total = %d", inv.Total)
	}
	if _, err := billSvc.ManualPayment(ctx, inv.ID, 0); err != nil {
		t.Fatal(err)
	}
	found, err := store.FindByInvoice(ctx, inv.ID)
	if err != nil || found == nil {
		t.Fatalf("topup row = %+v %v", found, err)
	}
	if found.Bytes != 5*topupdomain.GB || found.PeriodStart != "2026-11-01T00:00:00Z" {
		t.Fatalf("row = %+v", found)
	}

	monthly, err := billSvc.Create(ctx, "acc1", []billapp.ItemInput{
		{Description: "Tagihan bulanan", Qty: 1, Price: 50000},
	}, "2026-11-14")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := billSvc.ManualPayment(ctx, monthly.ID, 0); err != nil {
		t.Fatal(err)
	}
	if found, err := store.FindByInvoice(ctx, monthly.ID); err != nil || found != nil {
		t.Fatalf("monthly row = %+v %v", found, err)
	}
}
