package application_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	reportsapp "boilerplate/api/internal/reports/application"
	reportsdomain "boilerplate/api/internal/reports/domain"
	reportsinfra "boilerplate/api/internal/reports/infrastructure"
)

func openReports(t *testing.T) (*sql.DB, *reportsapp.Service) {
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
	svc := reportsapp.NewService(reportsinfra.NewStore(sqlcgen.New(pool)))
	svc.SetNow(func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) })
	return pool, svc
}

func seedReports(t *testing.T, pool *sql.DB) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ts := "2026-11-07T12:00:00Z"
	exec(`INSERT INTO customers (id, name, created_at, updated_at) VALUES
		('cust1', 'Budi', $1, $1), ('cust2', 'Siti', $1, $1)`, ts)
	exec(`INSERT INTO packages (id, name, price, period, created_at, updated_at)
		VALUES ('pkg1', 'Basic', 50000, 'monthly', $1, $1)`, ts)
	exec(`INSERT INTO nas (id, name, ip, secret, created_at, updated_at)
		VALUES ('nas1', 'N1', '10.0.0.1', 's', $1, $1)`, ts)
	exec(`INSERT INTO pppoe_accounts (id, customer_id, package_id, username, password, status, created_at, updated_at) VALUES
		('acc1', 'cust1', 'pkg1', 'u1', 'pw1234', 'active', '2026-10-10T00:00:00Z', $1),
		('acc2', 'cust1', 'pkg1', 'u2', 'pw1234', 'active', '2026-11-03T00:00:00Z', $1),
		('acc3', 'cust2', 'pkg1', 'u3', 'pw1234', 'disabled', '2026-09-01T00:00:00Z', '2026-11-05T00:00:00Z'),
		('acc4', 'cust2', 'pkg1', 'u4', 'pw1234', 'active', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`, ts)
	exec(`INSERT INTO invoices (id, number, pppoe_account_id, customer_id, total, due_date, status, issued_at, paid_at, created_at, updated_at) VALUES
		('inv-p1', 'INV-P1', 'acc1', 'cust1', 50000, '2026-11-01', 'paid', '2026-10-25T00:00:00Z', '2026-11-05T10:00:00Z', '2026-10-25T00:00:00Z', $1),
		('inv-p2', 'INV-P2', 'acc2', 'cust1', 30000, '2026-11-01', 'paid', '2026-10-25T00:00:00Z', '2026-11-05T11:00:00Z', '2026-10-25T00:00:00Z', $1),
		('inv-p3', 'INV-P3', 'acc1', 'cust1', 50000, '2026-10-01', 'paid', '2026-09-25T00:00:00Z', '2026-10-20T10:00:00Z', '2026-09-25T00:00:00Z', $1),
		('inv-u1', 'INV-U1', 'acc1', 'cust1', 50000, '2026-11-01', 'unpaid', '2026-10-25T00:00:00Z', NULL, '2026-10-25T00:00:00Z', $1),
		('inv-u2', 'INV-U2', 'acc4', 'cust2', 75000, '2026-11-20', 'unpaid', '2026-10-25T00:00:00Z', NULL, '2026-10-25T00:00:00Z', $1)`, ts)
	// Cash basis: base rows + one kriskris unique-code row (other income).
	exec(`INSERT INTO payments (id, invoice_id, amount, method, paid_at, created_at) VALUES
		('pay1', 'inv-p1', 50000, 'manual', '2026-11-05T10:00:00Z', '2026-11-05T10:00:00Z'),
		('pay2', 'inv-p2', 30000, 'kriskris', '2026-11-05T11:00:00Z', '2026-11-05T11:00:00Z'),
		('pay2c', 'inv-p2', 42, 'kriskris', '2026-11-05T11:00:00Z', '2026-11-05T11:00:00Z'),
		('pay3', 'inv-p3', 50000, 'manual', '2026-10-20T10:00:00Z', '2026-10-20T10:00:00Z')`)
	exec(`INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id, started_at, stopped_at, bytes_in, bytes_out, updated_at) VALUES
		('rs1', 's1', 'acc1', 'nas1', '2026-11-05T08:00:00Z', '2026-11-05T09:00:00Z', 1000, 2000, $1),
		('rs2', 's2', 'acc1', 'nas1', '2026-11-06T08:00:00Z', '2026-11-06T09:00:00Z', 3000, 4000, $1),
		('rs3', 's3', 'acc2', 'nas1', '2026-10-20T08:00:00Z', '2026-10-20T09:00:00Z', 7000, 8000, $1)`, ts)
}

