package httpserver

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/crypto/bcrypt"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/shared/config"
)

// jar is a minimal cookie jar for app.Test requests.
type jar struct {
	cookies map[string]string
}

func newJar() *jar { return &jar{cookies: map[string]string{}} }

func (j *jar) store(resp *http.Response) {
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
}

func (j *jar) apply(req *http.Request) {
	for name, val := range j.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
}

func (j *jar) csrfToken() string { return j.cookies["csrf_token"] }

func newTestApp(t *testing.T) *fiber.App {
	t.Helper()
	// Empty bundle: static tests assert the not-built path deterministically,
	// independent of whether `make build-web` ran in this worktree.
	return newTestAppWithFS(t, fstest.MapFS{})
}

func newTestAppWithFS(t *testing.T, dist fs.FS) *fiber.App {
	t.Helper()
	cfg := config.Config{
		Env:           "test",
		Port:          "0",
		DBDriver:      "sqlite",
		SessionSecret: "test-secret-0123456789abcdef",
		SessionTTL:    time.Hour,
		AdminUser:     "admin",
		AdminPass:     "admin123",
		UploadDir:     filepath.Join(t.TempDir(), "uploads"),
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
	if err := db.SeedAdmin(ctx, sqlcgen.New(pool), cfg.AdminUser, cfg.AdminPass, bcrypt.MinCost); err != nil {
		t.Fatal(err)
	}
	app, err := NewWithFS(cfg, pool, dist)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func do(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("request %s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var out map[string]any
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		return map[string]any{}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("response is not JSON %q: %v", string(raw), err)
	}
	return out
}

func jsonReq(t *testing.T, method, path, body string) *http.Request {
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

// fetchCSRF performs a safe GET to obtain the CSRF cookie.
func fetchCSRF(t *testing.T, app *fiber.App, j *jar) {
	t.Helper()
	resp := do(t, app, jsonReq(t, http.MethodGet, "/api/auth/csrf", ""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/csrf = %d", resp.StatusCode)
	}
	j.store(resp)
	if j.csrfToken() == "" {
		t.Fatal("expected csrf_token cookie to be set")
	}
}

func unsafeReq(t *testing.T, j *jar, method, path, body string) *http.Request {
	t.Helper()
	req := jsonReq(t, method, path, body)
	j.apply(req)
	req.Header.Set("X-CSRF-Token", j.csrfToken())
	return req
}

func TestSecurityHeaders(t *testing.T) {
	app := newTestApp(t)
	resp := do(t, app, jsonReq(t, http.MethodGet, "/api/auth/csrf", ""))
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("missing or weak CSP: %q", csp)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestLoginRequiresCSRF(t *testing.T) {
	app := newTestApp(t)
	resp := do(t, app, jsonReq(t, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"admin123"}`))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden || body["error"] != "csrf_invalid" {
		t.Fatalf("expected 403 csrf_invalid, got %d %v", resp.StatusCode, body)
	}
}

func TestLoginFlow(t *testing.T) {
	app := newTestApp(t)
	j := newJar()
	fetchCSRF(t, app, j)

	// Wrong password -> 401, no session cookie.
	resp := do(t, app, unsafeReq(t, j, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"nope"}`))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: expected 401, got %d %v", resp.StatusCode, body)
	}

	// Correct credentials -> 200 + HttpOnly session cookie.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"admin123"}`))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || body["username"] != "admin" {
		t.Fatalf("login: expected 200 admin, got %d %v", resp.StatusCode, body)
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite = %v, want Lax", sessionCookie.SameSite)
	}
	j.store(resp)

	// Authenticated request works.
	req := jsonReq(t, http.MethodGet, "/api/auth/me", "")
	j.apply(req)
	resp = do(t, app, req)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || body["username"] != "admin" {
		t.Fatalf("me: expected 200 admin, got %d %v", resp.StatusCode, body)
	}

	// Logout clears the cookie.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/auth/logout", ""))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}
	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == "session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected session cookie to be cleared on logout")
	}
}

