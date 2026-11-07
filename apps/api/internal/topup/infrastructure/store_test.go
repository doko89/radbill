package infrastructure_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	topupdomain "boilerplate/api/internal/topup/domain"
	topupinfra "boilerplate/api/internal/topup/infrastructure"
)

func openMigrated(t *testing.T) (*sql.DB, *sqlcgen.Queries) {
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
	return pool, sqlcgen.New(pool)
}

func seedTopupBase(t *testing.T, pool *sql.DB) {
	t.Helper()
	ts := "2026-11-07T12:00:00Z"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO customers (id, name, created_at, updated_at) VALUES ('cust1', 'Budi', $1, $1)`, ts)
	exec(`INSERT INTO packages (id, name, price, period, quota_bytes, quota_reset, created_at, updated_at)
		VALUES ('pkg-q', 'Quota', 50000, 'monthly', 10737418240, 'monthly', $1, $1)`, ts)
	exec(`INSERT INTO pppoe_accounts (id, customer_id, package_id, username, password, status, created_at, updated_at)
		VALUES ('acc1', 'cust1', 'pkg-q', 'user1', 'pw1234', 'active', $1, $1)`, ts)
	exec(`INSERT INTO invoices (id, number, pppoe_account_id, customer_id, total, due_date, status, issued_at, created_at, updated_at)
		VALUES ('inv1', 'INV-1', 'acc1', 'cust1', 50000, '2026-11-14', 'paid', $1, $1, $1)`, ts)
	exec(`INSERT INTO invoice_items (id, invoice_id, description, qty, price)
		VALUES ('item1', 'inv1', 'Top-up kuota 5 GB', 1, 50000)`)
}

func TestStoreAccountPackagePrice(t *testing.T) {
	pool, q := openMigrated(t)
	seedTopupBase(t, pool)
	s := topupinfra.NewStore(q)
	ctx := context.Background()

	acct, err := s.GetAccount(ctx, "acc1")
	if err != nil || acct.CustomerID != "cust1" || acct.PackageID != "pkg-q" {
		t.Fatalf("account = %+v %v", acct, err)
	}
	if _, err := s.GetAccount(ctx, "ghost"); err != topupdomain.ErrAccountNotFound {
		t.Fatalf("ghost = %v", err)
	}
	pkg, err := s.GetPackage(ctx, "pkg-q")
	if err != nil || pkg.QuotaBytes == nil || *pkg.QuotaBytes != 10737418240 || pkg.QuotaReset != "monthly" {
		t.Fatalf("package = %+v %v", pkg, err)
	}
	// Seeded by schema.sql.
	price, err := s.PricePerGB(ctx)
	if err != nil || price != 10000 {
		t.Fatalf("price = %d %v", price, err)
	}
	// Missing/invalid settings fall back to the default.
	if _, err := pool.Exec(`DELETE FROM settings WHERE key = 'topup_price_per_gb'`); err != nil {
		t.Fatal(err)
	}
	if price, _ := s.PricePerGB(ctx); price != topupdomain.DefaultPricePerGB {
		t.Fatalf("missing price = %d", price)
	}
	if _, err := pool.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('topup_price_per_gb', 'junk', '2026-11-07T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if price, _ := s.PricePerGB(ctx); price != topupdomain.DefaultPricePerGB {
		t.Fatalf("junk price = %d", price)
	}
}

func TestStoreTopupRoundTrip(t *testing.T) {
	pool, q := openMigrated(t)
	seedTopupBase(t, pool)
	s := topupinfra.NewStore(q)
	ctx := context.Background()

	if found, err := s.FindByInvoice(ctx, "inv1"); err != nil || found != nil {
		t.Fatalf("before insert = %+v %v", found, err)
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	topup, err := topupdomain.NewTopup("t1", "acc1", 5*topupdomain.GB, "2026-11-01T00:00:00Z", "inv1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, topup); err != nil {
		t.Fatal(err)
	}
	found, err := s.FindByInvoice(ctx, "inv1")
	if err != nil || found == nil || found.Bytes != 5*topupdomain.GB || found.PeriodStart != "2026-11-01T00:00:00Z" {
		t.Fatalf("found = %+v %v", found, err)
	}
	sum, err := s.SumByAccountPeriod(ctx, "acc1", "2026-11-01T00:00:00Z")
	if err != nil || sum != 5*topupdomain.GB {
		t.Fatalf("sum = %d %v", sum, err)
	}
	sum, err = s.SumByAccountPeriod(ctx, "acc1", "2026-10-01T00:00:00Z")
	if err != nil || sum != 0 {
		t.Fatalf("other bucket = %d %v", sum, err)
	}

	inv, err := s.GetInvoice(ctx, "inv1")
	if err != nil || !inv.Paid || inv.PPPoEAccountID != "acc1" {
		t.Fatalf("invoice = %+v %v", inv, err)
	}
	if _, err := s.GetInvoice(ctx, "ghost"); err != topupdomain.ErrInvoiceNotFound {
		t.Fatalf("ghost = %v", err)
	}
	items, err := s.ListItems(ctx, "inv1")
	if err != nil || len(items) != 1 || items[0].Description != "Top-up kuota 5 GB" {
		t.Fatalf("items = %+v %v", items, err)
	}
}
