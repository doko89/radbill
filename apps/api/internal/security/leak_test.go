package security_test

// G4 hardening (agent E2): secret-leak gate. Boots the full app, logs in,
// plants distinctive probe secrets (PPPoE password, NAS shared secret,
// kriskris API secret), then asserts response bodies never contain them —
// nor password_hash, raw secret values, or unmasked encrypted blobs — on
// success AND error paths (PRD §5.2, §9, §12).
//
// External test package on purpose: E1 will import internal/security from
// httpserver for the rate limiter, and an internal (white-box) test importing
// httpserver would then be an import cycle.
//
// Key names vs values: the settings map legitimately lists secret KEYS
// (e.g. "kriskris_api_secret"); the assertion is that their VALUES are the
// "***" mask, never the plaintext or the enc:v1: blob. Other endpoints must
// not mention secret keys at all.

import (
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

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/httpserver"
	"boilerplate/api/internal/shared/config"
)

// Distinctive probe secrets: random-looking so a substring match is a real
// leak, never a coincidence.
const (
	pppoeProbe = "pppoe-Pl4in-Le4k-Probe-9z"
	nasProbe   = "nas-Sh4red-Le4k-Probe-8y"
	krisProbe  = "kriskris-S3cret-Le4k-Probe-7x"
)

type leakJar struct {
	cookies map[string]string
}

func newLeakJar() *leakJar { return &leakJar{cookies: map[string]string{}} }

func (j *leakJar) store(resp *http.Response) {
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
}

func (j *leakJar) apply(req *http.Request) {
	for name, val := range j.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
}

