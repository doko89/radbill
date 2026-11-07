package infrastructure_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	authinfra "boilerplate/api/internal/jwtauth/infrastructure"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// openTestDB opens a migrated sqlite :memory: database, skipping when the
// driver is unavailable.
func openTestDB(t *testing.T) (*sql.DB, *sqlcgen.Queries) {
	t.Helper()
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		pool.Close()
		t.Skipf("migrate unavailable: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool, sqlcgen.New(pool)
}

func TestSQLAccountLifecycle(t *testing.T) {
	_, q := openTestDB(t)
	ctx := context.Background()
	repo := authinfra.NewSQLAccountRepository(q)
	now := time.Now().UTC().Truncate(time.Second)

	acct := &authdomain.Account{
		ID: "acct-lifecycle-1", Username: "lifecycle-admin",
		PasswordHash: "phc-hash", Role: authdomain.RoleAdmin,
		Status: authdomain.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(ctx, acct); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, acct); !errors.Is(err, authdomain.ErrUsernameTaken) {
		t.Fatalf("duplicate create = %v, want ErrUsernameTaken", err)
	}
	got, err := repo.GetByUsername(ctx, "lifecycle-admin")
	if err != nil || got.ID != acct.ID {
		t.Fatalf("get by username: %v %+v", err, got)
	}
	if _, err := repo.GetByID(ctx, acct.ID); err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if _, err := repo.GetByUsername(ctx, "no-such-user"); !errors.Is(err, authdomain.ErrAccountNotFound) {
		t.Fatalf("missing user = %v, want ErrAccountNotFound", err)
	}
	if err := repo.UpdatePassword(ctx, acct.ID, "new-hash", now); err != nil {
		t.Fatalf("update password: %v", err)
	}
	got, _ = repo.GetByID(ctx, acct.ID)
	if got.PasswordHash != "new-hash" {
		t.Fatalf("password not updated")
	}
	if err := repo.UpdateStatus(ctx, acct.ID, authdomain.StatusDisabled, now); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, _ = repo.GetByID(ctx, acct.ID)
	if !got.Disabled() {
		t.Fatalf("account not disabled")
	}

	custID, err := repo.CustomerIDForAccount(ctx, acct.ID)
	if err != nil || custID != "" {
		t.Fatalf("customer link = %q, %v; want empty", custID, err)
	}
	if _, err := q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID: "cust-link-1", AccountID: sql.NullString{String: acct.ID, Valid: true},
		Name: "Linked", CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("link customer: %v", err)
	}
	custID, err = repo.CustomerIDForAccount(ctx, acct.ID)
	if err != nil || custID != "cust-link-1" {
		t.Fatalf("customer link = %q, %v", custID, err)
	}
}

func TestSQLRefreshLifecycle(t *testing.T) {
	_, q := openTestDB(t)
	ctx := context.Background()
	accounts := authinfra.NewSQLAccountRepository(q)
	refresh := authinfra.NewSQLRefreshTokenRepository(q)
	now := time.Now().UTC().Truncate(time.Second)

	acct := &authdomain.Account{
		ID: "acct-refresh-1", Username: "refresh-user",
		PasswordHash: "h", Role: authdomain.RoleUser,
		Status: authdomain.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := accounts.Create(ctx, acct); err != nil {
		t.Fatalf("create: %v", err)
	}
	tok := &authdomain.RefreshToken{
		ID: "rt-1", AccountID: acct.ID, TokenHash: "hash-rt-1",
		ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}
	if err := refresh.Create(ctx, tok); err != nil {
		t.Fatalf("create token: %v", err)
	}
	got, err := refresh.GetByHash(ctx, "hash-rt-1")
	if err != nil || got.AccountID != acct.ID {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := refresh.GetByHash(ctx, "nope"); !errors.Is(err, authdomain.ErrTokenInvalid) {
		t.Fatalf("missing = %v, want ErrTokenInvalid", err)
	}
	if err := refresh.Revoke(ctx, "hash-rt-1", now); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ = refresh.GetByHash(ctx, "hash-rt-1")
	if !got.Revoked() {
		t.Fatalf("token not revoked")
	}
	if err := refresh.Create(ctx, &authdomain.RefreshToken{
		ID: "rt-2", AccountID: acct.ID, TokenHash: "hash-rt-2",
		ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if err := refresh.RevokeAllForAccount(ctx, acct.ID, now); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	got, _ = refresh.GetByHash(ctx, "hash-rt-2")
	if !got.Revoked() {
		t.Fatalf("token 2 not revoked")
	}
}

func TestSQLPasswordResetLifecycle(t *testing.T) {
	_, q := openTestDB(t)
	ctx := context.Background()
	accounts := authinfra.NewSQLAccountRepository(q)
	resets := authinfra.NewSQLPasswordResetRepository(q)
	now := time.Now().UTC().Truncate(time.Second)

	acct := &authdomain.Account{
		ID: "acct-reset-1", Username: "reset-user",
		PasswordHash: "h", Role: authdomain.RoleUser,
		Status: authdomain.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := accounts.Create(ctx, acct); err != nil {
		t.Fatalf("create: %v", err)
	}
	pr := &authdomain.PasswordReset{
		ID: "pr-1", AccountID: acct.ID, TokenHash: "hash-pr-1",
		ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now,
	}
	if err := resets.Create(ctx, pr); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := resets.GetByHash(ctx, "hash-pr-1")
	if err != nil || !got.Usable(now) {
		t.Fatalf("usable reset: %v %+v", err, got)
	}
	if err := resets.MarkUsed(ctx, "pr-1", now); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	got, _ = resets.GetByHash(ctx, "hash-pr-1")
	if got.Usable(now) {
		t.Fatalf("used token still usable")
	}
}
