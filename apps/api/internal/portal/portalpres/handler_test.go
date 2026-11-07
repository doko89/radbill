package portalpres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	_ "modernc.org/sqlite"
)

// Self-contained sqlite harness: minimal DDL mirroring platform schema
// column names (no G1 imports; the production queries only rely on the
// shared contract, verified by these tests running the real SQL).

var testDBSeq atomic.Int64

const testDDL = `
CREATE TABLE customers (id TEXT PRIMARY KEY, account_id TEXT, name TEXT NOT NULL, phone TEXT, address TEXT, notes TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE packages (id TEXT PRIMARY KEY, name TEXT NOT NULL, price BIGINT NOT NULL, period TEXT NOT NULL, rate_limit TEXT, address_pool TEXT, validity_seconds BIGINT, online_seconds_limit BIGINT, quota_bytes BIGINT, quota_reset TEXT NOT NULL DEFAULT 'none', fup_threshold_bytes BIGINT, fup_rate_limit TEXT, on_exhausted TEXT NOT NULL DEFAULT 'reject', is_public INTEGER NOT NULL DEFAULT 1, allow_self_service INTEGER NOT NULL DEFAULT 1, upgrade_group TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE nas (id TEXT PRIMARY KEY, name TEXT NOT NULL, ip TEXT NOT NULL, secret TEXT NOT NULL, coa_port INTEGER NOT NULL DEFAULT 3799, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE pppoe_accounts (id TEXT PRIMARY KEY, customer_id TEXT NOT NULL, package_id TEXT NOT NULL, nas_id TEXT, username TEXT NOT NULL, password TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', expires_at TEXT, static_ip TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE radius_sessions (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, pppoe_account_id TEXT NOT NULL, nas_id TEXT NOT NULL, framed_ip TEXT, started_at TEXT NOT NULL, stopped_at TEXT, bytes_in BIGINT NOT NULL DEFAULT 0, bytes_out BIGINT NOT NULL DEFAULT 0, terminate_cause TEXT, updated_at TEXT NOT NULL);
CREATE TABLE usage_counters (pppoe_account_id TEXT NOT NULL, period_start TEXT NOT NULL, bytes_in BIGINT NOT NULL DEFAULT 0, bytes_out BIGINT NOT NULL DEFAULT 0, online_seconds BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (pppoe_account_id, period_start));
CREATE TABLE invoices (id TEXT PRIMARY KEY, number TEXT NOT NULL, pppoe_account_id TEXT NOT NULL, customer_id TEXT NOT NULL, total BIGINT NOT NULL, due_date TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'unpaid', issued_at TEXT NOT NULL, paid_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE invoice_items (id TEXT PRIMARY KEY, invoice_id TEXT NOT NULL, description TEXT NOT NULL, qty BIGINT NOT NULL DEFAULT 1, price BIGINT NOT NULL);
CREATE TABLE payments (id TEXT PRIMARY KEY, invoice_id TEXT NOT NULL, amount BIGINT NOT NULL, method TEXT NOT NULL, attempt_id TEXT, paid_at TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE payment_attempts (id TEXT PRIMARY KEY, invoice_id TEXT NOT NULL, kriskris_invoice_id TEXT NOT NULL, base_amount BIGINT NOT NULL, unique_code BIGINT NOT NULL, amount BIGINT NOT NULL, qr_payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', expires_at TEXT NOT NULL, paid_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
`

