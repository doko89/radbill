package kriskrisapp

import (
	"context"
	"testing"
	"time"

	kriskristest "boilerplate/api/internal/kriskris/testkit"
)

// Internal test: shrinks pollInterval/pollJitter to observe StartPoller.

func TestStartPollerAppliesPaidAndExpires(t *testing.T) {
	oldInterval, oldJitter := pollInterval, pollJitter
	pollInterval, pollJitter = 20*time.Millisecond, 0
	t.Cleanup(func() { pollInterval, pollJitter = oldInterval, oldJitter })

	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	t.Setenv("KRISKRIS_BASE_URL", fake.Server.URL)
	t.Setenv("KRISKRIS_API_KEY", "test-key")
	t.Setenv("KRISKRIS_API_SECRET", "test-secret")
	t.Setenv("KRISKRIS_CALLBACK_SECRET", "cb-secret")

	paid := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "pol1", PPPoEStatus: "isolated"})
	old := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "pol2"})

	svc, err := NewServiceFromPool(pool)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	svc.SetSleep(func(time.Duration) {})
	ctx := context.Background()
	payRes, err := svc.Pay(ctx, paid.InvoiceID, paid.CustomerID)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	att, _ := svc.Store().GetAttempt(ctx, payRes.AttemptID)
	fake.SetStatus(att.KriskrisID, "paid", att.Amount)

	oldRes, err := svc.Pay(ctx, old.InvoiceID, old.CustomerID)
	if err != nil {
		t.Fatalf("pay old: %v", err)
	}
	// Force the second attempt into the past so the poller expires it.
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := pool.Exec(`UPDATE payment_attempts SET expires_at=$1 WHERE id=$2`, past, oldRes.AttemptID); err != nil {
		t.Fatal(err)
	}

	pctx, cancel := context.WithCancel(context.Background())
	stop := StartPoller(pctx, pool)
	defer stop()
	defer cancel()

	deadline := time.Now().Add(5 * time.Second)
	for {
		invPaid := kriskristest.InvoiceStatus(t, pool, paid.InvoiceID) == "paid"
		attPaid := kriskristest.AttemptStatus(t, pool, payRes.AttemptID) == "paid"
		attExp := kriskristest.AttemptStatus(t, pool, oldRes.AttemptID) == "expired"
		if invPaid && attPaid && attExp {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("poller did not converge: inv=%s att=%s old=%s",
				kriskristest.InvoiceStatus(t, pool, paid.InvoiceID),
				kriskristest.AttemptStatus(t, pool, payRes.AttemptID),
				kriskristest.AttemptStatus(t, pool, oldRes.AttemptID))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pays := kriskristest.ListPayments(t, pool, paid.InvoiceID); len(pays) != 2 {
		t.Fatalf("payments = %+v", pays)
	}
	// Stop is idempotent and waits for exit.
	stop()
	stop()
}

func TestStartPollerUnconfiguredSurvives(t *testing.T) {
	oldInterval := pollInterval
	pollInterval = 20 * time.Millisecond
	t.Cleanup(func() { pollInterval = oldInterval })
	oldRebuild := pollRebuildWait
	pollRebuildWait = 50 * time.Millisecond
	t.Cleanup(func() { pollRebuildWait = oldRebuild })

	pool := kriskristest.OpenDB(t) // no env, no settings: unconfigured
	pctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := StartPoller(pctx, pool)
	defer stop()
	time.Sleep(100 * time.Millisecond) // must not crash; just logs + retries
}

func TestTokenBucket(t *testing.T) {
	b := newTokenBucket(2, time.Second)
	if !b.Allow() || !b.Allow() {
		t.Fatal("first two should pass")
	}
	if b.Allow() {
		t.Fatal("third should be capped")
	}
	time.Sleep(1100 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("refill should allow one")
	}
}

func TestNewServiceFromPoolUsesEnv(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	t.Setenv("KRISKRIS_BASE_URL", "https://x.example")
	t.Setenv("KRISKRIS_API_KEY", "k")
	t.Setenv("KRISKRIS_API_SECRET", "s")
	t.Setenv("KRISKRIS_CALLBACK_SECRET", "c")
	svc, err := NewServiceFromPool(pool)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if svc.CallbackSecret() != "c" {
		t.Fatal("callback secret not wired")
	}
}
