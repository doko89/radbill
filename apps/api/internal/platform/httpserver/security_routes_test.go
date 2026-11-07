package httpserver

// G4 hardening (agent E2): generic authorization gate for every /api/v1/*
// route. Each protected route, called without a Bearer [REDACTED] must answer
// 401/403 — never 200 (unprotected) or 500 (crash/leak).
//
// NOTE TO ORCHESTRATOR: the route table below is a snapshot of server.go as
// of this writing. Agent E1 works in parallel and may add G4 routes AFTER
// this test was written; when that happens this table must grow to cover the
// new routes and the suite must be rerun. A new v1 route missing from the
// table is invisible to this test.
//
// Explicit public allowlist (tested separately in
// TestAllowlistedRoutesStayPublic):
//
//	POST /api/v1/auth/login, /auth/refresh, /auth/password/reset-request,
//	/auth/password/reset, POST /api/v1/webhooks/kriskris (HMAC, not JWT),
//	GET /healthz (liveness probe, not under /api/v1).
//
// POST /api/v1/auth/logout is intentionally public + idempotent (cookie
// cleanup must work with an expired token); it is NOT in the task allowlist
// but the test pins its 200 so the exception stays deliberate.
//
// Helpers (jar, do, readBody, jsonReq, fetchCSRF, unsafeReq) are reused from
// server_test.go (same package); this file only adds the :memory: app
// builder and the G4 tests.

import (
	"net/http"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/shared/config"
)

