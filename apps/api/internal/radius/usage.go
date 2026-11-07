// Usage mirror: in-memory per-account usage cache with lazy period
// rollover (Fase 3 quota/time/FUP enforcement).
//
// Auth and accounting never query the database per request. Instead this
// mirror holds one entry per account -- {period_start, bytes_in,
// bytes_out, online_seconds} plus the top-ups of the running period --
// warmed from usage_counters + quota_topups at boot and advanced by every
// accounting delta. The batch Writer stays the only path that persists
// usage; the mirror is eventually consistent with the DB:
//
//   - unflushed deltas (writer queue, ~2s) exist only in the mirror;
//   - a restart reloads the last flushed values (dropped ops, if the queue
//     overflowed, are lost in both);
//   - top-ups land via periodic refresh (see RunWatcher), so a purchase
//     takes effect within one poll interval;
//   - DB rows for a new bucket materialise on the next accounting write
//     (or an explicit zero-op on rollover), never from the auth path.
package radius

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// QuotaReset modes stored in packages.quota_reset.
const (
	QuotaResetNone         = "none"
	QuotaResetDaily        = "daily"
	QuotaResetMonthly      = "monthly"
	QuotaResetBillingCycle = "billing_cycle"
)

// EternalBucket is the single usage bucket for quota_reset=none. Usage
// accumulates forever and never rolls over. Billing must store top-ups for
// such packages under this same key.
const EternalBucket = "1970-01-01T00:00:00Z"

const (
	qListUsage  = `SELECT pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds FROM usage_counters`
	qListTopups = `SELECT pppoe_account_id, period_start, bytes FROM quota_topups`
)

// BucketStart returns the usage-bucket key (RFC3339 UTC) covering now for
// the given quota_reset mode. anchor is the account created_at (RFC3339),
// used only by billing_cycle:
//
//   - none: one eternal bucket (never rolls over).
//   - daily: midnight UTC of the current day.
//   - monthly: midnight UTC of the first of the month (same as PeriodStart).
//   - billing_cycle: midnight UTC of the most recent monthly anniversary
//     of the account creation day; the day clamps to the month length (a
//     31st anchor rolls on the 28th/29th/30th in short months).
//
// Empty or unknown modes fall back to monthly (the G1 bucket), and a
// missing or unparseable billing_cycle anchor falls back to monthly as
// well. Pure: no I/O.
func BucketStart(reset, anchor string, now time.Time) string {
	u := now.UTC()
	switch reset {
	case QuotaResetNone:
		return EternalBucket
	case QuotaResetDaily:
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	case QuotaResetBillingCycle:
		if b, ok := cycleBucket(anchor, u); ok {
			return b
		}
		return PeriodStart(now)
	case QuotaResetMonthly, "":
		return PeriodStart(now)
	default:
		return PeriodStart(now)
	}
}

// cycleBucket resolves the billing_cycle bucket for an anchor instant.
func cycleBucket(anchor string, now time.Time) (string, bool) {
	t, err := time.Parse(time.RFC3339, anchor)
	if err != nil {
		return "", false
	}
	day := t.UTC().Day()
	cand := cycleDay(now.Year(), now.Month(), day)
	if cand.After(now) {
		// This month's anniversary is still ahead: use last month's.
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		cand = cycleDay(prev.Year(), prev.Month(), day)
	}
	return cand.Format(time.RFC3339), true
}

