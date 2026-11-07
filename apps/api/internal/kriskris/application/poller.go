package kriskrisapp

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
)

// Poller knobs (PRD §6.4: ~5s cadence, batch ≤50, ≤60 req/min per API key).
// pollInterval/pollJitter are vars so tests can shrink them.
var (
	pollInterval    = 5 * time.Second
	pollJitter      = 500 * time.Millisecond
	pollBatchSize   = kriskrisdomain.MaxBatchIDs
	pollRebuildWait = 30 * time.Second
)

// StartPoller launches the backup poller: every ~5s it batch-checks pending
// attempts (≤50), applies newly-paid ones (re-verified via GET), and expires
// past-due ones. Requests are capped at 60/min via token bucket. It runs
// until ctx is done; the returned func stops it early (idempotent, waits).
//
// The service is built lazily from pool (env, else settings table): when
// kriskris is unconfigured the poller logs and retries every 30s instead of
// crashing boot.
func StartPoller(ctx context.Context, pool *sql.DB) func() {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runPoller(ctx, pool)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}

func runPoller(ctx context.Context, pool *sql.DB) {
	logger := log.Default()
	bucket := newTokenBucket(60, time.Second) // 60 req/min sustained
	var svc *Service
	timer := time.NewTimer(pollDelay())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if svc == nil {
			var err error
			svc, err = NewServiceFromPool(pool)
			if err != nil {
				logger.Printf("kriskris: poller unconfigured (%v); retry in %s", err, pollRebuildWait)
				timer.Reset(pollRebuildWait)
				continue
			}
		}
		if err := pollOnce(ctx, svc, bucket); err != nil {
			logger.Printf("kriskris: poll: %v", err)
		}
		timer.Reset(pollDelay())
	}
}

func pollDelay() time.Duration {
	if pollJitter <= 0 {
		return pollInterval
	}
	//nolint:gosec // jitter only, math/rand is fine
	return pollInterval + time.Duration(rand.Int63n(int64(pollJitter)))
}

// pollOnce is one backup sweep. It never returns fatal errors: every failure
// is logged by the caller and retried on the next tick.
func pollOnce(ctx context.Context, svc *Service, bucket *tokenBucket) error {
	now := time.Now().UTC()
	pendings, err := svc.Store().ListPendingBatch(ctx, pollBatchSize)
	if err != nil {
		return err
	}
	if len(pendings) == 0 {
		return nil
	}
	ids := make([]string, 0, len(pendings))
	for _, att := range pendings {
		ids = append(ids, att.KriskrisID)
	}
	statuses := map[string]string{}
	if !bucket.Allow() {
		return nil // rate-capped: skip this tick entirely, retry next tick
	}
	rows, err := svc.Client().BatchStatus(ctx, ids)
	if err != nil {
		return err
	}
	for _, r := range rows {
		statuses[r.ID] = r.Status
	}
	var expired []string
	for _, att := range pendings {
		st, known := statuses[att.KriskrisID]
		if known && st == kriskrisdomain.AttemptPaid {
			// Paid per batch: re-verify via GET inside ConfirmPaid, then
			// apply in one transaction (same path as the webhook).
			if !bucket.Allow() {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			cerr := svc.ConfirmPaid(cctx, att.KriskrisID)
			cancel()
			if cerr != nil && !errors.Is(cerr, ErrNotPaidYet) {
				log.Printf("kriskris: poll confirm %s: %v", att.KriskrisID, cerr)
			}
			continue
		}
		if !now.Before(att.ExpiresAt) {
			expired = append(expired, att.ID)
		}
	}
	if len(expired) > 0 {
		if err := svc.Store().MarkExpired(ctx, expired, now); err != nil {
			return err
		}
	}
	return nil
}

// tokenBucket is a tiny time-based limiter (no deps).
type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens per second
	last   time.Time
}

func newTokenBucket(max int, per time.Duration) *tokenBucket {
	rate := float64(max) / per.Seconds()
	return &tokenBucket{tokens: float64(max), max: float64(max), rate: rate, last: time.Now()}
}

// Allow consumes one token when available.
func (b *tokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
