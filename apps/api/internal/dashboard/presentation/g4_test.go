package presentation_test

// Fase 4 (agen E1/G4) integration tests: admin dashboard aggregates and
// metrics. Full server on isolated sqlite :memory: DBs.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	_ "modernc.org/sqlite"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/httpserver"
	"boilerplate/api/internal/shared/config"
)

var d4dbSeq atomic.Int64

type d4jar struct {
	cookies map[string]string
}

func d4App(t *testing.T) (*fiber.App, *sql.DB, *sqlcgen.Queries) {
	t.Helper()
	cfg := config.Config{
		Env:                      "test",
		Port:                     "0",
		DBDriver:                 "sqlite",
		SessionSecret:            "test-secret-0123456789abcdef",
		SessionTTL:               time.Hour,
		AdminUser:                "admin",
		AdminPass:                "admin123",
		UploadDir:                filepath.Join(t.TempDir(), "uploads"),
		JWTSecret:                "test-jwt-secret-min-32-chars-ok!",
		JWTAccessTTLMinutes:      15,
		RefreshTTLDays:           30,
		TZ:                       "Asia/Jakarta",
		SettingsEncKey:           "test-settings-enc-key-min-32!",
		SchedulerIntervalSeconds: 60,
	}
	name := fmt.Sprintf("file:g4dash_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		d4dbSeq.Add(1))
	pool, err := sql.Open("sqlite", name)
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, "sqlite"); err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(pool)
	if err := db.SeedBillingAdmin(ctx, q, cfg.AdminUser, cfg.AdminPass); err != nil {
		t.Fatal(err)
	}
	app, err := httpserver.NewWithFS(cfg, pool, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return app, pool, q
}

func d4do(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("request %s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func d4body(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("response is not JSON %q: %v", string(raw), err)
	}
	return out
}

func d4jsonReq(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func d4csrf(t *testing.T, app *fiber.App, j *d4jar) {
	t.Helper()
	resp := d4do(t, app, d4jsonReq(t, http.MethodGet, "/api/auth/csrf", ""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/csrf = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
}

func d4unsafe(t *testing.T, j *d4jar, method, path, body string) *http.Request {
	t.Helper()
	req := d4jsonReq(t, method, path, body)
	for name, val := range j.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
	req.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	return req
}

func d4bearer(t *testing.T, token, method, path, body string) *http.Request {
	t.Helper()
	req := d4jsonReq(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func d4authed(t *testing.T, j *d4jar, token, method, path, body string) *http.Request {
	t.Helper()
	req := d4unsafe(t, j, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func d4login(t *testing.T, app *fiber.App, j *d4jar, username, password string) string {
	t.Helper()
	resp := d4do(t, app, d4unsafe(t, j, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`))
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
	body := d4body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d %v", username, resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login missing access_token: %v", body)
	}
	return token
}

func d4postJSON(t *testing.T, app *fiber.App, j *d4jar, token, method, path, body string, want int) map[string]any {
	t.Helper()
	resp := d4do(t, app, d4authed(t, j, token, method, path, body))
	out := d4body(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("%s %s = %d %v, want %d", method, path, resp.StatusCode, out, want)
	}
	data, _ := out["data"].(map[string]any)
	return data
}

func d4getJSON(t *testing.T, app *fiber.App, token, path string, want int) map[string]any {
	t.Helper()
	resp := d4do(t, app, d4bearer(t, token, http.MethodGet, path, ""))
	out := d4body(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("GET %s = %d %v, want %d", path, resp.StatusCode, out, want)
	}
	return out
}

// TestG4Dashboard seeds accounts, invoices, payments, and sessions, then
// asserts the exact E3-contract shape and aggregates.
func TestG4Dashboard(t *testing.T) {
	app, pool, _ := d4App(t)
	j := &d4jar{cookies: map[string]string{}}
	d4csrf(t, app, j)
	admin := d4login(t, app, j, "admin", "admin123")

	cust := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/customers",
		`{"name":"Dash Customer"}`, http.StatusCreated)
	custID := cust["id"].(string)
	pkg := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"Dash Pack","price":50000,"period":"monthly"}`, http.StatusCreated)
	pkgID := pkg["id"].(string)
	mkPPP := func(username string) string {
		t.Helper()
		ppp := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts",
			`{"customer_id":"`+custID+`","package_id":"`+pkgID+`","username":"`+username+`","password":"dashpass1"}`,
			http.StatusCreated)
		return ppp["id"].(string)
	}
	a1, a2, a3, a4 := mkPPP("dashu1"), mkPPP("dashu2"), mkPPP("dashu3"), mkPPP("dashu4")
	d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+a3+"/isolate", "", http.StatusOK)
	d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+a4+"/disable", "", http.StatusOK)

	mkInv := func(ppp string, price int, due string) map[string]any {
		t.Helper()
		return d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices",
			fmt.Sprintf(`{"pppoe_account_id":"%s","items":[{"description":"d","qty":1,"price":%d}],"due_date":"%s"}`,
				ppp, price, due),
			http.StatusCreated)
	}
	payNum := mkInv(a1, 50000, "2099-01-01")
	d4postJSON(t, app, j, admin, http.MethodPost,
		"/api/v1/admin/invoices/"+payNum["id"].(string)+"/manual-payment", `{"amount":0}`, http.StatusOK)
	mkInv(a1, 30000, "2020-01-01") // overdue, unpaid
	mkInv(a2, 20000, "2099-01-01") // not due, unpaid
	oldInv := mkInv(a2, 10000, "2099-01-01")

	// One payment parked in the previous month (excluded from MTD but
	// present in recent payments).
	now := time.Now().UTC()
	lastMonth := now.AddDate(0, -1, 0)
	oldPaidAt := time.Date(lastMonth.Year(), lastMonth.Month(), 15, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := pool.Exec(`INSERT INTO payments (id, invoice_id, amount, method, paid_at, created_at)
VALUES ('pay-old', $1, 10000, 'manual', $2, $2)`, oldInv["id"].(string), oldPaidAt); err != nil {
		t.Fatal(err)
	}

	// One online + one stopped session (direct SQL: no RADIUS traffic).
	nas := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/nas",
		`{"name":"Dash NAS","ip":"10.30.30.1","secret":"dashsecret1","coa_port":3799}`, http.StatusCreated)
	nasID := nas["id"].(string)
	stamp := now.Format(time.RFC3339)
	if _, err := pool.Exec(`INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id, started_at, stopped_at, bytes_in, bytes_out, updated_at)
VALUES ('rs-on','sess-on',$1,$2,$3,NULL,10,20,$3)`, a1, nasID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(`INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id, started_at, stopped_at, bytes_in, bytes_out, updated_at)
VALUES ('rs-off','sess-off',$1,$2,$3,$3,10,20,$3)`, a2, nasID, stamp); err != nil {
		t.Fatal(err)
	}

	out := d4getJSON(t, app, admin, "/api/v1/admin/dashboard", http.StatusOK)
	data, _ := out["data"].(map[string]any)
	if data == nil {
		t.Fatalf("dashboard has no data: %v", out)
	}
	num := func(key string) int64 {
		t.Helper()
		f, ok := data[key].(float64)
		if !ok {
			t.Fatalf("dashboard.%s = %v, want number", key, data[key])
		}
		return int64(f)
	}
	if got := num("revenue_mtd"); got != 50000 {
		t.Fatalf("revenue_mtd = %d, want 50000", got)
	}
	if got := num("outstanding_total"); got != 60000 {
		t.Fatalf("outstanding_total = %d, want 60000", got)
	}
	if got := num("overdue_count"); got != 1 {
		t.Fatalf("overdue_count = %d, want 1", got)
	}
	accounts, _ := data["accounts"].(map[string]any)
	if accounts["active"] != float64(2) || accounts["isolated"] != float64(1) || accounts["disabled"] != float64(1) {
		t.Fatalf("accounts = %v, want {2,1,1}", accounts)
	}
	if got := num("online_sessions"); got != 1 {
		t.Fatalf("online_sessions = %d, want 1", got)
	}
	recent, _ := data["recent_payments"].([]any)
	if len(recent) != 2 {
		t.Fatalf("recent_payments len = %d, want 2: %v", len(recent), data["recent_payments"])
	}
	first, _ := recent[0].(map[string]any)
	if first["number"] != payNum["number"] || first["amount"] != float64(50000) {
		t.Fatalf("recent[0] = %v, want paid invoice 50000", first)
	}
	if _, ok := first["id"].(string); !ok {
		t.Fatalf("recent[0] has no id: %v", first)
	}
	if _, ok := first["paid_at"].(string); !ok {
		t.Fatalf("recent[0] has no paid_at: %v", first)
	}
	second, _ := recent[1].(map[string]any)
	if second["number"] != oldInv["number"] || second["paid_at"] != oldPaidAt {
		t.Fatalf("recent[1] = %v, want old payment", second)
	}

	// Auth: 401 without token.
	resp := d4do(t, app, d4jsonReq(t, http.MethodGet, "/api/v1/admin/dashboard", ""))
	d4body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}

// TestG4Metrics asserts the metrics shape: uptime, db_ok, scheduler
// jobs reuse, pending QRIS attempts, and trailing-24h failures.
func TestG4Metrics(t *testing.T) {
	app, pool, _ := d4App(t)
	j := &d4jar{cookies: map[string]string{}}
	d4csrf(t, app, j)
	admin := d4login(t, app, j, "admin", "admin123")

	cust := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/customers",
		`{"name":"Metrics Customer"}`, http.StatusCreated)
	pkg := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"Metrics Pack","price":50000,"period":"monthly"}`, http.StatusCreated)
	ppp := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts",
		`{"customer_id":"`+cust["id"].(string)+`","package_id":"`+pkg["id"].(string)+`","username":"metru1","password":"metrpass1"}`,
		http.StatusCreated)
	inv := d4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices",
		`{"pppoe_account_id":"`+ppp["id"].(string)+`","items":[{"description":"m","qty":1,"price":50000}],"due_date":"2099-01-01"}`,
		http.StatusCreated)
	invID := inv["id"].(string)

	now := time.Now().UTC()
	mkAttempt := func(id, krisID, status, updated string) {
		t.Helper()
		exp := now.Add(15 * time.Minute).Format(time.RFC3339)
		if _, err := pool.Exec(`INSERT INTO payment_attempts
(id, invoice_id, kriskris_invoice_id, base_amount, unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at)
VALUES ($1,$2,$3,50000,42,50042,'qr',$4,$5,NULL,$6,$7)`,
			id, invID, krisID, status, exp, now.Format(time.RFC3339), updated); err != nil {
			t.Fatal(err)
		}
	}
	mkAttempt("att-pending", "k-pending", "pending", now.Format(time.RFC3339))
	mkAttempt("att-failed-new", "k-failed-new", "failed", now.Format(time.RFC3339))
	mkAttempt("att-failed-old", "k-failed-old", "failed", now.Add(-25*time.Hour).Format(time.RFC3339))

	out := d4getJSON(t, app, admin, "/api/v1/admin/metrics", http.StatusOK)
	data, _ := out["data"].(map[string]any)
	if data == nil {
		t.Fatalf("metrics has no data: %v", out)
	}
	uptime, ok := data["uptime_seconds"].(float64)
	if !ok || uptime < 0 {
		t.Fatalf("uptime_seconds = %v, want number >= 0", data["uptime_seconds"])
	}
	if data["db_ok"] != true {
		t.Fatalf("db_ok = %v, want true", data["db_ok"])
	}
	sched, _ := data["scheduler"].(map[string]any)
	jobs, _ := sched["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("scheduler.jobs = %v, want 2 jobs", sched)
	}
	names := map[string]map[string]any{}
	for _, item := range jobs {
		m, _ := item.(map[string]any)
		name, _ := m["job"].(string)
		names[name] = m
	}
	for _, want := range []string{"generate", "isolate"} {
		m, ok := names[want]
		if !ok {
			t.Fatalf("scheduler.jobs missing %q: %v", want, jobs)
		}
		// Never ran in tests: zero shape mirrors the status endpoint.
		if m["last_ok"] != false || m["runs"] != float64(0) || m["last_error"] != "" || m["last_run"] != nil {
			t.Fatalf("job %q = %v, want zero shape", want, m)
		}
	}
	if data["kriskris_pending"] != float64(1) {
		t.Fatalf("kriskris_pending = %v, want 1", data["kriskris_pending"])
	}
	if data["webhooks_failed_24h"] != float64(1) {
		t.Fatalf("webhooks_failed_24h = %v, want 1", data["webhooks_failed_24h"])
	}

	// Auth: 401 without token.
	resp := d4do(t, app, d4jsonReq(t, http.MethodGet, "/api/v1/admin/metrics", ""))
	d4body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}
