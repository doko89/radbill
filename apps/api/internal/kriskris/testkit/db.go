// Package kriskristest is the test-only kit for kriskris tests: an isolated
// sqlite :memory: DB with the assumed C1 table shapes, seed helpers, and a
// fake kriskris HTTP server. Imported only by _test.go files.
package kriskristest

import (
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Schema mirrors the table shapes this integration assumes (DDL itself is
// owned by C1; see infrastructure/store.go for the contract). Kept in one
// place so every test package drills the same assumptions.
const Schema = `
CREATE TABLE customers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE packages (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    price BIGINT NOT NULL,
    period TEXT NOT NULL,
    validity_seconds BIGINT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE pppoe_accounts (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    username TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE invoices (
    id TEXT PRIMARY KEY,
    number TEXT NOT NULL UNIQUE,
    pppoe_account_id TEXT NOT NULL,
    customer_id TEXT NOT NULL,
    total BIGINT NOT NULL,
    due_date TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unpaid',
    issued_at TEXT NOT NULL,
    paid_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE payments (
    id TEXT PRIMARY KEY,
    invoice_id TEXT NOT NULL,
    amount BIGINT NOT NULL,
    method TEXT NOT NULL,
    attempt_id TEXT,
    note TEXT NOT NULL DEFAULT '',
    paid_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE payment_attempts (
    id TEXT PRIMARY KEY,
    invoice_id TEXT NOT NULL,
    kriskris_invoice_id TEXT NOT NULL UNIQUE,
    base_amount BIGINT NOT NULL,
    unique_code BIGINT NOT NULL,
    amount BIGINT NOT NULL,
    qr_payload TEXT NOT NULL,
    status TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    paid_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    event TEXT NOT NULL,
    payload TEXT NOT NULL,
    received_at TEXT NOT NULL
);
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`

var dbSeq atomic.Int64

// OpenDB returns an isolated sqlite :memory: DB with Schema applied.
func OpenDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("file:kriskris_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbSeq.Add(1))
	pool, err := sql.Open("sqlite", name)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	if _, err := pool.Exec(Schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return pool
}

// SeedOpts describes one billable invoice fixture.
type SeedOpts struct {
	Tag           string // unique suffix for ids/usernames
	CustomerID    string // default "cust-"+Tag
	Total         int64  // default 50000
	Status        string // default "unpaid"
	PPPoEStatus   string // default "active"
	ValiditySecs  *int64 // package validity (nil = NULL -> 30-day fallback)
	ExpiresAt     string // pppoe expires_at RFC3339 or "" for NULL
	InvoiceNumber string // default "INV-"+Tag
}

// SeedResult holds the seeded ids.
type SeedResult struct {
	CustomerID string
	PackageID  string
	PPPoEID    string
	InvoiceID  string
	Number     string
}

// Seed creates customer+package+pppoe+invoice rows.
func Seed(t *testing.T, pool *sql.DB, o SeedOpts) SeedResult {
	t.Helper()
	if o.Total == 0 {
		o.Total = 50000
	}
	if o.Status == "" {
		o.Status = "unpaid"
	}
	if o.PPPoEStatus == "" {
		o.PPPoEStatus = "active"
	}
	if o.CustomerID == "" {
		o.CustomerID = "cust-" + o.Tag
	}
	if o.InvoiceNumber == "" {
		o.InvoiceNumber = "INV-" + o.Tag
	}
	r := SeedResult{
		CustomerID: o.CustomerID,
		PackageID:  "pkg-" + o.Tag,
		PPPoEID:    "ppp-" + o.Tag,
		InvoiceID:  "inv-" + o.Tag,
		Number:     o.InvoiceNumber,
	}
	now := time.Now().UTC().Format(time.RFC3339)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(q, args...); err != nil {
			t.Fatalf("seed %s: %v", q, err)
		}
	}
	exec(`INSERT INTO customers (id, name, created_at, updated_at) VALUES ($1,$2,$3,$3)`,
		r.CustomerID, "Customer "+o.Tag, now)
	var validity any
	if o.ValiditySecs != nil {
		validity = *o.ValiditySecs
	}
	exec(`INSERT INTO packages (id, name, price, period, validity_seconds, created_at, updated_at)
VALUES ($1,$2,50000,'monthly',$3,$4,$4)`, r.PackageID, "Paket "+o.Tag, validity, now)
	var expires any
	if o.ExpiresAt != "" {
		expires = o.ExpiresAt
	}
	exec(`INSERT INTO pppoe_accounts (id, customer_id, package_id, username, password, status, expires_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,'secret1',$5,$6,$7,$7)`,
		r.PPPoEID, r.CustomerID, r.PackageID, "user-"+o.Tag, o.PPPoEStatus, expires, now)
	exec(`INSERT INTO invoices (id, number, pppoe_account_id, customer_id, total, due_date, status, issued_at, paid_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,'2026-12-07',$6,$7,NULL,$7,$7)`,
		r.InvoiceID, r.Number, r.PPPoEID, r.CustomerID, o.Total, o.Status, now)
	return r
}

// Query helpers for assertions.

func InvoiceStatus(t *testing.T, pool *sql.DB, invoiceID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(`SELECT status FROM invoices WHERE id=$1`, invoiceID).Scan(&s); err != nil {
		t.Fatalf("invoice status: %v", err)
	}
	return s
}

func AttemptStatus(t *testing.T, pool *sql.DB, attemptID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(`SELECT status FROM payment_attempts WHERE id=$1`, attemptID).Scan(&s); err != nil {
		t.Fatalf("attempt status: %v", err)
	}
	return s
}

// PaymentRow is one payments row for assertions.
type PaymentRow struct {
	Amount  int64
	Method  string
	Attempt string
	Note    string
}

func ListPayments(t *testing.T, pool *sql.DB, invoiceID string) []PaymentRow {
	t.Helper()
	rows, err := pool.Query(`SELECT amount, method, COALESCE(attempt_id,''), note FROM payments WHERE invoice_id=$1 ORDER BY amount DESC`, invoiceID)
	if err != nil {
		t.Fatalf("list payments: %v", err)
	}
	defer rows.Close()
	var out []PaymentRow
	for rows.Next() {
		var p PaymentRow
		if err := rows.Scan(&p.Amount, &p.Method, &p.Attempt, &p.Note); err != nil {
			t.Fatalf("scan payment: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("payments rows: %v", err)
	}
	return out
}

func PPPoEState(t *testing.T, pool *sql.DB, pppoeID string) (status, expires string) {
	t.Helper()
	var exp sql.NullString
	if err := pool.QueryRow(`SELECT status, expires_at FROM pppoe_accounts WHERE id=$1`, pppoeID).Scan(&status, &exp); err != nil {
		t.Fatalf("pppoe state: %v", err)
	}
	if exp.Valid {
		expires = exp.String
	}
	return status, expires
}

func SetSetting(t *testing.T, pool *sql.DB, key, value string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := pool.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ($1,$2,$3)
ON CONFLICT(key) DO UPDATE SET value=$2, updated_at=$3`, key, value, now); err != nil {
		t.Fatalf("set setting: %v", err)
	}
}

// Int64Ptr is a helper for SeedOpts.ValiditySecs.
func Int64Ptr(v int64) *int64 { return &v }