func TestRevenueDayAndMonth(t *testing.T) {
	pool, svc := openReports(t)
	seedReports(t, pool)
	ctx := context.Background()

	rev, err := svc.Revenue(ctx, "2026-11-01", "2026-11-07", "day")
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Points) != 7 {
		t.Fatalf("points = %d", len(rev.Points))
	}
	// 50000 + 30000 + 42 unique code on Nov 5.
	if rev.Points[4].Period != "2026-11-05" || rev.Points[4].Total != 80042 {
		t.Fatalf("nov5 = %+v", rev.Points[4])
	}
	if rev.Total != 80042 {
		t.Fatalf("total = %d", rev.Total)
	}
	if rev.Points[0].Total != 0 || rev.Points[6].Period != "2026-11-07" {
		t.Fatalf("zero-fill = %+v", rev.Points)
	}

	rev, err = svc.Revenue(ctx, "2026-10-01", "2026-11-30", "month")
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Points) != 2 || rev.Points[0].Period != "2026-10" || rev.Points[1].Period != "2026-11" {
		t.Fatalf("months = %+v", rev.Points)
	}
	if rev.Points[0].Total != 50000 || rev.Points[1].Total != 80042 || rev.Total != 130042 {
		t.Fatalf("month totals = %+v total=%d", rev.Points, rev.Total)
	}

	if _, err := svc.Revenue(ctx, "", "", "year"); err != reportsdomain.ErrBadGroupBy {
		t.Fatalf("group = %v", err)
	}
	if _, err := svc.Revenue(ctx, "2026-11-07", "2026-11-01", "day"); err != reportsdomain.ErrBadRange {
		t.Fatalf("inverted = %v", err)
	}
	if _, err := svc.Revenue(ctx, "junk", "", "day"); err != reportsdomain.ErrBadRange {
		t.Fatalf("junk = %v", err)
	}
}

func TestUsagePointsAndTotals(t *testing.T) {
	pool, svc := openReports(t)
	seedReports(t, pool)
	u, err := svc.Usage(context.Background(), "2026-11-05", "2026-11-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Points) != 2 {
		t.Fatalf("points = %d", len(u.Points))
	}
	if u.Points[0].Date != "2026-11-05" || u.Points[0].BytesIn != 1000 || u.Points[0].BytesOut != 2000 {
		t.Fatalf("day1 = %+v", u.Points[0])
	}
	if u.Points[1].Date != "2026-11-06" || u.Points[1].BytesIn != 3000 || u.Points[1].BytesOut != 4000 {
		t.Fatalf("day2 = %+v", u.Points[1])
	}
	if u.Totals.BytesIn != 4000 || u.Totals.BytesOut != 6000 {
		t.Fatalf("totals = %+v", u.Totals)
	}
}

func TestReceivables(t *testing.T) {
	pool, svc := openReports(t)
	seedReports(t, pool)
	r, err := svc.Receivables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Invoices) != 2 || r.Total != 125000 {
		t.Fatalf("receivables = %+v total=%d", r.Invoices, r.Total)
	}
	first := r.Invoices[0]
	if first.ID != "inv-u1" || first.CustomerName != "Budi" || first.DaysOverdue != 6 {
		t.Fatalf("first = %+v", first)
	}
	if second := r.Invoices[1]; second.DueDate != "2026-11-20" || second.DaysOverdue != 0 {
		t.Fatalf("second = %+v", second)
	}
}

func TestGrowth(t *testing.T) {
	pool, svc := openReports(t)
	seedReports(t, pool)
	g, err := svc.Growth(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Points) != 2 {
		t.Fatalf("points = %d", len(g.Points))
	}
	oct, nov := g.Points[0], g.Points[1]
	if oct.Period != "2026-10" || oct.New != 1 || oct.Churn != 0 || oct.Active != 3 {
		t.Fatalf("oct = %+v", oct)
	}
	if nov.Period != "2026-11" || nov.New != 1 || nov.Churn != 1 || nov.Active != 3 {
		t.Fatalf("nov = %+v", nov)
	}
	if _, err := svc.Growth(context.Background(), 0); err != nil {
		t.Fatalf("default months: %v", err)
	}
	if _, err := svc.Growth(context.Background(), 99); err != reportsdomain.ErrBadMonths {
		t.Fatalf("months = %v", err)
	}
}
