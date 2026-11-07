package infrastructure

import (
	"context"
	"errors"
	"testing"
	"time"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/pppoe/domain"
)

func openTestDB(t *testing.T) (*sqlcgen.Queries, *SQLPortalStore) {
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
	q := sqlcgen.New(pool)
	return q, NewSQLPortalStore(pool, q)
}

func TestPortalAccountLifecycle(t *testing.T) {
	q, store := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	stamp := now.Format(time.RFC3339)

	if _, err := q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID: "cust-portal-1", Name: "Portal", CreatedAt: stamp, UpdatedAt: stamp,
	}); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	accountID, err := store.CreatePortalAccount(ctx, "cust-portal-1", "portal-user-1", "hash-1", now)
	if err != nil || accountID == "" {
		t.Fatalf("create portal: %v %q", err, accountID)
	}
	if _, err := store.CreatePortalAccount(ctx, "cust-portal-1", "portal-user-x", "hash-x", now); !errors.Is(err, domain.ErrPortalExists) {
		t.Fatalf("second portal = %v, want ErrPortalExists", err)
	}
	cust, _ := q.GetBillingCustomer(ctx, "cust-portal-1")
	if !cust.AccountID.Valid || cust.AccountID.String != accountID {
		t.Fatalf("customer not linked: %+v", cust.AccountID)
	}
	username, err := store.ResetPortalPassword(ctx, "cust-portal-1", "hash-2", now)
	if err != nil || username != "portal-user-1" {
		t.Fatalf("reset: %v %q", err, username)
	}
	acct, _ := q.GetBillingAccountByID(ctx, accountID)
	if acct.PasswordHash != "hash-2" {
		t.Fatalf("password not rotated")
	}
	if _, err := q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID: "cust-portal-2", Name: "NoPortal", CreatedAt: stamp, UpdatedAt: stamp,
	}); err != nil {
		t.Fatalf("seed customer 2: %v", err)
	}
	if _, err := store.ResetPortalPassword(ctx, "cust-portal-2", "h", now); !errors.Is(err, domain.ErrNoPortalAccount) {
		t.Fatalf("reset without portal = %v, want ErrNoPortalAccount", err)
	}
}
