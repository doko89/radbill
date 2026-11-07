package presentation_test

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

func newBillingApp(t *testing.T) *fiber.App {
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

func fetchCSRF(t *testing.T, app *fiber.App, j *jar) {
	t.Helper()
	resp := do(t, app, jsonReq(t, http.MethodGet, "/api/auth/csrf", ""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/csrf = %d", resp.StatusCode)
	}
	j.store(resp)
	if j.cookies["csrf_token"] == "" {
		t.Fatal("expected csrf_token cookie")
	}
}

func unsafeReq(t *testing.T, j *jar, method, path, body string) *http.Request {
	t.Helper()
	req := jsonReq(t, method, path, body)
	j.apply(req)
	req.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	return req
}

func bearerReq(t *testing.T, token, method, path, body string) *http.Request {
	t.Helper()
	req := jsonReq(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func login(t *testing.T, app *fiber.App, j *jar, username, password string) (string, *http.Response) {
	t.Helper()
	resp := do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`))
	j.store(resp)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login response missing access_token: %v", body)
	}
	return token, resp
}

func TestHealthz(t *testing.T) {
	app := newBillingApp(t)
	resp := do(t, app, jsonReq(t, http.MethodGet, "/healthz", ""))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz = %d %v", resp.StatusCode, body)
	}
}

func TestBillingAuthFlow(t *testing.T) {
	app := newBillingApp(t)
	j := newJar()

	// New API routes inherit CSRF protection.
	resp := do(t, app, jsonReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden || body["error"] != "csrf_invalid" {
		t.Fatalf("login without CSRF = %d %v, want 403 csrf_invalid", resp.StatusCode, body)
	}

	// Admin routes require a bearer token.
	resp = do(t, app, jsonReq(t, http.MethodGet, "/api/v1/admin/customers", ""))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated admin = %d %v, want 401", resp.StatusCode, body)
	}

	fetchCSRF(t, app, j)

	// Wrong password is 401 with the billing envelope.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrong"}`))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d %v, want 401", resp.StatusCode, body)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "unauthorized" {
		t.Fatalf("bad login envelope = %v", body)
	}

	token, loginResp := login(t, app, j, "admin", "admin123")
	loginResp.Body.Close()
	if j.cookies["refresh_token"] == "" {
		t.Fatal("login did not set refresh_token cookie")
	}

	resp = do(t, app, bearerReq(t, token, http.MethodGet, "/api/v1/auth/me", ""))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me = %d %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	user, _ := data["user"].(map[string]any)
	if user["username"] != "admin" || user["role"] != "admin" {
		t.Fatalf("me user = %v", user)
	}
	if perms, _ := user["permissions"].([]any); len(perms) == 0 {
		t.Fatalf("me permissions empty: %v", user)
	}

	// Refresh rotates the cookie.
	refreshReq := unsafeReq(t, j, http.MethodPost, "/api/v1/auth/refresh", "")
	resp = do(t, app, refreshReq)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d %v", resp.StatusCode, body)
	}

	// Logout revokes; the (pre-rotation) cookie no longer refreshes.
	resp = do(t, app, unsafeReq(t, j, http.MethodPost, "/api/v1/auth/logout", ""))
	readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
}

func TestBillingCustomerCRUD(t *testing.T) {
	app := newBillingApp(t)
	j := newJar()
	fetchCSRF(t, app, j)
	token, resp := login(t, app, j, "admin", "admin123")
	resp.Body.Close()

	req := bearerReq(t, token, http.MethodGet, "/api/v1/admin/customers", "")
	resp = do(t, app, req)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || body["total"] != float64(0) {
		t.Fatalf("list = %d %v", resp.StatusCode, body)
	}

	create := unsafeReq(t, j, http.MethodPost, "/api/v1/admin/customers", `{"name":"Budi","phone":"0812"}`)
	create.Header.Set("Authorization", "Bearer "+token)
	resp = do(t, app, create)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	custID, _ := data["id"].(string)
	if custID == "" || data["name"] != "Budi" {
		t.Fatalf("create body = %v", body)
	}

	resp = do(t, app, bearerReq(t, token, http.MethodGet, "/api/v1/admin/customers/"+custID, ""))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get = %d %v", resp.StatusCode, body)
	}

	resp = do(t, app, bearerReq(t, token, http.MethodGet, "/api/v1/admin/customers/nope", ""))
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing = %d %v, want 404", resp.StatusCode, body)
	}
}
