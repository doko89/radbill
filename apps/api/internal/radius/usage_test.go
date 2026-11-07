package radius

import (
	"context"
	"log/slog"
	"testing"
	"time"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2866"
)

func TestBucketStart(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	tests := []struct {
		name   string
		reset  string
		anchor string
		now    time.Time
		want   string
	}{
		{name: "none is eternal", reset: "none", now: time.Date(2026, 11, 7, 12, 34, 56, 0, time.UTC), want: EternalBucket},
		{name: "none ignores anchor", reset: "none", anchor: "2026-01-01T00:00:00Z", now: time.Date(2027, 5, 5, 5, 5, 5, 0, time.UTC), want: EternalBucket},

		{name: "daily", reset: "daily", now: time.Date(2026, 11, 7, 12, 34, 56, 0, time.UTC), want: "2026-11-07T00:00:00Z"},
		{name: "daily at midnight", reset: "daily", now: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), want: "2026-11-07T00:00:00Z"},
		{name: "daily just before midnight", reset: "daily", now: time.Date(2026, 11, 7, 23, 59, 59, 0, time.UTC), want: "2026-11-07T00:00:00Z"},
		// 2026-11-08 06:00+07:00 is still Nov 7 in UTC.
		{name: "daily uses UTC", reset: "daily", now: time.Date(2026, 11, 8, 6, 0, 0, 0, loc), want: "2026-11-07T00:00:00Z"},

		{name: "monthly", reset: "monthly", now: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), want: "2026-11-01T00:00:00Z"},
		{name: "monthly first day", reset: "monthly", now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), want: "2026-01-01T00:00:00Z"},

		{name: "cycle before anniversary", reset: "billing_cycle", anchor: "2026-10-15T10:00:00Z",
			now: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), want: "2026-10-15T00:00:00Z"},
		{name: "cycle on anniversary", reset: "billing_cycle", anchor: "2026-10-15T10:00:00Z",
			now: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), want: "2026-11-15T00:00:00Z"},
		{name: "cycle just before anniversary", reset: "billing_cycle", anchor: "2026-10-15T10:00:00Z",
			now: time.Date(2026, 11, 14, 23, 59, 59, 0, time.UTC), want: "2026-10-15T00:00:00Z"},
		{name: "cycle anchor day-of-month only", reset: "billing_cycle", anchor: "2025-03-15T23:59:59+07:00",
			now: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), want: "2026-11-15T00:00:00Z"},
		// 31st anchor clamps to Feb 28 in 2026 (not a leap year).
		{name: "cycle clamps short month", reset: "billing_cycle", anchor: "2026-01-31T08:00:00Z",
			now: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), want: "2026-02-28T00:00:00Z"},
		{name: "cycle clamp boundary", reset: "billing_cycle", anchor: "2026-01-31T08:00:00Z",
			now: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), want: "2026-01-31T00:00:00Z"},
		{name: "cycle bad anchor falls back to monthly", reset: "billing_cycle", anchor: "not-a-time",
			now: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), want: "2026-11-01T00:00:00Z"},
		{name: "cycle empty anchor falls back to monthly", reset: "billing_cycle", anchor: "",
			now: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), want: "2026-11-01T00:00:00Z"},

		{name: "empty reset falls back to monthly", reset: "",
			now: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), want: "2026-11-01T00:00:00Z"},
		{name: "unknown reset falls back to monthly", reset: "weekly",
			now: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), want: "2026-11-01T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BucketStart(tt.reset, tt.anchor, tt.now); got != tt.want {
				t.Fatalf("BucketStart(%q,%q,%v) = %q, want %q",
					tt.reset, tt.anchor, tt.now, got, tt.want)
			}
		})
	}
}