// newSecRoutesApp boots the full app on sqlite :memory: with a seeded
// billing admin. Kriskris is configured via env so the public webhook
// exercises its HMAC check (unconfigured it answers generic 500 before
// signature verification, which would mask the 401 assertion).
func newSecRoutesApp(t *testing.T) *fiber.App {
	t.Helper()
	t.Setenv("KRISKRIS_BASE_URL", "https://kriskris.test")
	t.Setenv("KRISKRIS_API_KEY", "test-key")
	t.Setenv("KRISKRIS_API_SECRET", "test-secret")
	t.Setenv("KRISKRIS_CALLBACK_SECRET", "test-callback-secret")
	t.Setenv("SETTINGS_ENC_KEY", "test-settings-enc-key-for-g4-e2")

	cfg := config.Config{
		Env:                 "test",
		Port:                "0",
		DBDriver:            "sqlite",
		SessionSecret:       "test-secret-0123456789abcdef",
		SessionTTL:          time.Hour,
		AdminUser:           "admin",
		AdminPass:           "admin123",
		UploadDir:           filepath.Join(t.TempDir(), "uploads"),
		JWTSecret:           "test-jwt-secret-min-32-chars-ok!",
		JWTAccessTTLMinutes: 15,
		RefreshTTLDays:      30,
		TZ:                  "Asia/Jakarta",
		SettingsEncKey:      "test-settings-enc-key-for-g4-e2",
	}
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	ctx := t.Context()
	if err := db.Migrate(ctx, pool, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedBillingAdmin(ctx, sqlcgen.New(pool), cfg.AdminUser, cfg.AdminPass); err != nil {
		t.Fatal(err)
	}
	app, err := NewWithFS(cfg, pool, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// secRoute is one protected v1 endpoint: method + path with a dummy id where
// the route takes :id (auth runs before the handler, so the id never
// resolves — 401/403 proves the guard, 404 would prove its absence only if
// auth passed).
type secRoute struct {
	method string
	path   string
}

// secProtectedRoutes enumerates every /api/v1/* route requiring a Bearer
// token (snapshot of server.go; see file doc for the E1 caveat).
var secProtectedRoutes = []secRoute{
	// Auth (self).
	{http.MethodGet, "/api/v1/auth/me"},
	{http.MethodPost, "/api/v1/auth/password/change"},
	// Admin: customers.
	{http.MethodGet, "/api/v1/admin/customers"},
	{http.MethodGet, "/api/v1/admin/customers/no-such-id"},
	{http.MethodPost, "/api/v1/admin/customers"},
	{http.MethodPut, "/api/v1/admin/customers/no-such-id"},
	{http.MethodPatch, "/api/v1/admin/customers/no-such-id"},
	{http.MethodDelete, "/api/v1/admin/customers/no-such-id"},
	// Admin: packages.
	{http.MethodGet, "/api/v1/admin/packages"},
	{http.MethodGet, "/api/v1/admin/packages/no-such-id"},
	{http.MethodPost, "/api/v1/admin/packages"},
	{http.MethodPut, "/api/v1/admin/packages/no-such-id"},
	{http.MethodPatch, "/api/v1/admin/packages/no-such-id"},
	{http.MethodDelete, "/api/v1/admin/packages/no-such-id"},
	// Admin: pppoe-accounts.
	{http.MethodGet, "/api/v1/admin/pppoe-accounts"},
	{http.MethodGet, "/api/v1/admin/pppoe-accounts/no-such-id"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts"},
	{http.MethodPut, "/api/v1/admin/pppoe-accounts/no-such-id"},
	{http.MethodPatch, "/api/v1/admin/pppoe-accounts/no-such-id"},
	{http.MethodDelete, "/api/v1/admin/pppoe-accounts/no-such-id"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/isolate"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/activate"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/disable"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/extend"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/change-package"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/reset-quota"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/disconnect"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/portal-account"},
	{http.MethodPost, "/api/v1/admin/pppoe-accounts/no-such-id/portal-account/reset"},
	// Admin: nas.
	{http.MethodGet, "/api/v1/admin/nas"},
	{http.MethodGet, "/api/v1/admin/nas/no-such-id"},
	{http.MethodPost, "/api/v1/admin/nas"},
	{http.MethodPut, "/api/v1/admin/nas/no-such-id"},
	{http.MethodPatch, "/api/v1/admin/nas/no-such-id"},
	{http.MethodDelete, "/api/v1/admin/nas/no-such-id"},
	{http.MethodPost, "/api/v1/admin/nas/no-such-id/test-coa"},
	// Admin: sessions (group-level Require).
	{http.MethodGet, "/api/v1/admin/sessions"},
	{http.MethodGet, "/api/v1/admin/sessions/history"},
	// Admin: invoices.
	{http.MethodGet, "/api/v1/admin/invoices"},
	{http.MethodGet, "/api/v1/admin/invoices/no-such-id"},
	{http.MethodPost, "/api/v1/admin/invoices"},
	{http.MethodPost, "/api/v1/admin/invoices/no-such-id/void"},
	{http.MethodPost, "/api/v1/admin/invoices/no-such-id/manual-payment"},
	// Admin: settings + scheduler.
	{http.MethodGet, "/api/v1/admin/settings"},
	{http.MethodPut, "/api/v1/admin/settings"},
	{http.MethodGet, "/api/v1/admin/scheduler/status"},
	// Portal (customer-scoped, still JWT).
	{http.MethodGet, "/api/v1/portal/summary"},
	{http.MethodGet, "/api/v1/portal/invoices"},
	{http.MethodGet, "/api/v1/portal/invoices/no-such-id"},
	{http.MethodGet, "/api/v1/portal/usage"},
	{http.MethodPost, "/api/v1/portal/invoices/no-such-id/pay"},
	{http.MethodGet, "/api/v1/portal/payment-attempts/no-such-id"},
	{http.MethodPost, "/api/v1/portal/topup"},
	{http.MethodPost, "/api/v1/portal/plan-changes"},
	{http.MethodGet, "/api/v1/portal/plan-changes"},
	{http.MethodPost, "/api/v1/portal/plan-changes/no-such-id/cancel"},
	{http.MethodGet, "/api/v1/portal/packages"},
	// Admin: plan-changes + reports.
	{http.MethodGet, "/api/v1/admin/plan-changes"},
	{http.MethodGet, "/api/v1/admin/reports/revenue"},
	{http.MethodGet, "/api/v1/admin/reports/usage"},
	{http.MethodGet, "/api/v1/admin/reports/receivables"},
	{http.MethodGet, "/api/v1/admin/reports/growth"},
	// Admin: G4 audit-logs + dashboard (added by E1 AFTER this test's
	// first draft — the anticipated case; orchestrator reruns the suite
	// once E1's tree builds).
	{http.MethodGet, "/api/v1/admin/audit-logs"},
	{http.MethodGet, "/api/v1/admin/dashboard"},
	{http.MethodGet, "/api/v1/admin/metrics"},
}

// TestV1RoutesRequireAuth calls every protected v1 route without a token:
// unsafe methods carry a valid CSRF token (so CSRF passes and the AUTH guard
// is what answers), safe methods need none. All must be 401/403.
func TestV1RoutesRequireAuth(t *testing.T) {
	app := newSecRoutesApp(t)
	j := newJar()
	fetchCSRF(t, app, j)

	for _, r := range secProtectedRoutes {
		var req *http.Request
		if r.method == http.MethodGet || r.method == http.MethodHead {
			req = jsonReq(t, r.method, r.path, "")
		} else {
			req = unsafeReq(t, j, r.method, r.path, `{}`)
		}
		resp := do(t, app, req)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s without token = %d %v, want 401/403", r.method, r.path, resp.StatusCode, body)
		}
	}
}

// TestWebhookRequiresSignature pins the public webhook's HMAC gate: no
// signature (or a bogus one) is 401, never 2xx/500. CSRF is exempt here by
// design (external caller), so none is sent.
func TestWebhookRequiresSignature(t *testing.T) {
	app := newSecRoutesApp(t)

	resp := do(t, app, jsonReq(t, http.MethodPost, "/api/v1/webhooks/kriskris", `{"event":"invoice.paid"}`))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("webhook without signature = %d %v, want 401", resp.StatusCode, body)
	}

	for name, sig := range map[string]string{
		"malformed": "bogus",
		"wrong":     "sha256=0000000000000000000000000000000000000000000000000000000000000000",
	} {
		req := jsonReq(t, http.MethodPost, "/api/v1/webhooks/kriskris", `{"event":"invoice.paid"}`)
		req.Header.Set("X-Kriskris-Signature", sig)
		resp := do(t, app, req)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("webhook with %s signature = %d %v, want 401", name, resp.StatusCode, body)
		}
	}
}

// TestAdminPostRequiresCSRF spot-checks that CSRF still guards v1 unsafe
// methods: valid JWT is irrelevant here — without X-CSRF-Token the answer
// is 403 csrf_invalid (CSRF middleware runs before auth).
func TestAdminPostRequiresCSRF(t *testing.T) {
	app := newSecRoutesApp(t)

	for _, r := range []secRoute{
		{http.MethodPost, "/api/v1/admin/customers"},
		{http.MethodPut, "/api/v1/admin/nas/no-such-id"},
		{http.MethodPost, "/api/v1/auth/password/change"},
	} {
		resp := do(t, app, jsonReq(t, r.method, r.path, `{}`))
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden || body["error"] != "csrf_invalid" {
			t.Errorf("%s %s without CSRF = %d %v, want 403 csrf_invalid", r.method, r.path, resp.StatusCode, body)
		}
	}
}

// TestAllowlistedRoutesStayPublic proves the explicit public allowlist is
// reachable without a token (answering 200/400/401 by input validity, never
// 403-csrf when CSRF is supplied and never 404).
func TestAllowlistedRoutesStayPublic(t *testing.T) {
	app := newSecRoutesApp(t)
	j := newJar()
	fetchCSRF(t, app, j)

	resp := do(t, app, jsonReq(t, http.MethodGet, "/healthz", ""))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz = %d %v, want 200", resp.StatusCode, body)
	}

	// Login with bad credentials: public route, auth failure (401), not
	// CSRF (403) or missing (404).
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrong"}`))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login bad-password = %d %v, want 401 (public)", resp.StatusCode, body)
	}

	// Refresh without cookie: public route, token failure (401).
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/refresh", ""))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("refresh without cookie = %d %v, want 401 (public)", resp.StatusCode, body)
	}

	// Reset-request never enumerates: unknown user is still 200.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/password/reset-request", `{"username":"nobody"}`))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("reset-request = %d %v, want 200 (public)", resp.StatusCode, body)
	}

	// Reset with a bogus token: public route, token failure (401).
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/password/reset", `{"token":"bogus","new_password":"newpassword123"}`))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("reset bogus-token = %d %v, want 401 (public)", resp.StatusCode, body)
	}

	// Logout without cookie: intentionally public + idempotent (200), so
	// clients can always clear state. Deliberate exception, see file doc.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/logout", ""))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("logout without cookie = %d %v, want 200 (public idempotent)", resp.StatusCode, body)
	}
}