// cycleDay returns midnight UTC of day in the given month, clamped to the
// month length.
func cycleDay(year int, month time.Month, day int) time.Time {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// UsageSnapshot is the account-level usage a decision needs: cumulative
// bytes and online seconds in the running bucket plus its top-ups.
type UsageSnapshot struct {
	UsedBytes   uint64
	UsedSeconds uint64
	TopupBytes  int64
}

// UsageEntry is one account's cached usage for its current bucket.
type UsageEntry struct {
	PeriodStart   string
	BytesIn       uint64
	BytesOut      uint64
	OnlineSeconds uint64
	Topups        int64
}

// UsedBytes returns the total bytes (in + out) in the entry.
func (e UsageEntry) UsedBytes() uint64 { return e.BytesIn + e.BytesOut }

// Snapshot converts the entry to the decision-friendly snapshot shape.
func (e UsageEntry) Snapshot() UsageSnapshot {
	return UsageSnapshot{UsedBytes: e.UsedBytes(), UsedSeconds: e.OnlineSeconds, TopupBytes: e.Topups}
}

// UsageMirror is the in-memory usage mirror described above. It is safe
// for concurrent use by the auth and accounting paths.
type UsageMirror struct {
	mu      sync.RWMutex
	entries map[string]*UsageEntry      // by pppoe_account_id
	topups  map[string]map[string]int64 // account -> period_start -> bytes
	log     *slog.Logger
}

// NewUsageMirror returns an empty mirror. log may be nil (discard).
func NewUsageMirror(log *slog.Logger) *UsageMirror {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &UsageMirror{
		entries: make(map[string]*UsageEntry),
		topups:  make(map[string]map[string]int64),
		log:     log,
	}
}

// Warmup loads usage_counters (latest row per account) and quota_topups
// into the mirror. A usage_counters failure is returned (the table is a
// committed contract); a quota_topups failure only warns and degrades to
// zero top-ups, retried by RefreshTopups on every poll tick.
func (m *UsageMirror) Warmup(ctx context.Context, db *sql.DB) error {
	if err := m.loadUsage(ctx, db); err != nil {
		return err
	}
	if err := m.loadTopups(ctx, db); err != nil {
		m.log.Warn("usage warmup: topups unavailable, assuming zero until refresh", "err", err)
		return nil
	}
	m.attachTopups()
	return nil
}

// RefreshTopups reloads quota_topups (billing writes rows on purchase; the
// mirror has no per-row invalidation for them). Best-effort: the caller
// logs failures and retries next tick.
func (m *UsageMirror) RefreshTopups(ctx context.Context, db *sql.DB) error {
	if err := m.loadTopups(ctx, db); err != nil {
		return err
	}
	m.attachTopups()
	return nil
}

// Snapshot returns the current-bucket usage for one account, rolling the
// mirror forward when the bucket changed since the last touch. The auth
// path calls this; it performs no I/O.
func (m *UsageMirror) Snapshot(accountID, reset, anchor string, now time.Time) UsageEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, _ := m.ensureLocked(accountID, reset, anchor, now)
	return *e
}

// Add records one accounting delta, rolling the bucket forward first when
// needed. It returns the post-add entry and whether a rollover happened
// (rolled is false for a first-seen account). The caller persists deltas
// through the batch Writer under entry.PeriodStart.
func (m *UsageMirror) Add(accountID, reset, anchor string, now time.Time, dIn, dOut, dSecs uint64) (UsageEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, rolled := m.ensureLocked(accountID, reset, anchor, now)
	e.BytesIn += dIn
	e.BytesOut += dOut
	e.OnlineSeconds += dSecs
	return *e, rolled
}

// ensureLocked returns the entry for the bucket covering now, resetting a
// stale entry (any mismatch, past or future) to the current bucket.
func (m *UsageMirror) ensureLocked(accountID, reset, anchor string, now time.Time) (*UsageEntry, bool) {
	bucket := BucketStart(reset, anchor, now)
	e, ok := m.entries[accountID]
	if !ok {
		e = &UsageEntry{PeriodStart: bucket, Topups: m.topupsFor(accountID, bucket)}
		m.entries[accountID] = e
		return e, false
	}
	if e.PeriodStart != bucket {
		*e = UsageEntry{PeriodStart: bucket, Topups: m.topupsFor(accountID, bucket)}
		return e, true
	}
	// Top-ups may have refreshed since the entry was created.
	e.Topups = m.topupsFor(accountID, bucket)
	return e, false
}

func (m *UsageMirror) topupsFor(accountID, bucket string) int64 {
	return m.topups[accountID][bucket]
}

func (m *UsageMirror) loadUsage(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, qListUsage)
	if err != nil {
		return err
	}
	defer rows.Close()
	fresh := make(map[string]*UsageEntry)
	for rows.Next() {
		var id, period string
		var in, out, secs int64
		if err := rows.Scan(&id, &period, &in, &out, &secs); err != nil {
			return err
		}
		cur, ok := fresh[id]
		if ok && period <= cur.PeriodStart {
			continue // keep the latest bucket only; older rows are history
		}
		fresh[id] = &UsageEntry{
			PeriodStart:   period,
			BytesIn:       u64nz(in),
			BytesOut:      u64nz(out),
			OnlineSeconds: u64nz(secs),
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = fresh
	return nil
}

func (m *UsageMirror) loadTopups(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, qListTopups)
	if err != nil {
		return err
	}
	defer rows.Close()
	fresh := make(map[string]map[string]int64)
	for rows.Next() {
		var id, period string
		var b int64
		if err := rows.Scan(&id, &period, &b); err != nil {
			return err
		}
		if b < 0 {
			b = 0
		}
		per, ok := fresh[id]
		if !ok {
			per = make(map[string]int64)
			fresh[id] = per
		}
		per[period] += b
	}
	if err := rows.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topups = fresh
	return nil
}

// attachTopups re-points every cached entry at its bucket's top-up sum
// (after warmup or refresh).
func (m *UsageMirror) attachTopups() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.entries {
		e.Topups = m.topups[id][e.PeriodStart]
	}
}

func u64nz(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}
