package presentation_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	jwtapp "boilerplate/api/internal/jwtauth/application"
	authdomain "boilerplate/api/internal/jwtauth/domain"
	jwtinfra "boilerplate/api/internal/jwtauth/infrastructure"
	jwtpres "boilerplate/api/internal/jwtauth/presentation"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

func sqlNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func newBillingCtxService(t *testing.T) (*jwtapp.Service, *sqlcgen.Queries) {
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
	if err := db.SeedBillingAdmin(context.Background(), q, "admin", "admin1234"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	hasher := jwtinfra.NewArgon2Hasher()
	svc := jwtapp.NewService(
		jwtinfra.NewSQLAccountRepository(q),
		jwtinfra.NewSQLRefreshTokenRepository(q),
		jwtinfra.NewSQLPasswordResetRepository(q),
		hasher, "test-jwt-secret-min-32-chars-ok!", 15*time.Minute, 24*time.Hour,
	)
	return svc, q
}

func probeApp(svc *jwtapp.Service, perm string) *fiber.App {
	app := fiber.New()
	app.Get("/probe", jwtpres.RequireWithBillingContext(svc, perm), func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"billing_customer_id": jwtpres.BillingCustomerID(c),
			"billing_role":        jwtpres.BillingRole(c),
		})
	})
	return app
}

func probeGet(t *testing.T, app *fiber.App, token string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body
}

func TestBillingContextUserLocals(t *testing.T) {
	svc, q := newBillingCtxService(t)
	ctx := context.Background()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	hash, err := jwtinfra.NewArgon2Hasher().Hash("user1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateBillingAccount(ctx, sqlcgen.CreateBillingAccountParams{
		ID: "user-acct-1", Username: "portal1", PasswordHash: hash,
		Role: authdomain.RoleUser, Status: authdomain.StatusActive,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID: "cust-portal-1", AccountID: sqlNullString("user-acct-1"), Name: "Portal User",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	sess, _, err := svc.Login(ctx, "portal1", "user1234", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, body := probeGet(t, probeApp(svc, authdomain.PermInvoiceReadOwn), sess.AccessToken)
	if code != http.StatusOK {
		t.Fatalf("probe = %d %v", code, body)
	}
	if body["billing_customer_id"] != "cust-portal-1" || body["billing_role"] != "user" {
		t.Fatalf("locals = %v", body)
	}
}

func TestBillingContextAdminLocalsEmpty(t *testing.T) {
	svc, _ := newBillingCtxService(t)
	sess, _, err := svc.Login(context.Background(), "admin", "admin1234", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, body := probeGet(t, probeApp(svc, authdomain.PermCustomerRead), sess.AccessToken)
	if code != http.StatusOK {
		t.Fatalf("probe = %d %v", code, body)
	}
	if body["billing_customer_id"] != "" || body["billing_role"] != "admin" {
		t.Fatalf("locals = %v", body)
	}
}

func TestBillingContextDenies(t *testing.T) {
	svc, _ := newBillingCtxService(t)
	sess, _, err := svc.Login(context.Background(), "admin", "admin1234", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// Missing token: 401.
	if code, _ := probeGet(t, probeApp(svc, authdomain.PermCustomerRead), ""); code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", code)
	}
	// Admin lacks user-only permissions: 403, same as Require.
	if code, _ := probeGet(t, probeApp(svc, authdomain.PermInvoiceReadOwn), sess.AccessToken); code != http.StatusForbidden {
		t.Fatalf("wrong perm = %d, want 403", code)
	}
}
