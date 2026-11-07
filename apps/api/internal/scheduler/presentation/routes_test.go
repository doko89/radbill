package presentation_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"

	jwtinfra "boilerplate/api/internal/jwtauth/infrastructure"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/httpserver"
	"boilerplate/api/internal/shared/config"
)

// g2App builds the full server (Fase 1 + G2 wiring) on a scratch sqlite file.
func g2App(t *testing.T) (*fiber.App, *sqlcgen.Queries, func()) {
	t.Helper()
	cfg := config.Config{
		Env:                 "test",
		Port:                "0",
		DBDriver:            "sqlite",
		SQLitePath:          filepath.Join(t.TempDir(), "app.db"),
		SessionSecret:       "test-secret-0123456789abcdef",
		SessionTTL:          time.Hour,
		AdminUser:           "admin",
		AdminPass:           "admin123",
		UploadDir:           filepath.Join(t.TempDir(), "uploads"),
		JWTSecret:           "test-jwt-secret-min-32-chars-ok!",
		JWTAccessTTLMinutes: 15,
		RefreshTTLDays:      30,
		TZ:                  "Asia/Jakarta",
		// G2.
		SettingsEncKey:           "test-settings-enc-key-min-32!",
		SchedulerIntervalSeconds: 60,
	}
	pool, err := db.Open(cfg.DBDriver, cfg.SQLitePath, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, cfg.DBDriver); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	q := sqlcgen.New(pool)
	if err := db.SeedBillingAdmin(ctx, q, cfg.AdminUser, cfg.AdminPass); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	app, err := httpserver.NewWithFS(cfg, pool, fstest.MapFS{})
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return app, q, func() { pool.Close() }
}

type g2jar struct {
	cookies map[string]string
}

func (j *g2jar) store(resp *http.Response) {
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
}

