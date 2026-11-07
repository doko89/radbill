package kriskrisinfra_test

import (
	"context"
	"errors"
	"testing"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
	kriskristest "boilerplate/api/internal/kriskris/testkit"
)

func insertAttempt(t *testing.T, store *kriskrisinfra.Store, seed kriskristest.SeedResult, tag, kid string, base, code int64) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	att := &kriskrisinfra.Attempt{
		ID: "att-" + tag, InvoiceID: seed.InvoiceID, KriskrisID: kid,
		BaseAmount: base, UniqueCode: code, Amount: base + code,
		QRPayload: "QR:" + kid, Status: kriskrisdomain.AttemptPending,
		ExpiresAt: now.Add(15 * time.Minute), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertAttempt(context.Background(), att); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	return att.ID
}

func TestApplyPaidTxHappyPath(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{
		Tag: "apply1", Total: 50000, PPPoEStatus: "isolated",
		ValiditySecs: kriskristest.Int64Ptr(7 * 24 * 3600),
	})
	store := kriskrisinfra.NewStore(pool)
	ctx := context.Background()
	aid := insertAttempt(t, store, seed, "apply1", "kk-apply1", 50000, 42)
	now := time.Now().UTC().Truncate(time.Second)

	applied, err := store.ApplyPaidTx(ctx, aid, 50042, now)
	if err != nil || !applied {
		t.Fatalf("apply = %v, %v", applied, err)
	}
	if st := kriskristest.InvoiceStatus(t, pool, seed.InvoiceID); st != "paid" {
		t.Fatalf("invoice = %q", st)
	}
	pays := kriskristest.ListPayments(t, pool, seed.InvoiceID)
	if len(pays) != 2 {
		t.Fatalf("payments = %+v", pays)
	}
	if pays[0].Amount != 50000 || pays[0].Note != "" || pays[0].Method != "kriskris" || pays[0].Attempt != aid {
		t.Fatalf("base row = %+v", pays[0])
	}
	if pays[1].Amount != 42 || pays[1].Note != "unique_code" || pays[1].Method != "kriskris" || pays[1].Attempt != aid {
		t.Fatalf("code row = %+v", pays[1])
	}
	status, expires := kriskristest.PPPoEState(t, pool, seed.PPPoEID)
	if status != "active" {
		t.Fatalf("pppoe status = %q", status)
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		t.Fatalf("expires: %v", err)
	}
	if want := now.Add(7 * 24 * time.Hour); exp.Sub(want).Abs() > 5*time.Second {
		t.Fatalf("expires = %v, want ~%v", exp, want)
	}
	if st := kriskristest.AttemptStatus(t, pool, aid); st != "paid" {
		t.Fatalf("attempt = %q", st)
	}
	// pg_notify on sqlite is ignored (no error above proves it).
}

func TestApplyPaidTxIdempotent(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "idem1"})
	store := kriskrisinfra.NewStore(pool)
	ctx := context.Background()
	aid := insertAttempt(t, store, seed, "idem1", "kk-idem1", 50000, 11)
	now := time.Now().UTC()
	if _, err := store.ApplyPaidTx(ctx, aid, 50011, now); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	applied, err := store.ApplyPaidTx(ctx, aid, 50011, now)
	if err != nil || applied {
		t.Fatalf("replay = %v, %v", applied, err)
	}
	if pays := kriskristest.ListPayments(t, pool, seed.InvoiceID); len(pays) != 2 {
		t.Fatalf("payments after replay = %+v", pays)
	}
}

func TestApplyPaidTxLatePaymentOnCancelled(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "late1", ValiditySecs: kriskristest.Int64Ptr(3600)})
	store := kriskrisinfra.NewStore(pool)
	ctx := context.Background()
	aid := insertAttempt(t, store, seed, "late1", "kk-late1", 50000, 77)
	now := time.Now().UTC()
	if err := store.SetAttemptStatus(ctx, aid, kriskrisdomain.AttemptCancelled, now); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyPaidTx(ctx, aid, 50077, now)
	if err != nil || !applied {
		t.Fatalf("late apply = %v, %v", applied, err)
	}
	if st := kriskristest.InvoiceStatus(t, pool, seed.InvoiceID); st != "paid" {
		t.Fatalf("invoice = %q", st)
	}
}