func TestUsageMirrorRollover(t *testing.T) {
	newYears := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		reset  string
		anchor string
		first  time.Time
		second time.Time
		rolled bool
		bucket string
	}{
		{name: "daily same day", reset: "daily", first: newYears.Add(time.Hour), second: newYears.Add(2 * time.Hour),
			bucket: "2026-01-01T00:00:00Z"},
		{name: "daily rolls at midnight", reset: "daily", first: newYears.Add(-time.Hour), second: newYears.Add(time.Hour),
			rolled: true, bucket: "2026-01-01T00:00:00Z"},
		{name: "monthly same month", reset: "monthly", first: newYears.AddDate(0, 0, 5), second: newYears.AddDate(0, 0, 20),
			bucket: "2026-01-01T00:00:00Z"},
		{name: "monthly rolls", reset: "monthly", first: newYears.AddDate(0, 0, 5), second: newYears.AddDate(0, 1, 0),
			rolled: true, bucket: "2026-02-01T00:00:00Z"},
		{name: "none never rolls", reset: "none", first: newYears, second: newYears.AddDate(3, 0, 0), bucket: EternalBucket},
		{name: "cycle same period", reset: "billing_cycle", anchor: "2026-01-15T10:00:00Z",
			first: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), second: time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC),
			bucket: "2026-01-15T00:00:00Z"},
		{name: "cycle rolls on anniversary", reset: "billing_cycle", anchor: "2026-01-15T10:00:00Z",
			first: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), second: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			rolled: true, bucket: "2026-02-15T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewUsageMirror(nil)
			if _, rolled := m.Add("a1", tt.reset, tt.anchor, tt.first, 100, 200, 60); rolled {
				t.Fatal("first touch must not report rollover")
			}
			got, rolled := m.Add("a1", tt.reset, tt.anchor, tt.second, 10, 20, 6)
			if rolled != tt.rolled {
				t.Fatalf("rolled = %v, want %v", rolled, tt.rolled)
			}
			if got.PeriodStart != tt.bucket {
				t.Fatalf("bucket = %q, want %q", got.PeriodStart, tt.bucket)
			}
			wantIn, wantOut, wantSecs := uint64(110), uint64(220), uint64(66)
			if tt.rolled {
				wantIn, wantOut, wantSecs = 10, 20, 6
			}
			if got.BytesIn != wantIn || got.BytesOut != wantOut || got.OnlineSeconds != wantSecs {
				t.Fatalf("usage = %d,%d,%d, want %d,%d,%d",
					got.BytesIn, got.BytesOut, got.OnlineSeconds, wantIn, wantOut, wantSecs)
			}
			// A pure read (auth path) also rolls forward without adding.
			m2 := NewUsageMirror(nil)
			m2.Add("a1", tt.reset, tt.anchor, tt.first, 100, 200, 60)
			snap := m2.Snapshot("a1", tt.reset, tt.anchor, tt.second)
			if snap.PeriodStart != tt.bucket || (tt.rolled && snap.UsedBytes() != 0) {
				t.Fatalf("snapshot after read = %+v, want bucket %q", snap, tt.bucket)
			}
		})
	}
}

func TestUsageMirrorWarmupSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	execTestRow(t, db, `INSERT OR REPLACE INTO usage_counters (pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds)
		VALUES ('acct-active', '2026-10-01T00:00:00Z', 111, 222, 33)`)
	execTestRow(t, db, `INSERT OR REPLACE INTO usage_counters (pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds)
		VALUES ('acct-active', '2026-11-01T00:00:00Z', 1000, 2000, 60)`)
	execTestRow(t, db, `INSERT OR REPLACE INTO quota_topups (id, pppoe_account_id, bytes, period_start, invoice_id, created_at)
		VALUES ('top-cur', 'acct-active', 5000, '2026-11-01T00:00:00Z', 'inv-1', $1)`, testStamp)
	execTestRow(t, db, `INSERT OR REPLACE INTO quota_topups (id, pppoe_account_id, bytes, period_start, invoice_id, created_at)
		VALUES ('top-old', 'acct-active', 9999, '2026-10-01T00:00:00Z', 'inv-0', $1)`, testStamp)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM usage_counters WHERE pppoe_account_id = 'acct-active'`)
		_, _ = db.Exec(`DELETE FROM quota_topups WHERE pppoe_account_id = 'acct-active'`)
	})

	m := NewUsageMirror(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Warmup(ctx, db); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	got := m.Snapshot("acct-active", "monthly", "", now)
	if got.PeriodStart != "2026-11-01T00:00:00Z" {
		t.Fatalf("bucket = %q", got.PeriodStart)
	}
	if got.BytesIn != 1000 || got.BytesOut != 2000 || got.OnlineSeconds != 60 {
		t.Fatalf("usage = %+v, want latest row", got)
	}
	if got.Topups != 5000 {
		t.Fatalf("topups = %d, want 5000 (current bucket only)", got.Topups)
	}
	if snap := m.Snapshot("acct-unknown", "monthly", "", now); snap.UsedBytes() != 0 || snap.Topups != 0 {
		t.Fatalf("unknown account = %+v, want zeros", snap)
	}
}

func TestUsageMirrorWarmupWithoutTopupTable(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	execTestRow(t, db, `INSERT OR REPLACE INTO usage_counters (pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds)
		VALUES ('acct-active', '2026-11-01T00:00:00Z', 7, 8, 9)`)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM usage_counters WHERE pppoe_account_id = 'acct-active'`)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Simulate the D2 table not existing yet: warmup must degrade to zero
	// top-ups instead of failing the boot. The table is restored for the
	// other tests sharing this in-memory DB.
	if _, err := db.ExecContext(ctx, `DROP TABLE quota_topups`); err != nil {
		t.Fatalf("drop quota_topups: %v", err)
	}
	defer func() {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS quota_topups (
			id TEXT PRIMARY KEY, pppoe_account_id TEXT NOT NULL, bytes BIGINT NOT NULL,
			period_start TEXT NOT NULL, invoice_id TEXT NOT NULL, created_at TEXT NOT NULL)`)
	}()
	m := NewUsageMirror(nil)
	if err := m.Warmup(ctx, db); err != nil {
		t.Fatalf("Warmup without quota_topups must not fail: %v", err)
	}
	got := m.Snapshot("acct-active", "monthly", "", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC))
	if got.BytesIn != 7 || got.Topups != 0 {
		t.Fatalf("got %+v, want usage kept with zero topups", got)
	}
}

// TestMirrorVsDBAfterInterimSQLite drives Start/Interim/Stop through the
// accounting server and asserts the in-memory mirror matches the flushed
// DB row (same bucket, same totals).
func TestMirrorVsDBAfterInterimSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	defer func() {
		_, _ = db.Exec(`DELETE FROM usage_counters WHERE pppoe_account_id = 'acct-active'`)
		_, _ = db.Exec(`DELETE FROM radius_sessions WHERE nas_id = 'nas-store'`)
	}()
	cache := NewCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.LoadAll(ctx, db); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	mirror := NewUsageMirror(nil)
	if err := mirror.Warmup(ctx, db); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	fixedNow := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	log := slog.New(slog.DiscardHandler)
	writer := NewWriter(db, log)
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	go writer.Run(wctx)
	srv := &AcctServer{Cache: cache, Log: log, Sessions: NewSessionTracker(),
		Writer: writer, Usage: mirror, Now: func() time.Time { return fixedNow }}

	cause := rfc2866.AcctTerminateCause_Value_UserRequest
	for i, pkt := range []*lradius.Packet{
		acctPacket(t, rfc2866.AcctStatusType_Value_Start, "store-user", "sess-mirror", 0, 0, 0, nil),
		acctPacket(t, rfc2866.AcctStatusType_Value_InterimUpdate, "store-user", "sess-mirror", 1000, 2000, 60, nil),
		acctPacket(t, rfc2866.AcctStatusType_Value_InterimUpdate, "store-user", "sess-mirror", 1500, 2500, 120, nil),
		acctPacket(t, rfc2866.AcctStatusType_Value_Stop, "store-user", "sess-mirror", 1600, 2600, 150, &cause),
	} {
		if got := serveAcct(srv, pkt, "192.0.2.10"); got == nil || got.Code != lradius.CodeAccountingResponse {
			t.Fatalf("step %d: got %+v, want Accounting-Response", i, got)
		}
	}
	wcancel()
	select {
	case <-writer.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("writer did not finish drain")
	}

	snap := mirror.Snapshot("acct-active", "monthly", "", fixedNow)
	if snap.PeriodStart != "2026-11-01T00:00:00Z" ||
		snap.BytesIn != 1600 || snap.BytesOut != 2600 || snap.OnlineSeconds != 150 {
		t.Fatalf("mirror = %+v", snap)
	}
	qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer qcancel()
	var in, out, secs int64
	err := db.QueryRowContext(qctx, `SELECT bytes_in, bytes_out, online_seconds FROM usage_counters
		WHERE pppoe_account_id = $1 AND period_start = $2`,
		"acct-active", snap.PeriodStart).Scan(&in, &out, &secs)
	if err != nil {
		t.Fatalf("usage row missing: %v", err)
	}
	if in != int64(snap.BytesIn) || out != int64(snap.BytesOut) || secs != int64(snap.OnlineSeconds) {
		t.Fatalf("db = %d,%d,%d, mirror = %+v", in, out, secs, snap)
	}
}

// TestInterimRolloverMaterializesNewBucketSQLite seeds the mirror in an old
// bucket, then runs a Start in the new bucket: the mirror must roll and
// the batch writer must persist the fresh (zero) bucket row.
func TestInterimRolloverMaterializesNewBucketSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	defer func() {
		_, _ = db.Exec(`DELETE FROM usage_counters WHERE pppoe_account_id = 'acct-active'`)
		_, _ = db.Exec(`DELETE FROM radius_sessions WHERE nas_id = 'nas-store'`)
	}()
	cache := NewCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.LoadAll(ctx, db); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	mirror := NewUsageMirror(nil)
	if err := mirror.Warmup(ctx, db); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	mirror.Add("acct-active", "monthly", "", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), 100, 100, 10)

	dec5 := time.Date(2026, 12, 5, 10, 0, 0, 0, time.UTC)
	log := slog.New(slog.DiscardHandler)
	writer := NewWriter(db, log)
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	go writer.Run(wctx)
	srv := &AcctServer{Cache: cache, Log: log, Sessions: NewSessionTracker(),
		Writer: writer, Usage: mirror, Now: func() time.Time { return dec5 }}

	pkt := acctPacket(t, rfc2866.AcctStatusType_Value_Start, "store-user", "sess-roll", 0, 0, 0, nil)
	if got := serveAcct(srv, pkt, "192.0.2.10"); got == nil || got.Code != lradius.CodeAccountingResponse {
		t.Fatalf("got %+v, want Accounting-Response", got)
	}
	wcancel()
	select {
	case <-writer.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("writer did not finish drain")
	}

	snap := mirror.Snapshot("acct-active", "monthly", "", dec5)
	if snap.PeriodStart != "2026-12-01T00:00:00Z" || snap.UsedBytes() != 0 {
		t.Fatalf("mirror = %+v, want fresh December bucket", snap)
	}
	qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer qcancel()
	var in, out, secs int64
	err := db.QueryRowContext(qctx, `SELECT bytes_in, bytes_out, online_seconds FROM usage_counters
		WHERE pppoe_account_id = $1 AND period_start = $2`,
		"acct-active", "2026-12-01T00:00:00Z").Scan(&in, &out, &secs)
	if err != nil {
		t.Fatalf("new bucket row missing: %v", err)
	}
	if in != 0 || out != 0 || secs != 0 {
		t.Fatalf("new bucket row = %d,%d,%d, want zeros", in, out, secs)
	}
}

func TestUsageMirrorTopupRefreshSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM quota_topups WHERE pppoe_account_id = 'acct-active'`)
	})
	m := NewUsageMirror(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Warmup(ctx, db); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	if got := m.Snapshot("acct-active", "monthly", "", now); got.Topups != 0 {
		t.Fatalf("topups before purchase = %d", got.Topups)
	}
	// A purchase lands in quota_topups; the next poll refresh picks it up.
	execTestRow(t, db, `INSERT INTO quota_topups (id, pppoe_account_id, bytes, period_start, invoice_id, created_at)
		VALUES ('top-new', 'acct-active', 1024, '2026-11-01T00:00:00Z', 'inv-9', $1)`, testStamp)
	if err := m.RefreshTopups(ctx, db); err != nil {
		t.Fatalf("RefreshTopups: %v", err)
	}
	if got := m.Snapshot("acct-active", "monthly", "", now); got.Topups != 1024 {
		t.Fatalf("topups after refresh = %d, want 1024", got.Topups)
	}
}
