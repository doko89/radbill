package radius

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"boilerplate/api/internal/shared/id"
)

// PeriodStart returns the monthly usage bucket for t: midnight UTC on the
// first of the month, RFC3339. Computed in Go (never SQL functions) so the
// value is identical on sqlite and postgres.
func PeriodStart(t time.Time) string {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

// SessionKey identifies one NAS session: Acct-Session-Id is unique per NAS.
type SessionKey struct {
	NASID     string
	SessionID string
}

// SessionTracker keeps the last accounting sample per session in memory so
// Interim/Stop updates convert to deltas. It is the only session state on
// the accounting fast path; rows are written asynchronously by Writer.
type SessionTracker struct {
	mu      sync.Mutex
	last    map[SessionKey]Totals
	stopped map[SessionKey]stoppedEntry // tombstones against Stop retransmits

	lastSweep time.Time
}

type stoppedEntry struct {
	totals Totals
	at     time.Time
}

// tombstoneTTL bounds how long a stopped session is remembered. A Stop
// retransmit (same totals) inside the window is a duplicate; a reused
// session id (different totals) reopens the session.
const tombstoneTTL = time.Hour

// NewSessionTracker returns an empty tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		last:    make(map[SessionKey]Totals),
		stopped: make(map[SessionKey]stoppedEntry),
	}
}

// Update records the current sample and returns the delta against the
// previous one. existed is false for the first sample of a session, in
// which case the delta counts from zero.
func (t *SessionTracker) Update(key SessionKey, cur Totals) (Delta, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tomb, found := t.stopped[key]; found {
		if cur == tomb.totals {
			return Delta{Duplicate: true}, true
		}
		delete(t.stopped, key) // session id reused: reopen
	}
	last, existed := t.last[key]
	t.last[key] = cur
	if !existed {
		last = Totals{}
	}
	return ComputeDelta(last, cur), existed
}

// Stop forgets a live session and plants a tombstone with its final totals
// so a retransmitted Stop is recognised as a duplicate.
func (t *SessionTracker) Stop(key SessionKey, cur Totals) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.last, key)
	t.stopped[key] = stoppedEntry{totals: cur, at: time.Now()}
	t.sweepLocked()
}

// Reopen clears any tombstone for key (a fresh Start reuses the id).
func (t *SessionTracker) Reopen(key SessionKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.stopped, key)
}

// Remove forgets a session entirely, tombstone included.
func (t *SessionTracker) Remove(key SessionKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.last, key)
	delete(t.stopped, key)
}

// RemoveNAS forgets all sessions of one NAS (after Accounting-On/Off) and
// returns how many live entries were dropped.
func (t *SessionTracker) RemoveNAS(nasID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for k := range t.last {
		if k.NASID == nasID {
			delete(t.last, k)
			n++
		}
	}
	for k := range t.stopped {
		if k.NASID == nasID {
			delete(t.stopped, k)
		}
	}
	return n
}

// sweepLocked evicts expired tombstones (at most every few minutes).
func (t *SessionTracker) sweepLocked() {
	now := time.Now()
	if now.Sub(t.lastSweep) < 5*time.Minute {
		return
	}
	t.lastSweep = now
	for k, tomb := range t.stopped {
		if now.Sub(tomb.at) > tombstoneTTL {
			delete(t.stopped, k)
		}
	}
}

// OpKind identifies one batched accounting write.
type OpKind int

const (
	// OpUsage adds a delta to usage_counters (monthly bucket).
	OpUsage OpKind = iota
	// OpSessionUpsert inserts or refreshes a radius_sessions row (Start/Interim).
	OpSessionUpsert
	// OpSessionStop finalises a radius_sessions row (Stop).
	OpSessionStop
	// OpCloseOrphans closes all open sessions of one NAS (Accounting-On/Off).
	OpCloseOrphans
)

// Op is one accounting write, applied asynchronously by Writer.
type Op struct {
	Kind OpKind

	AccountID   string
	PeriodStart string
	DeltaIn     int64
	DeltaOut    int64
	DeltaSecs   int64

	SessionID      string
	NASID          string
	FramedIP       string
	TotalIn        int64
	TotalOut       int64
	StartedAt      string
	StoppedAt      string
	TerminateCause string
	UpdatedAt      string
}

