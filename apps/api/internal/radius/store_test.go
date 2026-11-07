package radius

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"
)

// testSchema mirrors the B1 contract columns consumed by radiusd queries.
const testSchema = `
CREATE TABLE IF NOT EXISTS nas (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, ip TEXT NOT NULL UNIQUE,
    secret TEXT NOT NULL, coa_port INTEGER NOT NULL DEFAULT 3799,
    created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS customers (
    id TEXT PRIMARY KEY, account_id TEXT, name TEXT NOT NULL, phone TEXT,
    address TEXT, notes TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS packages (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, price BIGINT NOT NULL, period TEXT NOT NULL,
    rate_limit TEXT, address_pool TEXT, validity_seconds BIGINT, online_seconds_limit BIGINT,
    quota_bytes BIGINT, quota_reset TEXT NOT NULL DEFAULT 'none',
    fup_threshold_bytes BIGINT, fup_rate_limit TEXT,
    on_exhausted TEXT NOT NULL DEFAULT 'reject',
    is_public INTEGER NOT NULL DEFAULT 1, allow_self_service INTEGER NOT NULL DEFAULT 1,
    upgrade_group TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS pppoe_accounts (
    id TEXT PRIMARY KEY, customer_id TEXT NOT NULL, package_id TEXT NOT NULL, nas_id TEXT,
    username TEXT NOT NULL UNIQUE, password TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active',
    expires_at TEXT, static_ip TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS usage_counters (
    pppoe_account_id TEXT NOT NULL, period_start TEXT NOT NULL,
    bytes_in BIGINT NOT NULL DEFAULT 0, bytes_out BIGINT NOT NULL DEFAULT 0,
    online_seconds BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (pppoe_account_id, period_start));
CREATE TABLE IF NOT EXISTS radius_sessions (
    id TEXT PRIMARY KEY, session_id TEXT NOT NULL, pppoe_account_id TEXT NOT NULL,
    nas_id TEXT NOT NULL, framed_ip TEXT, started_at TEXT NOT NULL, stopped_at TEXT,
    bytes_in BIGINT NOT NULL DEFAULT 0, bytes_out BIGINT NOT NULL DEFAULT 0,
    terminate_cause TEXT, updated_at TEXT NOT NULL,
    UNIQUE (session_id, nas_id));
CREATE TABLE IF NOT EXISTS quota_topups (
    id TEXT PRIMARY KEY, pppoe_account_id TEXT NOT NULL, bytes BIGINT NOT NULL,
    period_start TEXT NOT NULL, invoice_id TEXT NOT NULL,
    created_at TEXT NOT NULL);`

// openTestDB opens a shared :memory: sqlite DB with the test schema. The
// sqlite driver may be unavailable in some sandboxes; callers skip then.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(Config{DBDriver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("sqlite ping failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, testSchema); err != nil {
		t.Skipf("sqlite schema failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func execTestRow(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("fixture exec failed: %v", err)
	}
}

const testStamp = "2026-11-07T00:00:00Z"

func seedStoreFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	execTestRow(t, db, `INSERT OR REPLACE INTO nas (id, name, ip, secret, coa_port, created_at, updated_at)
		VALUES ('nas-store', 'store-nas', '192.0.2.10', 'storesecret', 3799, $1, $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO customers (id, name, created_at, updated_at)
		VALUES ('cust-store', 'Store', $1, $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO packages (id, name, price, period, rate_limit, address_pool,
		quota_bytes, quota_reset, on_exhausted, is_public, allow_self_service, created_at, updated_at)
		VALUES ('pkg-store', 'Store 10M', 100000, 'monthly', '10M/10M', 'pool-store',
		10737418240, 'monthly', 'reject', 1, 1, $1, $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO pppoe_accounts (id, customer_id, package_id, username, password,
		status, expires_at, static_ip, created_at, updated_at)
		VALUES ('acct-active', 'cust-store', 'pkg-store', 'store-user', 'pw-active', 'active', NULL, '198.51.100.7', $1, $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO pppoe_accounts (id, customer_id, package_id, username, password,
		status, expires_at, static_ip, created_at, updated_at)
		VALUES ('acct-isol', 'cust-store', 'pkg-store', 'store-isol', 'pw-isol', 'isolated', NULL, NULL, $1, $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO pppoe_accounts (id, customer_id, package_id, username, password,
		status, expires_at, static_ip, created_at, updated_at)
		VALUES ('acct-dis', 'cust-store', 'pkg-store', 'store-dis', 'pw-dis', 'disabled', NULL, NULL, $1, $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO settings (key, value, updated_at)
		VALUES ('acct_interim_interval', '120', $1)`, testStamp)
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM pppoe_accounts WHERE id IN ('acct-active','acct-isol','acct-dis')`,
			`DELETE FROM packages WHERE id = 'pkg-store'`,
			`DELETE FROM nas WHERE id = 'nas-store'`,
			`DELETE FROM customers WHERE id = 'cust-store'`,
			`DELETE FROM settings WHERE key = 'acct_interim_interval'`,
		} {
			_, _ = db.Exec(q)
		}
	})
}

func TestCacheLoadAllSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)

	cache := NewCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.LoadAll(ctx, db); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	nas, ok := cache.NASByIP("192.0.2.10")
	if !ok || nas.ID != "nas-store" || nas.Secret != "storesecret" || nas.CoAPort != 3799 {
		t.Fatalf("NASByIP = %+v,%v", nas, ok)
	}
	if _, ok := cache.NASByIP("192.0.2.99"); ok {
		t.Fatal("unknown NAS must not match")
	}
	a, ok := cache.Account("store-user")
	if !ok || a.Status != StatusActive || a.StaticIP != "198.51.100.7" || a.Password != "pw-active" {
		t.Fatalf("Account(active) = %+v,%v", a, ok)
	}
	if _, ok := cache.Account("store-isol"); !ok {
		t.Fatal("isolated account must be cached")
	}
	if _, ok := cache.Account("store-dis"); ok {
		t.Fatal("disabled account must not be cached")
	}
	p, ok := cache.Package("pkg-store")
	if !ok || p.RateLimit != "10M/10M" || !p.HasQuota || p.QuotaBytes != 10737418240 {
		t.Fatalf("Package = %+v,%v", p, ok)
	}
	if got := cache.InterimInterval(); got != 120 {
		t.Fatalf("InterimInterval = %d, want 120", got)
	}
	if got := cache.Setting(SettingIsolirRateLimit, DefaultIsolirRateLimit); got != DefaultIsolirRateLimit {
		t.Fatalf("missing setting must fall back, got %q", got)
	}
	accts, nass := cache.Counts()
	if accts != 2 || nass != 1 {
		t.Fatalf("Counts = %d,%d, want 2,1", accts, nass)
	}
}

func TestCacheReloadAndDropSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	cache := NewCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.LoadAll(ctx, db); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// Simulate billing renaming a user then disabling them.
	execTestRow(t, db, `UPDATE pppoe_accounts SET username = 'store-renamed' WHERE id = 'acct-active'`)
	if err := cache.ReloadRow(ctx, db, NotifyPPPoE, "acct-active"); err != nil {
		t.Fatalf("ReloadRow: %v", err)
	}
	if _, ok := cache.Account("store-renamed"); !ok {
		t.Fatal("renamed account missing after reload")
	}
	if _, ok := cache.Account("store-user"); ok {
		t.Fatal("old username must be gone after rename reload")
	}
	execTestRow(t, db, `UPDATE pppoe_accounts SET username = 'store-user', status = 'disabled' WHERE id = 'acct-active'`)
	if err := cache.ReloadRow(ctx, db, NotifyPPPoE, "acct-active"); err != nil {
		t.Fatalf("ReloadRow: %v", err)
	}
	if _, ok := cache.Account("store-user"); ok {
		t.Fatal("disabled account must drop out of cache on reload")
	}

	// NAS reload picks up an IP change; delete drops the entry.
	execTestRow(t, db, `UPDATE nas SET ip = '192.0.2.11' WHERE id = 'nas-store'`)
	if err := cache.ReloadRow(ctx, db, NotifyNAS, "nas-store"); err != nil {
		t.Fatalf("ReloadRow nas: %v", err)
	}
	if _, ok := cache.NASByIP("192.0.2.11"); !ok {
		t.Fatal("NAS new IP missing after reload")
	}
	if _, ok := cache.NASByIP("192.0.2.10"); ok {
		t.Fatal("NAS old IP must be gone after reload")
	}
	cache.DropRow(NotifyNAS, "nas-store")
	if _, ok := cache.NASByIP("192.0.2.11"); ok {
		t.Fatal("NAS must be gone after delete")
	}

	// Package delete drops the entry.
	cache.DropRow(NotifyPackage, "pkg-store")
	if _, ok := cache.Package("pkg-store"); ok {
		t.Fatal("package must be gone after delete")
	}
}

func TestWriterFlushSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	log := slog.New(slog.DiscardHandler)

	w := NewWriter(db, log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	bucket := "2026-11-01T00:00:00Z"
	w.Enqueue(Op{Kind: OpUsage, AccountID: "acct-active", PeriodStart: bucket, DeltaIn: 100, DeltaOut: 200, DeltaSecs: 60})
	w.Enqueue(Op{Kind: OpUsage, AccountID: "acct-active", PeriodStart: bucket, DeltaIn: 50, DeltaOut: 25, DeltaSecs: 30})
	w.Enqueue(Op{Kind: OpSessionUpsert, SessionID: "sess-1", AccountID: "acct-active", NASID: "nas-store",
		FramedIP: "198.51.100.7", TotalIn: 100, TotalOut: 200, StartedAt: testStamp, UpdatedAt: testStamp})
	w.Enqueue(Op{Kind: OpSessionStop, SessionID: "sess-1", AccountID: "acct-active", NASID: "nas-store",
		FramedIP: "198.51.100.7", TotalIn: 150, TotalOut: 225, StartedAt: testStamp,
		StoppedAt: testStamp, TerminateCause: "User-Request", UpdatedAt: testStamp})
	cancel() // triggers drain + final flush
	select {
	case <-w.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("writer did not finish drain")
	}

	qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer qcancel()
	var in, out, secs int64
	err := db.QueryRowContext(qctx, `SELECT bytes_in, bytes_out, online_seconds FROM usage_counters
		WHERE pppoe_account_id = $1 AND period_start = $2`, "acct-active", bucket).Scan(&in, &out, &secs)
	if err != nil {
		t.Fatalf("usage row missing: %v", err)
	}
	if in != 150 || out != 225 || secs != 90 {
		t.Fatalf("usage = %d,%d,%d, want 150,225,90", in, out, secs)
	}
	var stopped sql.NullString
	var cause sql.NullString
	var sin, sout int64
	err = db.QueryRowContext(qctx, `SELECT stopped_at, terminate_cause, bytes_in, bytes_out FROM radius_sessions
		WHERE session_id = $1 AND nas_id = $2`, "sess-1", "nas-store").Scan(&stopped, &cause, &sin, &sout)
	if err != nil {
		t.Fatalf("session row missing: %v", err)
	}
	if !stopped.Valid || cause.String != "User-Request" || sin != 150 || sout != 225 {
		t.Fatalf("session row = %+v %+v %d %d", stopped, cause, sin, sout)
	}
	_, _ = db.Exec(`DELETE FROM usage_counters WHERE pppoe_account_id = 'acct-active'`)
	_, _ = db.Exec(`DELETE FROM radius_sessions WHERE session_id = 'sess-1'`)
}

func TestPeriodStart(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	tests := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 11, 7, 12, 34, 56, 0, time.UTC), "2026-11-01T00:00:00Z"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2026-01-01T00:00:00Z"},
		// 2026-12-01 06:00+07:00 is still November in UTC.
		{time.Date(2026, 12, 1, 6, 0, 0, 0, loc), "2026-11-01T00:00:00Z"},
	}
	for _, tt := range tests {
		if got := PeriodStart(tt.in); got != tt.want {
			t.Fatalf("PeriodStart(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