// newLeakApp boots the full app with a seeded billing admin.
func newLeakApp(t *testing.T) *fiber.App {
	t.Helper()
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
	pool, err := db.Open("sqlite", filepath.Join(t.TempDir(), "app.db"), "")
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
	app, err := httpserver.NewWithFS(cfg, pool, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func leakReq(t *testing.T, method, path, body string) *http.Request {
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

// authed works for safe and unsafe methods: CSRF cookie+header plus Bearer.
func authed(t *testing.T, j *leakJar, token, method, path, body string) *http.Request {
	t.Helper()
	req := leakReq(t, method, path, body)
	j.apply(req)
	if csrf := j.cookies["csrf_token"]; csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// doRaw performs req and returns status + raw body string.
func doRaw(t *testing.T, app *fiber.App, req *http.Request) (int, string) {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("request %s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

// leakLogin fetches CSRF, logs in as billing admin, returns the access token.
func leakLogin(t *testing.T, app *fiber.App, j *leakJar) string {
	t.Helper()
	// Fetch via app.Test directly (doRaw drops response headers/cookies).
	req := leakReq(t, http.MethodGet, "/api/auth/csrf", "")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/csrf = %d", resp.StatusCode)
	}
	j.store(resp)
	if j.cookies["csrf_token"] == "" {
		t.Fatal("expected csrf_token cookie")
	}

	loginReq := leakReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`)
	j.apply(loginReq)
	loginReq.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	code, raw := doRaw(t, app, loginReq)
	if code != http.StatusOK {
		t.Fatalf("login = %d %s", code, raw)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("login body is not JSON: %s", raw)
	}
	data, _ := body["data"].(map[string]any)
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login response missing access_token: %s", raw)
	}
	return token
}

// dataID extracts data.id from a {data:{id}} response, failing on other codes.
func dataID(t *testing.T, app *fiber.App, req *http.Request, wantCode int) (string, string) {
	t.Helper()
	code, raw := doRaw(t, app, req)
	if code != wantCode {
		t.Fatalf("%s %s = %d, want %d: %s", req.Method, req.URL.Path, code, wantCode, raw)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("response is not JSON: %s", raw)
	}
	data, _ := body["data"].(map[string]any)
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("response missing data.id: %s", raw)
	}
	return id, raw
}

// assertClean fails when raw contains any forbidden substring.
func assertClean(t *testing.T, where, raw string, forbidden ...string) {
	t.Helper()
	for _, s := range forbidden {
		if strings.Contains(raw, s) {
			t.Errorf("%s leaks %q in body: %s", where, s, raw)
		}
	}
}

// TestResponsesLeakNoSecrets plants probe secrets, then asserts key endpoints
// never return them (nor password_hash / encrypted blobs).
func TestResponsesLeakNoSecrets(t *testing.T) {
	app := newLeakApp(t)
	j := newLeakJar()
	token := leakLogin(t, app, j)

	// /auth/me: profile only, never any credential material.
	code, raw := doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/auth/me", ""))
	if code != http.StatusOK {
		t.Fatalf("me = %d: %s", code, raw)
	}
	assertClean(t, "me", raw, "password_hash", pppoeProbe, nasProbe, krisProbe, "enc:v1:")

	// Fixtures carrying the probe secrets.
	custID, _ := dataID(t, app,
		authed(t, j, token, http.MethodPost, "/api/v1/admin/customers", `{"name":"Leak Probe","phone":"0800"}`),
		http.StatusCreated)
	pkgID, _ := dataID(t, app,
		authed(t, j, token, http.MethodPost, "/api/v1/admin/packages", `{"name":"Probe 10M","price":100000,"period":"monthly"}`),
		http.StatusCreated)
	pppoeID, createRaw := dataID(t, app,
		authed(t, j, token, http.MethodPost, "/api/v1/admin/pppoe-accounts",
			`{"customer_id":"`+custID+`","package_id":"`+pkgID+`","username":"leakprobe01","password":"`+pppoeProbe+`"}`),
		http.StatusCreated)
	assertClean(t, "pppoe create", createRaw, "password_hash", pppoeProbe)
	nasID, nasCreateRaw := dataID(t, app,
		authed(t, j, token, http.MethodPost, "/api/v1/admin/nas",
			`{"name":"probe-nas","ip":"10.9.9.9","secret":"`+nasProbe+`","coa_port":3799}`),
		http.StatusCreated)
	assertClean(t, "nas create", nasCreateRaw, "password_hash", nasProbe, `"secret"`)
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodPut, "/api/v1/admin/settings",
		`{"kriskris_api_secret":"`+krisProbe+`"}`))
	if code != http.StatusOK {
		t.Fatalf("settings update = %d: %s", code, raw)
	}
	assertClean(t, "settings update", raw, krisProbe, "enc:v1:")

	// PPPoE get WITHOUT reveal: no plaintext, no password key at all.
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/pppoe-accounts/"+pppoeID, ""))
	if code != http.StatusOK {
		t.Fatalf("pppoe get = %d: %s", code, raw)
	}
	assertClean(t, "pppoe get", raw, "password_hash", pppoeProbe, `"password"`)

	// PPPoE list: never includes passwords.
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/pppoe-accounts", ""))
	if code != http.StatusOK {
		t.Fatalf("pppoe list = %d: %s", code, raw)
	}
	assertClean(t, "pppoe list", raw, "password_hash", pppoeProbe)

	// Positive control: WITH reveal the password IS present (proves the
	// probe is real and absence above is meaningful, not a broken fixture).
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/pppoe-accounts/"+pppoeID+"?reveal=true", ""))
	if code != http.StatusOK {
		t.Fatalf("pppoe reveal = %d: %s", code, raw)
	}
	if !strings.Contains(raw, pppoeProbe) {
		t.Fatalf("pppoe reveal missing probe password (fixture broken?): %s", raw)
	}
	assertClean(t, "pppoe reveal", raw, "password_hash")

	// NAS get: shared secret is write-only, never serialized.
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/nas/"+nasID, ""))
	if code != http.StatusOK {
		t.Fatalf("nas get = %d: %s", code, raw)
	}
	assertClean(t, "nas get", raw, "password_hash", nasProbe, `"secret"`)

	// Settings list: secret values are exactly "***", never plaintext nor
	// the encrypted blob.
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/settings", ""))
	if code != http.StatusOK {
		t.Fatalf("settings list = %d: %s", code, raw)
	}
	assertClean(t, "settings list", raw, krisProbe, "enc:v1:")
	var settingsBody map[string]any
	if err := json.Unmarshal([]byte(raw), &settingsBody); err != nil {
		t.Fatalf("settings list is not JSON: %s", raw)
	}
	settingsData, _ := settingsBody["data"].(map[string]any)
	if got, _ := settingsData["kriskris_api_secret"].(string); got != "***" {
		t.Errorf("settings kriskris_api_secret = %q, want masked %q (body: %s)", got, "***", raw)
	}

	// Invoice create + get: billing bodies carry no credential material.
	invID, invCreateRaw := dataID(t, app,
		authed(t, j, token, http.MethodPost, "/api/v1/admin/invoices",
			`{"pppoe_account_id":"`+pppoeID+`","items":[{"description":"probe","qty":1,"price":50000}]}`),
		http.StatusCreated)
	assertClean(t, "invoice create", invCreateRaw, "password_hash", pppoeProbe, nasProbe, krisProbe)
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/invoices/"+invID, ""))
	if code != http.StatusOK {
		t.Fatalf("invoice get = %d: %s", code, raw)
	}
	assertClean(t, "invoice get", raw, "password_hash", pppoeProbe, nasProbe, krisProbe)

	// Error path: 404 bodies never contain password_hash either.
	code, raw = doRaw(t, app, authed(t, j, token, http.MethodGet, "/api/v1/admin/pppoe-accounts/no-such-id", ""))
	if code != http.StatusNotFound {
		t.Fatalf("pppoe get missing = %d, want 404: %s", code, raw)
	}
	assertClean(t, "pppoe 404", raw, "password_hash", pppoeProbe)
}

// TestAuthErrorsLeakNoPasswordHash asserts password_hash never appears in
// authentication failure responses.
func TestAuthErrorsLeakNoPasswordHash(t *testing.T) {
	app := newLeakApp(t)
	j := newLeakJar()
	// CSRF fetch (stores cookies via app.Test since doRaw drops headers).
	req := leakReq(t, http.MethodGet, "/api/auth/csrf", "")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	j.store(resp)

	// Wrong password → 401 without credential material.
	badLogin := leakReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrong"}`)
	j.apply(badLogin)
	badLogin.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	code, raw := doRaw(t, app, badLogin)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401: %s", code, raw)
	}
	assertClean(t, "bad login", raw, "password_hash", "argon2", "$argon2")

	// Garbage token → 401 without credential material.
	forged := leakReq(t, http.MethodGet, "/api/v1/auth/me", "")
	forged.Header.Set("Authorization", "Bearer forged.token.here")
	code, raw = doRaw(t, app, forged)
	if code != http.StatusUnauthorized {
		t.Fatalf("forged token = %d, want 401: %s", code, raw)
	}
	assertClean(t, "forged token", raw, "password_hash")
}
