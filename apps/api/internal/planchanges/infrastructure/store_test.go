package infrastructure_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"boilerplate/api/internal/network/notify"
	plandomain "boilerplate/api/internal/planchanges/domain"
	planinfra "boilerplate/api/internal/planchanges/infrastructure"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	pppoeinfra "boilerplate/api/internal/pppoe/infrastructure"
)

func openStore(t *testing.T) (*sql.DB, *planinfra.Store) {
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
	return pool, planinfra.NewStore(q, pppoeinfra.NewSQLRepository(q, notify.New(pool, "sqlite")))
}

func seedPlanBase(t *testing.T, pool *sql.DB) {
	t.Helper()
	ts := "2026-11-07T12:00:00Z"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO customers (id, name, created_at, updated_at) VALUES ('cust1', 'Budi', $1, $1)`, ts)
	exec(`INSERT INTO packages (id, name, price, period, allow_self_service, upgrade_group, is_public, created_at, updated_at) VALUES
		('basic', 'Basic', 50000, 'monthly', 1, 'g1', 1, $1, $1),
		('pro', 'Pro', 100000, 'monthly', 1, 'g1', 1, $1, $1),
		('hidden', 'Hidden', 70000, 'monthly', 0, 'g1', 0, $1, $1)`, ts)
	exec(`INSERT INTO pppoe_accounts (id, customer_id, package_id, username, password, status, created_at, updated_at)
		VALUES ('acc1', 'cust1', 'basic', 'user1', 'pw1234', 'active', $1, $1)`, ts)
}

func TestStoreAccountPackageUnpaid(t *testing.T) {
	pool, s := openStore(t)
	seedPlanBase(t, pool)
	ctx := context.Background()

	acct, err := s.GetAccount(ctx, "acc1")
	if err != nil || acct.CustomerID != "cust1" || acct.PackageID != "basic" {
		t.Fatalf("account = %+v %v", acct, err)
	}
	pkg, err := s.GetPackage(ctx, "pro")
	if err != nil || !pkg.AllowSelfService || pkg.UpgradeGroup != "g1" || pkg.Price != 100000 {
		t.Fatalf("package = %+v %v", pkg, err)
	}
	if _, err := s.GetPackage(ctx, "ghost"); err != plandomain.ErrPackageNotFound {
		t.Fatalf("ghost = %v", err)
	}
	if has, _ := s.HasUnpaid(ctx, "acc1"); has {
		t.Fatal("unpaid before invoice")
	}
	ts := "2026-11-07T12:00:00Z"
	if _, err := pool.Exec(`INSERT INTO invoices (id, number, pppoe_account_id, customer_id, total, due_date, status, issued_at, created_at, updated_at)
		VALUES ('inv1', 'INV-1', 'acc1', 'cust1', 50000, '2026-11-14', 'unpaid', $1, $1, $1)`, ts); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasUnpaid(ctx, "acc1"); !has {
		t.Fatal("unpaid not detected")
	}
	if err := s.SetPackage(ctx, "acc1", "pro", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if acct, _ := s.GetAccount(ctx, "acc1"); acct.PackageID != "pro" {
		t.Fatalf("package = %q", acct.PackageID)
	}
	if err := s.SetPackage(ctx, "acc1", "ghost", time.Now().UTC()); err != plandomain.ErrPackageNotFound {
		t.Fatalf("ghost package = %v", err)
	}
}

func TestStorePlanRoundTrip(t *testing.T) {
	pool, s := openStore(t)
	seedPlanBase(t, pool)
	ctx := context.Background()

	mk := func(id, status, created, effective string) *plandomain.PlanChange {
		cr, _ := time.Parse(time.RFC3339, created)
		ef, _ := time.Parse(time.RFC3339, effective)
		c, err := plandomain.NewPlanChange(id, "acc1", "basic", "pro", ef, status, cr)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	oct := mk("c-oct", plandomain.StatusApplied, "2026-10-05T00:00:00Z", "2026-10-05T00:00:00Z")
	nov := mk("c-nov", plandomain.StatusPending, "2026-11-02T00:00:00Z", "2026-12-01T00:00:00Z")
	for _, c := range []*plandomain.PlanChange{oct, nov} {
		if err := s.Insert(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(ctx, "c-nov")
	if err != nil || got.Status != plandomain.StatusPending || got.ToPackage != "pro" {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := s.Get(ctx, "ghost"); err != plandomain.ErrChangeNotFound {
		t.Fatalf("ghost = %v", err)
	}
	// Frequency counts pending+applied created since month start.
	n, err := s.CountActiveSince(ctx, "acc1", "2026-11-01T00:00:00Z")
	if err != nil || n != 1 {
		t.Fatalf("count nov = %d %v", n, err)
	}
	n, err = s.CountActiveSince(ctx, "acc1", "2026-10-01T00:00:00Z")
	if err != nil || n != 2 {
		t.Fatalf("count oct = %d %v", n, err)
	}
	// Due lists pending rows at or before the instant.
	due, err := s.ListDue(ctx, "2026-12-01T00:00:00Z", 100)
	if err != nil || len(due) != 1 || due[0].ID != "c-nov" {
		t.Fatalf("due = %+v %v", due, err)
	}
	due, err = s.ListDue(ctx, "2026-11-01T00:00:00Z", 100)
	if err != nil || len(due) != 0 {
		t.Fatalf("due early = %+v %v", due, err)
	}
	// Status move + lists.
	updated, err := s.SetStatus(ctx, "c-nov", plandomain.StatusApplied)
	if err != nil || updated.Status != plandomain.StatusApplied {
		t.Fatalf("set = %+v %v", updated, err)
	}
	mine, err := s.ListMine(ctx, "cust1", 10, 0)
	total, err2 := s.CountMine(ctx, "cust1")
	if err != nil || err2 != nil || total != 2 || len(mine) != 2 {
		t.Fatalf("mine = %d/%d %v %v", len(mine), total, err, err2)
	}
	if mine[0].ID != "c-nov" {
		t.Fatalf("order = %s", mine[0].ID)
	}
	all, err := s.ListAll(ctx, 10, 0)
	total, err2 = s.CountAll(ctx)
	if err != nil || err2 != nil || total != 2 || len(all) != 2 {
		t.Fatalf("all = %d/%d %v %v", len(all), total, err, err2)
	}
	byAcct, err := s.ListByAccount(ctx, "acc1", 10, 0)
	if err != nil || len(byAcct) != 2 {
		t.Fatalf("by account = %+v %v", byAcct, err)
	}
}

func TestPackageCatalogPublicOnly(t *testing.T) {
	pool, _ := openStore(t)
	seedPlanBase(t, pool)
	cat := planinfra.NewPackageCatalog(sqlcgen.New(pool))
	rows, err := cat.ListPublic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "basic" || rows[1].ID != "pro" {
		t.Fatalf("public = %+v", rows)
	}
	if !rows[1].AllowSelfService || rows[1].UpgradeGroup == nil || *rows[1].UpgradeGroup != "g1" {
		t.Fatalf("pro flags = %+v", rows[1])
	}
}