func TestApplyPaidTxGuards(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	ctx := context.Background()
	store := kriskrisinfra.NewStore(pool)
	now := time.Now().UTC()

	// Amount mismatch: nothing changes.
	s1 := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "guard1"})
	a1 := insertAttempt(t, store, s1, "guard1", "kk-guard1", 50000, 10)
	if _, err := store.ApplyPaidTx(ctx, a1, 99999, now); !errors.Is(err, kriskrisinfra.ErrAmountMismatch) {
		t.Fatalf("mismatch = %v", err)
	}
	if st := kriskristest.InvoiceStatus(t, pool, s1.InvoiceID); st != "unpaid" {
		t.Fatalf("invoice = %q", st)
	}
	if st := kriskristest.AttemptStatus(t, pool, a1); st != "pending" {
		t.Fatalf("attempt = %q", st)
	}

	// Void invoice: attempt parked as failed, manual reconciliation.
	s2 := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "guard2", Status: "void"})
	a2 := insertAttempt(t, store, s2, "guard2", "kk-guard2", 50000, 10)
	if _, err := store.ApplyPaidTx(ctx, a2, 50010, now); !errors.Is(err, kriskrisinfra.ErrInvoiceVoid) {
		t.Fatalf("void = %v", err)
	}
	if st := kriskristest.AttemptStatus(t, pool, a2); st != "failed" {
		t.Fatalf("attempt = %q", st)
	}

	// Already-paid invoice: attempt closed as paid, no double payments.
	s3 := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "guard3", Status: "paid"})
	a3 := insertAttempt(t, store, s3, "guard3", "kk-guard3", 50000, 10)
	applied, err := store.ApplyPaidTx(ctx, a3, 50010, now)
	if err != nil || applied {
		t.Fatalf("paid invoice = %v, %v", applied, err)
	}
	if pays := kriskristest.ListPayments(t, pool, s3.InvoiceID); len(pays) != 0 {
		t.Fatalf("payments = %+v", pays)
	}
	if st := kriskristest.AttemptStatus(t, pool, a3); st != "paid" {
		t.Fatalf("attempt = %q", st)
	}
}

func TestApplyPaidTxExtendsFromFutureExpiry(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	future := time.Now().UTC().Add(10 * 24 * time.Hour).Truncate(time.Second)
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{
		Tag: "ext1", ExpiresAt: future.Format(time.RFC3339), // no validity -> 30d fallback
	})
	store := kriskrisinfra.NewStore(pool)
	ctx := context.Background()
	aid := insertAttempt(t, store, seed, "ext1", "kk-ext1", 50000, 10)
	now := time.Now().UTC()
	if _, err := store.ApplyPaidTx(ctx, aid, 50010, now); err != nil {
		t.Fatalf("apply: %v", err)
	}
	_, expires := kriskristest.PPPoEState(t, pool, seed.PPPoEID)
	exp, _ := time.Parse(time.RFC3339, expires)
	if want := future.Add(30 * 24 * time.Hour); exp.Sub(want).Abs() > 5*time.Second {
		t.Fatalf("expires = %v, want ~%v", exp, want)
	}
}

func TestClaimDeliveryIdempotent(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	store := kriskrisinfra.NewStore(pool)
	ctx := context.Background()
	now := time.Now().UTC()
	ok, err := store.ClaimDelivery(ctx, "dlv-1", "invoice.paid", "{}", now)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	ok, err = store.ClaimDelivery(ctx, "dlv-1", "invoice.paid", "{}", now)
	if err != nil || ok {
		t.Fatalf("dup claim = %v, %v", ok, err)
	}
}

func TestPendingQueriesAndExpiry(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	s1 := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "pend1"})
	s2 := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "pend2"})
	store := kriskrisinfra.NewStore(pool)
	ctx := context.Background()
	a1 := insertAttempt(t, store, s1, "pend1", "kk-pend1", 50000, 10)
	a2 := insertAttempt(t, store, s2, "pend2", "kk-pend2", 50000, 20)

	pend, err := store.ListPendingAttempts(ctx, s1.InvoiceID)
	if err != nil || len(pend) != 1 || pend[0].ID != a1 {
		t.Fatalf("pending inv1 = %+v %v", pend, err)
	}
	batch, err := store.ListPendingBatch(ctx, 50)
	if err != nil || len(batch) != 2 {
		t.Fatalf("batch = %d %v", len(batch), err)
	}
	latest, err := store.GetLatestAttempt(ctx, s1.InvoiceID)
	if err != nil || latest == nil || latest.KriskrisID != "kk-pend1" {
		t.Fatalf("latest = %+v %v", latest, err)
	}
	none, err := store.GetLatestAttempt(ctx, "inv-nope")
	if err != nil || none != nil {
		t.Fatalf("latest missing = %+v %v", none, err)
	}
	if _, err := store.GetAttempt(ctx, "att-nope"); !errors.Is(err, kriskrisinfra.ErrNotFound) {
		t.Fatalf("get missing = %v", err)
	}

	if err := store.MarkExpired(ctx, []string{a1, a2}, time.Now().UTC()); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if st := kriskristest.AttemptStatus(t, pool, a1); st != "expired" {
		t.Fatalf("a1 = %q", st)
	}
	batch, _ = store.ListPendingBatch(ctx, 50)
	if len(batch) != 0 {
		t.Fatalf("batch after expire = %d", len(batch))
	}
}