func g2do(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("request %s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func g2body(t *testing.T, resp *http.Response) map[string]any {
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

func g2jsonReq(t *testing.T, method, path, body string) *http.Request {
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

func g2csrf(t *testing.T, app *fiber.App, j *g2jar) {
	t.Helper()
	resp := g2do(t, app, g2jsonReq(t, http.MethodGet, "/api/auth/csrf", ""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/csrf = %d", resp.StatusCode)
	}
	j.store(resp)
}

func g2unsafe(t *testing.T, j *g2jar, method, path, body string) *http.Request {
	t.Helper()
	req := g2jsonReq(t, method, path, body)
	for name, val := range j.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
	req.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	return req
}

func g2bearer(t *testing.T, token, method, path, body string) *http.Request {
	t.Helper()
	req := g2jsonReq(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func g2login(t *testing.T, app *fiber.App, j *g2jar, username, password string) string {
	t.Helper()
	resp := g2do(t, app, g2unsafe(t, j, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`))
	j.store(resp)
	body := g2body(t, resp)
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

func g2seedUser(t *testing.T, q *sqlcgen.Queries, username, password, customerID string) {
	t.Helper()
	ctx := context.Background()
	hash, err := jwtinfra.NewArgon2Hasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	accountID := "acct-" + username
	if _, err := q.CreateBillingAccount(ctx, sqlcgen.CreateBillingAccountParams{
		ID: accountID, Username: username, PasswordHash: hash,
		Role: "user", Status: "active", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID: customerID, AccountID: sql.NullString{String: accountID, Valid: true},
		Name: "Portal " + username, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestG2SettingsMaskedCRUD(t *testing.T) {
	app, q, done := g2App(t)
	defer done()
	j := &g2jar{cookies: map[string]string{}}
	g2csrf(t, app, j)
	admin := g2login(t, app, j, "admin", "admin123")

	resp := g2do(t, app, g2bearer(t, admin, http.MethodGet, "/api/v1/admin/settings/", ""))
	body := g2body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET settings = %d %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["billing_cycle_mode"] != "activation" || data["grace_days"] != "3" {
		t.Fatalf("seeded settings = %v", data)
	}

	put := g2unsafe(t, j, http.MethodPut, "/api/v1/admin/settings/",
		`{"kriskris_api_key":"s3cr3t-key","grace_days":5,"billing_cycle_mode":"fixed"}`)
	put.Header.Set("Authorization", "Bearer "+admin)
	resp = g2do(t, app, put)
	body = g2body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT settings = %d %v", resp.StatusCode, body)
	}
	data, _ = body["data"].(map[string]any)
	if data["kriskris_api_key"] != "***" {
		t.Fatalf("secret exposed in PUT response: %v", data["kriskris_api_key"])
	}
	if data["grace_days"] != "5" || data["billing_cycle_mode"] != "fixed" {
		t.Fatalf("plain values = %v", data)
	}

	stored, err := q.GetBillingSetting(context.Background(), "kriskris_api_key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.Value, "enc:v1:") {
		t.Fatalf("secret stored plain: %q", stored.Value)
	}

	// Masked resubmit keeps the stored ciphertext.
	put = g2unsafe(t, j, http.MethodPut, "/api/v1/admin/settings/", `{"kriskris_api_key":"***"}`)
	put.Header.Set("Authorization", "Bearer "+admin)
	resp = g2do(t, app, put)
	g2body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT *** = %d", resp.StatusCode)
	}
	again, err := q.GetBillingSetting(context.Background(), "kriskris_api_key")
	if err != nil || again.Value != stored.Value {
		t.Fatalf("masked resubmit overwrote secret")
	}

	// Invalid known-key value is rejected.
	put = g2unsafe(t, j, http.MethodPut, "/api/v1/admin/settings/", `{"grace_days":"bogus"}`)
	put.Header.Set("Authorization", "Bearer "+admin)
	resp = g2do(t, app, put)
	body = g2body(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT bad grace_days = %d %v, want 400", resp.StatusCode, body)
	}
}

func TestG2SchedulerStatus(t *testing.T) {
	app, _, done := g2App(t)
	defer done()
	j := &g2jar{cookies: map[string]string{}}
	g2csrf(t, app, j)
	admin := g2login(t, app, j, "admin", "admin123")

	resp := g2do(t, app, g2bearer(t, admin, http.MethodGet, "/api/v1/admin/scheduler/status", ""))
	body := g2body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	jobs, _ := data["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("jobs = %v", data)
	}
	first, _ := jobs[0].(map[string]any)
	if first["job"] != "generate" || first["runs"] != float64(0) {
		t.Fatalf("generate job = %v", first)
	}
}

func TestG2PasswordChange(t *testing.T) {
	app, _, done := g2App(t)
	defer done()
	j := &g2jar{cookies: map[string]string{}}
	g2csrf(t, app, j)
	admin := g2login(t, app, j, "admin", "admin123")
	refreshBefore := j.cookies["refresh_token"]

	change := g2unsafe(t, j, http.MethodPost, "/api/v1/auth/password/change",
		`{"current_password":"admin123","new_password":"brand-new-admin-pass"}`)
	change.Header.Set("Authorization", "Bearer "+admin)
	resp := g2do(t, app, change)
	body := g2body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change = %d %v", resp.StatusCode, body)
	}

	// Old password fails, new one works.
	resp = g2do(t, app, g2unsafe(t, j, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"admin123"}`))
	g2body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", resp.StatusCode)
	}
	g2login(t, app, j, "admin", "brand-new-admin-pass")

	// Pre-change refresh token was revoked: send it as the ONLY refresh
	// cookie (the jar already holds the post-change one).
	reuse := g2jsonReq(t, http.MethodPost, "/api/v1/auth/refresh", "")
	reuse.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	reuse.AddCookie(&http.Cookie{Name: "csrf_token", Value: j.cookies["csrf_token"]})
	reuse.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshBefore})
	resp = g2do(t, app, reuse)
	g2body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old refresh = %d, want 401", resp.StatusCode)
	}
}

func TestG2PortalWiring(t *testing.T) {
	app, q, done := g2App(t)
	defer done()
	j := &g2jar{cookies: map[string]string{}}
	g2csrf(t, app, j)
	admin := g2login(t, app, j, "admin", "admin123")
	g2seedUser(t, q, "portal1", "user1234", "cust-portal-1")
	user := g2login(t, app, j, "portal1", "user1234")

	// No token: 401 from the billing middleware.
	resp := g2do(t, app, g2jsonReq(t, http.MethodGet, "/api/v1/portal/summary", ""))
	g2body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("portal without token = %d, want 401", resp.StatusCode)
	}
	// Admin lacks user-only permissions: 403.
	resp = g2do(t, app, g2bearer(t, admin, http.MethodGet, "/api/v1/portal/summary", ""))
	g2body(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("portal as admin = %d, want 403", resp.StatusCode)
	}
	// User reaches the C3 handler with locals filled.
	resp = g2do(t, app, g2bearer(t, user, http.MethodGet, "/api/v1/portal/summary", ""))
	body := g2body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("portal summary = %d %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	customer, _ := data["customer"].(map[string]any)
	if customer["id"] != "cust-portal-1" {
		t.Fatalf("summary customer = %v, want cust-portal-1", customer)
	}

	// C2 pay route sits behind the same middleware: with CSRF but no
	// Bearer [REDACTED] is 401 (without CSRF it would be 403 csrf_invalid first).
	resp = g2do(t, app, g2unsafe(t, j, http.MethodPost, "/api/v1/portal/invoices/x/pay", "{}"))
	g2body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pay without token = %d, want 401", resp.StatusCode)
	}
}

func TestG2WebhookExemptFromCSRF(t *testing.T) {
	app, _, done := g2App(t)
	defer done()
	// No CSRF cookie/header at all: the C2 handler answers (signature
	// validation), the CSRF middleware must not 403 first.
	resp := g2do(t, app, g2jsonReq(t, http.MethodPost, "/api/v1/webhooks/kriskris",
		`{"delivery_id":"probe-1","event":"invoice.paid"}`))
	body := g2body(t, resp)
	if resp.StatusCode == http.StatusForbidden && body["error"] == "csrf_invalid" {
		t.Fatal("webhook hit CSRF protection; it must be exempt")
	}
}