// Writer statements run on both sqlite and postgres ($1, ON CONFLICT).
const (
	qUpsertUsage = `INSERT INTO usage_counters (pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (pppoe_account_id, period_start) DO UPDATE SET
			bytes_in = usage_counters.bytes_in + EXCLUDED.bytes_in,
			bytes_out = usage_counters.bytes_out + EXCLUDED.bytes_out,
			online_seconds = usage_counters.online_seconds + EXCLUDED.online_seconds`

	qUpsertSession = `INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id, framed_ip,
			started_at, stopped_at, bytes_in, bytes_out, terminate_cause, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8, NULL, $9)
		ON CONFLICT (session_id, nas_id) DO UPDATE SET
			framed_ip = EXCLUDED.framed_ip,
			bytes_in = EXCLUDED.bytes_in,
			bytes_out = EXCLUDED.bytes_out,
			updated_at = EXCLUDED.updated_at`

	qStopSession = `INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id, framed_ip,
			started_at, stopped_at, bytes_in, bytes_out, terminate_cause, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (session_id, nas_id) DO UPDATE SET
			framed_ip = EXCLUDED.framed_ip,
			stopped_at = EXCLUDED.stopped_at,
			bytes_in = EXCLUDED.bytes_in,
			bytes_out = EXCLUDED.bytes_out,
			terminate_cause = EXCLUDED.terminate_cause,
			updated_at = EXCLUDED.updated_at`

	qCloseOrphans = `UPDATE radius_sessions SET stopped_at = $1, terminate_cause = $2, updated_at = $1
		WHERE nas_id = $3 AND stopped_at IS NULL`
)

// OrphanTerminateCause marks sessions closed by Accounting-On/Off.
const OrphanTerminateCause = "NAS-Reboot"

// Writer batches accounting writes: ops queue on a channel and flush every
// ~2s or 500 rows, whichever comes first.
type Writer struct {
	db  *sql.DB
	log *slog.Logger

	ch            chan Op
	flushInterval time.Duration
	batchSize     int

	done    chan struct{}
	dropped int64
}

// NewWriter returns a Writer with a 8192-op queue.
func NewWriter(db *sql.DB, log *slog.Logger) *Writer {
	return &Writer{
		db:            db,
		log:           log,
		ch:            make(chan Op, 8192),
		flushInterval: 2 * time.Second,
		batchSize:     500,
		done:          make(chan struct{}),
	}
}

// Enqueue buffers one op. When the queue is full the op is dropped and
// counted rather than blocking the UDP path.
func (w *Writer) Enqueue(op Op) {
	select {
	case w.ch <- op:
	default:
		w.dropped++
		w.log.Warn("accounting queue full, dropping op", "kind", int(op.Kind), "dropped_total", w.dropped)
	}
}

// QueueLen returns the queued op count (for /healthz).
func (w *Writer) QueueLen() int { return len(w.ch) }

// Done closes when Run has flushed and exited.
func (w *Writer) Done() <-chan struct{} { return w.done }

// Run flushes batches until ctx is cancelled, then drains the queue with
// one final flush.
func (w *Writer) Run(ctx context.Context) {
	defer close(w.done)
	t := time.NewTicker(w.flushInterval)
	defer t.Stop()
	var pending []Op
	flush := func() {
		if len(pending) == 0 {
			return
		}
		w.flush(pending)
		pending = pending[:0]
	}
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case op := <-w.ch:
					pending = append(pending, op)
					if len(pending) >= w.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case op := <-w.ch:
			pending = append(pending, op)
			if len(pending) >= w.batchSize {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

func (w *Writer) flush(ops []Op) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		w.log.Warn("accounting flush: begin failed", "err", err, "ops", len(ops))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for i := range ops {
		if err := applyOp(ctx, tx, &ops[i]); err != nil {
			w.log.Warn("accounting flush: op failed", "err", err, "kind", int(ops[i].Kind))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		w.log.Warn("accounting flush: commit failed", "err", err, "ops", len(ops))
		return
	}
	committed = true
}

func applyOp(ctx context.Context, tx *sql.Tx, op *Op) error {
	switch op.Kind {
	case OpUsage:
		_, err := tx.ExecContext(ctx, qUpsertUsage, op.AccountID, op.PeriodStart,
			op.DeltaIn, op.DeltaOut, op.DeltaSecs)
		return err
	case OpSessionUpsert:
		_, err := tx.ExecContext(ctx, qUpsertSession, id.New(), op.SessionID,
			op.AccountID, op.NASID, nullText(op.FramedIP), op.StartedAt,
			op.TotalIn, op.TotalOut, op.UpdatedAt)
		return err
	case OpSessionStop:
		_, err := tx.ExecContext(ctx, qStopSession, id.New(), op.SessionID,
			op.AccountID, op.NASID, nullText(op.FramedIP), op.StartedAt,
			op.StoppedAt, op.TotalIn, op.TotalOut,
			nullText(op.TerminateCause), op.UpdatedAt)
		return err
	case OpCloseOrphans:
		_, err := tx.ExecContext(ctx, qCloseOrphans, op.StoppedAt,
			OrphanTerminateCause, op.NASID)
		return err
	default:
		return nil
	}
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
