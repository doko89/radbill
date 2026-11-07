package application

import (
	"context"
	"log"
	"sync"
	"time"
)

// DefaultInterval is the ticker period when none is configured.
const DefaultInterval = 60 * time.Second

// Runner ticks RunOnce on an interval with single-flight: a tick arriving
// while the previous run is still active is skipped, never queued.
type Runner struct {
	svc      *Service
	interval time.Duration
	mu       sync.Mutex
}

// NewRunner builds a Runner. Non-positive intervals fall back to DefaultInterval.
func NewRunner(svc *Service, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Runner{svc: svc, interval: interval}
}

// RunOnce executes one run when no other run is active, reporting whether
// the run happened. It is exported for tests; the loop uses it too.
func (r *Runner) RunOnce(ctx context.Context) bool {
	if !r.mu.TryLock() {
		log.Print("scheduler: previous run still active, skipping tick")
		return false
	}
	defer r.mu.Unlock()
	if err := r.svc.RunOnce(ctx); err != nil {
		log.Printf("scheduler: run failed: %v", err)
	}
	return true
}

// Start runs once immediately (catch-up after downtime) and then on every
// tick until ctx ends. It returns a stop func that halts the loop; an
// in-flight run observes ctx cancellation and aborts promptly.
func (r *Runner) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		r.RunOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.RunOnce(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