func TestOrdersRequireAuth(t *testing.T) {
	app := newTestApp(t)
	resp := do(t, app, jsonReq(t, http.MethodGet, "/api/orders/", ""))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestOrdersFlow(t *testing.T) {
	app := newTestApp(t)
	j := newJar()
	fetchCSRF(t, app, j)
	resp := do(t, app, unsafeReq(t, j, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"admin123"}`))
	resp.Body.Close()
	j.store(resp)

	get := func(path string) (int, map[string]any) {
		req := jsonReq(t, http.MethodGet, path, "")
		j.apply(req)
		r := do(t, app, req)
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
			var arr []any
			if err := json.Unmarshal(raw, &arr); err != nil {
				t.Fatal(err)
			}
			return r.StatusCode, map[string]any{"_list": arr}
		}
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return r.StatusCode, out
	}

	if code, body := get("/api/orders/"); code != http.StatusOK || len(body["_list"].([]any)) != 0 {
		t.Fatalf("initial list: expected 200 [], got %d %v", code, body)
	}

	// Invalid create -> 400 with fields.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/orders/", `{"item":"","qty":0}`))
	invalid := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || invalid["fields"] == nil {
		t.Fatalf("invalid create: expected 400 + fields, got %d %v", resp.StatusCode, invalid)
	}

	// Valid create -> 201.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/orders/", `{"item":"keyboard","qty":2}`))
	created := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated || created["status"] != "open" {
		t.Fatalf("create: expected 201 open, got %d %v", resp.StatusCode, created)
	}
	orderID, _ := created["id"].(string)
	if orderID == "" {
		t.Fatal("created order has no id")
	}

	if code, body := get("/api/orders/"); code != http.StatusOK || len(body["_list"].([]any)) != 1 {
		t.Fatalf("list after create: expected 1 order, got %d %v", code, body)
	}

	// done -> 200, then done again -> 409.
	resp = do(t, app, unsafeReq(t, j, http.MethodPatch, "/api/orders/"+orderID+"/status", `{"status":"done"}`))
	done := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || done["status"] != "done" {
		t.Fatalf("set done: expected 200 done, got %d %v", resp.StatusCode, done)
	}
	resp = do(t, app, unsafeReq(t, j, http.MethodPatch, "/api/orders/"+orderID+"/status", `{"status":"cancelled"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("re-transition: expected 409, got %d", resp.StatusCode)
	}
}

func TestAuthMethodsDisabledByDefault(t *testing.T) {
	app := newTestApp(t)
	resp := do(t, app, jsonReq(t, http.MethodGet, "/api/auth/methods", ""))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body["password"] != true {
		t.Fatalf("password must always be true: %v", body)
	}
	for _, key := range []string{"oidc", "ldap"} {
		m, ok := body[key].(map[string]any)
		if !ok || m["enabled"] != false {
			t.Fatalf("%s must be disabled by default: %v", key, body)
		}
	}
}

func TestDisabledExternalLogins404(t *testing.T) {
	app := newTestApp(t)
	j := newJar()
	fetchCSRF(t, app, j)

	for _, req := range []*http.Request{
		jsonReq(t, http.MethodGet, "/api/auth/oidc/login", ""),
		jsonReq(t, http.MethodGet, "/api/auth/oidc/callback?code=x&state=y", ""),
		unsafeReq(t, j, http.MethodPost, "/api/auth/ldap/login", `{"username":"u","password":"p"}`),
	} {
		resp := do(t, app, req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d", req.Method, req.URL.Path, resp.StatusCode)
		}
	}
}

func TestSPAServesEmbeddedBundle(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/app.js":  {Data: []byte("console.log(1)")},
		"assets/app.css": {Data: []byte("body{}")},
	}
	app := newTestAppWithFS(t, dist)

	resp := do(t, app, jsonReq(t, http.MethodGet, "/", ""))
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "id=root") {
		t.Fatalf("GET /: expected 200 index.html, got %d %q", resp.StatusCode, string(raw))
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("index.html missing CSP: %q", csp)
	}

	// Client-side route falls back to index.html.
	resp = do(t, app, jsonReq(t, http.MethodGet, "/login", ""))
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "id=root") {
		t.Fatalf("GET /login: expected SPA fallback, got %d %q", resp.StatusCode, string(raw))
	}

	// Hashed assets get immutable caching.
	resp = do(t, app, jsonReq(t, http.MethodGet, "/assets/app.js", ""))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /assets/app.js: expected 200, got %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("asset Cache-Control = %q, want immutable", cc)
	}
}

func TestSPAFallbackWithoutBuild(t *testing.T) {
	app := newTestApp(t)
	resp := do(t, app, jsonReq(t, http.MethodGet, "/", ""))
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(raw), "build-web") {
		t.Fatalf("expected 503 build-web hint, got %d %q", resp.StatusCode, string(raw))
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Fatal("expected CSP header on SPA fallback")
	}
}