func openSeededDB(t *testing.T) *sql.DB {
	t.Helper()
	n := testDBSeq.Add(1)
	pool, err := sql.Open("sqlite", fmt.Sprintf("file:portal_test_%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)", n))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	if _, err := pool.Exec(testDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	seedPortal(t, pool)
	return pool
}

func seedPortal(t *testing.T, pool *sql.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	ts := now.Format(time.RFC3339)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO customers (id, name, phone, created_at, updated_at) VALUES
		('cust-a', 'Budi', '0811', $1, $1), ('cust-b', 'Siti', '0822', $1, $1)`, ts)
	exec(`INSERT INTO packages (id, name, price, period, rate_limit, validity_seconds, online_seconds_limit, quota_bytes, quota_reset, on_exhausted, created_at, updated_at) VALUES
		('pkg-a', 'Hemat 10M', 50000, 'monthly', '10M/10M', 2592000, 36000, 10000000000, 'monthly', 'throttle', $1, $1),
		('pkg-b', 'Basic', 30000, 'monthly', NULL, NULL, NULL, NULL, 'none', 'reject', $1, $1)`, ts)
	exec(`INSERT INTO nas (id, name, ip, secret, created_at, updated_at) VALUES ('nas-1', 'N1', '10.0.0.1', 's3cr3t', $1, $1)`, ts)
	exec(`INSERT INTO pppoe_accounts (id, customer_id, package_id, nas_id, username, password, status, expires_at, created_at, updated_at) VALUES
		('acc-a1', 'cust-a', 'pkg-a', 'nas-1', 'user-a1', 'pw-secret-1', 'active', $1, $2, $2),
		('acc-a2', 'cust-a', 'pkg-a', 'nas-1', 'user-a2', 'pw-secret-2', 'isolated', $3, $2, $2),
		('acc-b1', 'cust-b', 'pkg-b', 'nas-1', 'user-b1', 'pw-secret-3', 'active', NULL, $2, $2)`,
		now.Add(20*24*time.Hour).Format(time.RFC3339), ts, now.Add(-2*24*time.Hour).Format(time.RFC3339))
	exec(`INSERT INTO usage_counters (pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds) VALUES
		('acc-a1', $1, 1000000000, 2000000000, 3600),
		('acc-a2', $1, 500000000, 500000000, 1800),
		('acc-b1', $1, 777000000000, 111000000000, 99999)`, now.AddDate(0, 0, -5).Format(time.RFC3339))
	exec(`INSERT INTO invoices (id, number, pppoe_account_id, customer_id, total, due_date, status, issued_at, paid_at, created_at, updated_at) VALUES
		('inv-a1', 'INV-A1', 'acc-a1', 'cust-a', 50000, '2026-11-20', 'unpaid', $1, NULL, $1, $2),
		('inv-a2', 'INV-A2', 'acc-a2', 'cust-a', 75000, '2026-11-10', 'unpaid', $1, NULL, $1, $2),
		('inv-a3', 'INV-A3', 'acc-a1', 'cust-a', 50000, '2026-10-10', 'paid', $1, $1, $1, $2),
		('inv-b1', 'INV-B1', 'acc-b1', 'cust-b', 30000, '2026-11-12', 'unpaid', $1, NULL, $1, $2)`,
		now.AddDate(0, 0, -6).Format(time.RFC3339), ts)
	exec(`INSERT INTO invoice_items (id, invoice_id, description, qty, price) VALUES
		('item-1', 'inv-a1', 'Paket Hemat 10M', 1, 50000),
		('item-2', 'inv-a1', 'Biaya admin', 1, 0),
		('item-3', 'inv-b1', 'Paket Basic', 1, 30000)`)
	exec(`INSERT INTO payments (id, invoice_id, amount, method, paid_at, created_at) VALUES
		('pay-a3', 'inv-a3', 50000, 'manual', $1, $1)`, ts)
	exec(`INSERT INTO payment_attempts (id, invoice_id, kriskris_invoice_id, base_amount, unique_code, amount, qr_payload, status, expires_at, created_at, updated_at) VALUES
		('att-a1', 'inv-a1', 'kk-1', 50000, 42, 50042, 'EMV-QR-PAYLOAD-A1', 'pending', $1, $2, $2),
		('att-a1-old', 'inv-a1', 'kk-0', 50000, 11, 50011, 'EMV-OLD', 'expired', $1, $3, $3)`,
		now.Add(15*time.Minute).Format(time.RFC3339), ts, now.Add(-time.Hour).Format(time.RFC3339))
	// Sessions: a1 has two on D-1 and one on D-3; b1 has a huge one on D-1 (must not leak).
	day1 := now.AddDate(0, 0, -1)
	day3 := now.AddDate(0, 0, -3)
	s := func(id, acc string, start time.Time, dur time.Duration, in, out int64, stopped bool) {
		t.Helper()
		stop := ""
		if stopped {
			stop = start.Add(dur).Format(time.RFC3339)
		}
		var stopArg any
		if stop == "" {
			stopArg = nil
		} else {
			stopArg = stop
		}
		exec(`INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id, started_at, stopped_at, bytes_in, bytes_out, updated_at)
			VALUES ($1, $2, $3, 'nas-1', $4, $5, $6, $7, $8)`,
			id, "sess-"+id, acc, start.Format(time.RFC3339), stopArg, in, out, ts)
	}
	s("rs-1", "acc-a1", day1.Add(8*time.Hour), time.Hour, 1000, 2000, true)
	s("rs-2", "acc-a1", day1.Add(10*time.Hour), 30*time.Minute, 3000, 4000, true)
	s("rs-3", "acc-a1", day3.Add(9*time.Hour), 10*time.Minute, 100, 200, true)
	s("rs-4", "acc-b1", day1.Add(8*time.Hour), 2*time.Hour, 999999999, 888888888, true)
}

func testApp(pool *sql.DB, custID *string) *fiber.App {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if custID != nil {
			c.Locals(customerLocalsKey, *custID)
		}
		return c.Next()
	})
	h := NewHandler(pool)
	app.Get("/api/v1/portal/summary", h.Summary)
	app.Get("/api/v1/portal/invoices", h.Invoices)
	app.Get("/api/v1/portal/invoices/:id", h.InvoiceDetail)
	app.Get("/api/v1/portal/usage", h.Usage)
	return app
}

func doGet(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, body
}

func decodeData(t *testing.T, body []byte, out any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("envelope: %v (%s)", err, body)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("data: %v (%s)", err, env.Data)
	}
}

func strptr(s string) *string { return &s }

func TestSummaryMultiAccount(t *testing.T) {
	pool := openSeededDB(t)
	code, body := doGet(t, testApp(pool, strptr("cust-a")), "/api/v1/portal/summary")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %s", code, body)
	}
	var sum SummaryDTO
	decodeData(t, body, &sum)

	if sum.Customer == nil || sum.Customer.Name != "Budi" {
		t.Fatalf("customer = %+v", sum.Customer)
	}
	if len(sum.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(sum.Accounts))
	}
	if sum.Accounts[0].Username != "user-a1" || sum.Accounts[1].Username != "user-a2" {
		t.Fatalf("accounts order = %v", sum.Accounts)
	}
	if sum.DefaultAccountID == nil || *sum.DefaultAccountID != "acc-a1" {
		t.Fatalf("default = %+v, want acc-a1", sum.DefaultAccountID)
	}
	if sum.Accounts[0].Status != "active" || sum.Accounts[1].Status != "isolated" {
		t.Fatalf("statuses = %v", sum.Accounts)
	}
	if sum.Accounts[0].ExpiresAt == nil || sum.Accounts[1].ExpiresAt == nil {
		t.Fatalf("expires missing: %+v", sum.Accounts)
	}
	if sum.Accounts[0].Package.Name != "Hemat 10M" || sum.Accounts[0].Package.Price != 50000 {
		t.Fatalf("package = %+v", sum.Accounts[0].Package)
	}
	// Default account counters: 1e9+2e9 used of 10e9 quota, 3600s of 36000s.
	if sum.Quota.UsedBytes != 3000000000 {
		t.Fatalf("quota used = %d", sum.Quota.UsedBytes)
	}
	if sum.Quota.LimitBytes == nil || *sum.Quota.LimitBytes != 10000000000 {
		t.Fatalf("quota limit = %+v", sum.Quota.LimitBytes)
	}
	if sum.Quota.RemainingBytes == nil || *sum.Quota.RemainingBytes != 7000000000 {
		t.Fatalf("quota remaining = %+v", sum.Quota.RemainingBytes)
	}
	if sum.OnlineTime.UsedSeconds != 3600 || sum.OnlineTime.LimitSeconds == nil || *sum.OnlineTime.LimitSeconds != 36000 {
		t.Fatalf("online = %+v", sum.OnlineTime)
	}
	if sum.OnlineTime.RemainingSeconds == nil || *sum.OnlineTime.RemainingSeconds != 32400 {
		t.Fatalf("online remaining = %+v", sum.OnlineTime.RemainingSeconds)
	}
	// Nearest due: INV-A2 (2026-11-10), before INV-A1 (2026-11-20).
	if sum.Due == nil || sum.Due.Number != "INV-A2" || sum.Due.Total != 75000 {
		t.Fatalf("due = %+v", sum.Due)
	}
	if sum.UnpaidCount != 2 || sum.UnpaidTotal != 125000 {
		t.Fatalf("unpaid = %d / %d", sum.UnpaidCount, sum.UnpaidTotal)
	}
}

func TestInvoicesScoped(t *testing.T) {
	pool := openSeededDB(t)
	code, body := doGet(t, testApp(pool, strptr("cust-a")), "/api/v1/portal/invoices")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var env struct {
		Data  []InvoiceDTO `json:"data"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 3 || len(env.Data) != 3 {
		t.Fatalf("total = %d (%d rows), want 3 own invoices", env.Total, len(env.Data))
	}
	seen := map[string]bool{}
	for _, inv := range env.Data {
		seen[inv.Number] = true
		if inv.PPPoEUsername == "" || inv.PackageName == "" {
			t.Fatalf("labels missing: %+v", inv)
		}
	}
	if !seen["INV-A1"] || !seen["INV-A2"] || !seen["INV-A3"] || seen["INV-B1"] {
		t.Fatalf("leak or missing: %v", seen)
	}
	// Pending attempt attached to INV-A1 only (newest pending, not expired).
	for _, inv := range env.Data {
		if inv.Number == "INV-A1" {
			if inv.PendingAttempt == nil || inv.PendingAttempt.ID != "att-a1" || inv.PendingAttempt.Amount != 50042 {
				t.Fatalf("pending = %+v", inv.PendingAttempt)
			}
			if inv.PendingAttempt.QRPayload != "EMV-QR-PAYLOAD-A1" {
				t.Fatalf("qr = %q", inv.PendingAttempt.QRPayload)
			}
		} else if inv.PendingAttempt != nil {
			t.Fatalf("%s has unexpected pending %+v", inv.Number, inv.PendingAttempt)
		}
	}

	code, body = doGet(t, testApp(pool, strptr("cust-b")), "/api/v1/portal/invoices")
	if code != http.StatusOK {
		t.Fatalf("B status = %d", code)
	}
	var envB struct {
		Data  []InvoiceDTO `json:"data"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(body, &envB); err != nil {
		t.Fatalf("decode B: %v", err)
	}
	if envB.Total != 1 || len(envB.Data) != 1 || envB.Data[0].Number != "INV-B1" {
		t.Fatalf("B invoices = %+v", envB)
	}
}

func TestInvoiceDetailIDOR(t *testing.T) {
	pool := openSeededDB(t)
	appA := testApp(pool, strptr("cust-a"))

	// Own invoice: items + payments + pending attempt.
	code, body := doGet(t, appA, "/api/v1/portal/invoices/inv-a1")
	if code != http.StatusOK {
		t.Fatalf("own status = %d (%s)", code, body)
	}
	var det InvoiceDetailDTO
	decodeData(t, body, &det)
	if det.Number != "INV-A1" || len(det.Items) != 2 || det.PendingAttempt == nil {
		t.Fatalf("detail = %+v", det)
	}
	if len(det.Payments) != 0 {
		t.Fatalf("payments = %+v, want none", det.Payments)
	}
	code, body = doGet(t, appA, "/api/v1/portal/invoices/inv-a3")
	if code != http.StatusOK {
		t.Fatalf("a3 status = %d", code)
	}
	var det3 InvoiceDetailDTO
	decodeData(t, body, &det3)
	if len(det3.Payments) != 1 || det3.Payments[0].Method != "manual" {
		t.Fatalf("a3 payments = %+v", det3.Payments)
	}

	// Another customer's invoice is 404 (not 403: no existence oracle).
	if code, _ := doGet(t, appA, "/api/v1/portal/invoices/inv-b1"); code != http.StatusNotFound {
		t.Fatalf("cross-customer status = %d, want 404", code)
	}
	if code, _ := doGet(t, appA, "/api/v1/portal/invoices/nope"); code != http.StatusNotFound {
		t.Fatalf("unknown status = %d, want 404", code)
	}

	// Admin/bypass (empty scope) may read any invoice.
	code, body = doGet(t, testApp(pool, nil), "/api/v1/portal/invoices/inv-b1")
	if code != http.StatusOK {
		t.Fatalf("bypass status = %d (%s)", code, body)
	}
}

func TestUsageAggregation(t *testing.T) {
	pool := openSeededDB(t)
	now := time.Now().UTC()
	day1 := now.AddDate(0, 0, -1).Format("2006-01-02")
	day3 := now.AddDate(0, 0, -3).Format("2006-01-02")

	code, body := doGet(t, testApp(pool, strptr("cust-a")), "/api/v1/portal/usage?days=7")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	var u UsageDTO
	decodeData(t, body, &u)
	if u.Days != 7 || len(u.Points) != 7 {
		t.Fatalf("days = %d points = %d", u.Days, len(u.Points))
	}
	byDay := map[string]UsagePointDTO{}
	for _, p := range u.Points {
		byDay[p.Date] = p
	}
	d1, ok := byDay[day1]
	if !ok {
		t.Fatalf("missing day %s in %+v", day1, u.Points)
	}
	// rs-1 + rs-2 only (B's 999999999/888888888 must not leak).
	if d1.BytesIn != 4000 || d1.BytesOut != 6000 {
		t.Fatalf("day1 bytes = %d/%d", d1.BytesIn, d1.BytesOut)
	}
	if d1.OnlineSeconds != 5400 {
		t.Fatalf("day1 online = %d, want 5400", d1.OnlineSeconds)
	}
	d3, ok := byDay[day3]
	if !ok || d3.BytesIn != 100 || d3.BytesOut != 200 || d3.OnlineSeconds != 600 {
		t.Fatalf("day3 = %+v", d3)
	}
	// Zero-filled day.
	var zero UsagePointDTO
	for _, p := range u.Points {
		if p.Date != day1 && p.Date != day3 {
			zero = p
			break
		}
	}
	if zero.BytesIn != 0 || zero.BytesOut != 0 || zero.OnlineSeconds != 0 {
		t.Fatalf("zero day = %+v", zero)
	}
	// Totals from usage_counters over A's accounts only.
	if u.Totals.BytesIn != 1500000000 || u.Totals.BytesOut != 2500000000 || u.Totals.OnlineSeconds != 5400 {
		t.Fatalf("totals = %+v", u.Totals)
	}

	// B sees only their own sessions.
	code, body = doGet(t, testApp(pool, strptr("cust-b")), "/api/v1/portal/usage?days=7")
	if code != http.StatusOK {
		t.Fatalf("B status = %d", code)
	}
	var ub UsageDTO
	decodeData(t, body, &ub)
	found := false
	for _, p := range ub.Points {
		if p.Date == day1 {
			found = true
			if p.BytesIn != 999999999 || p.BytesOut != 888888888 || p.OnlineSeconds != 7200 {
				t.Fatalf("B day1 = %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("B missing day %s", day1)
	}
}

func TestUsageDaysValidation(t *testing.T) {
	pool := openSeededDB(t)
	app := testApp(pool, strptr("cust-a"))
	for _, bad := range []string{"0", "91", "-3", "abc"} {
		if code, _ := doGet(t, app, "/api/v1/portal/usage?days="+bad); code != http.StatusBadRequest {
			t.Fatalf("days=%s status = %d, want 400", bad, code)
		}
	}
	code, body := doGet(t, app, "/api/v1/portal/usage")
	if code != http.StatusOK {
		t.Fatalf("default status = %d", code)
	}
	var u UsageDTO
	decodeData(t, body, &u)
	if u.Days != 30 || len(u.Points) != 30 {
		t.Fatalf("default days = %d points = %d", u.Days, len(u.Points))
	}
}

func TestInvoicesStatusFilter(t *testing.T) {
	pool := openSeededDB(t)
	app := testApp(pool, strptr("cust-a"))
	code, body := doGet(t, app, "/api/v1/portal/invoices?status=paid")
	if code != http.StatusOK {
		t.Fatalf("paid status = %d", code)
	}
	var env struct {
		Data  []InvoiceDTO `json:"data"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 1 || len(env.Data) != 1 || env.Data[0].Number != "INV-A3" {
		t.Fatalf("paid = %+v", env)
	}
	if code, _ := doGet(t, app, "/api/v1/portal/invoices?status=bogus"); code != http.StatusBadRequest {
		t.Fatalf("bogus status = %d, want 400", code)
	}
}

func TestSummaryUnknownCustomer(t *testing.T) {
	pool := openSeededDB(t)
	code, body := doGet(t, testApp(pool, strptr("cust-ghost")), "/api/v1/portal/summary")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var sum SummaryDTO
	decodeData(t, body, &sum)
	if sum.Customer != nil || len(sum.Accounts) != 0 || sum.Due != nil {
		t.Fatalf("ghost summary = %+v", sum)
	}
	if sum.UnpaidCount != 0 || sum.UnpaidTotal != 0 {
		t.Fatalf("ghost unpaid = %d/%d", sum.UnpaidCount, sum.UnpaidTotal)
	}
}

func TestNoPasswordLeak(t *testing.T) {
	pool := openSeededDB(t)
	app := testApp(pool, strptr("cust-a"))
	for _, path := range []string{
		"/api/v1/portal/summary",
		"/api/v1/portal/invoices",
		"/api/v1/portal/invoices/inv-a1",
		"/api/v1/portal/usage?days=7",
	} {
		code, body := doGet(t, app, path)
		if code != http.StatusOK {
			t.Fatalf("%s status = %d", path, code)
		}
		for _, secret := range []string{"pw-secret-1", "pw-secret-2", "pw-secret-3", "s3cr3t"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("%s leaks %q", path, secret)
			}
		}
	}
}
