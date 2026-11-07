package kriskrisapp_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	kriskrisapp "boilerplate/api/internal/kriskris/application"
	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
	kriskristest "boilerplate/api/internal/kriskris/testkit"
)

func svcWithFake(t *testing.T, fake *kriskristest.FakeKriskris, pool *sql.DB) *kriskrisapp.Service {
	t.Helper()
	client := kriskrisinfra.NewClient(fake.Server.URL, "test-key", "test-secret")
	svc := kriskrisapp.NewService(kriskrisinfra.NewStore(pool), client,
		kriskrisinfra.Config{BaseURL: fake.Server.URL, APIKey: "test-key", APISecret: "test-secret", CallbackSecret: "cb-secret"})
	svc.SetSleep(func(time.Duration) {}) // no real backoff in tests
	return svc
}

func TestPayFlowCancelsStaleAndCreates(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "pay1", Total: 86500})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()

	first, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatalf("pay1: %v", err)
	}
	if first.QRPayload == "" || first.AttemptID == "" || first.ExpiresAt == "" {
		t.Fatalf("result = %+v", first)
	}
	if !kriskrisdomain.ValidUniqueCode(first.Code) {
		t.Fatalf("code = %d", first.Code)
	}
	if first.Amount != 86500+first.Code || first.Base != 86500 {
		t.Fatalf("amounts = %+v", first)
	}
	if !strings.Contains(first.Message, "kode unik") || !strings.Contains(first.Message, "Rp") {
		t.Fatalf("message = %q", first.Message)
	}
	if exp, err := time.Parse(time.RFC3339, first.ExpiresAt); err != nil || time.Until(exp) > 16*time.Minute {
		t.Fatalf("expires = %q %v", first.ExpiresAt, err)
	}

	second, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatalf("pay2: %v", err)
	}
	if second.AttemptID == first.AttemptID {
		t.Fatal("second pay must create a new attempt")
	}
	// Stale attempt cancelled locally AND remotely.
	if st := kriskristest.AttemptStatus(t, pool, first.AttemptID); st != "cancelled" {
		t.Fatalf("stale = %q", st)
	}
	if st := kriskristest.AttemptStatus(t, pool, second.AttemptID); st != "pending" {
		t.Fatalf("current = %q", st)
	}
	pend, err := svc.Store().ListPendingAttempts(ctx, seed.InvoiceID)
	if err != nil || len(pend) != 1 {
		t.Fatalf("pending count = %d", len(pend))
	}
}

func TestPayOwnershipAndState(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	mine := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "own1"})
	other := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "own2"})
	paid := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "own3", Status: "paid"})
	voided := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "own4", Status: "void"})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()

	if _, err := svc.Pay(ctx, "inv-nope", mine.CustomerID); !errors.Is(err, kriskrisapp.ErrInvoiceNotFound) {
		t.Fatalf("missing = %v", err)
	}
	if _, err := svc.Pay(ctx, other.InvoiceID, mine.CustomerID); !errors.Is(err, kriskrisapp.ErrInvoiceNotFound) {
		t.Fatalf("other's invoice = %v (must be 404, not 403)", err)
	}
	// Admin ("") bypasses ownership.
	if _, err := svc.Pay(ctx, other.InvoiceID, ""); err != nil {
		t.Fatalf("admin pay: %v", err)
	}
	var npe *kriskrisapp.NotPayableError
	if _, err := svc.Pay(ctx, paid.InvoiceID, paid.CustomerID); !errors.As(err, &npe) || npe.Status != "paid" {
		t.Fatalf("paid = %v", err)
	}
	if _, err := svc.Pay(ctx, voided.InvoiceID, voided.CustomerID); !errors.As(err, &npe) || npe.Status != "void" {
		t.Fatalf("void = %v", err)
	}
}

func TestWebhookPayloadNotTrusted(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "wh1", PPPoEStatus: "isolated"})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()

	res, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	att, err := svc.Store().GetAttempt(ctx, res.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	// Callback claims paid but GET still says pending: must NOT apply.
	dup, err := svc.ProcessWebhookSync(ctx, "dlv-1", "invoice.paid", att.KriskrisID, `{"event":"invoice.paid"}`)
	if dup || !errors.Is(err, kriskrisapp.ErrNotPaidYet) {
		t.Fatalf("untrusted = dup:%v err:%v", dup, err)
	}
	if st := kriskristest.InvoiceStatus(t, pool, seed.InvoiceID); st != "unpaid" {
		t.Fatalf("invoice = %q (payload must not settle)", st)
	}
	// Now the remote really is paid: a new delivery applies.
	fake.SetStatus(att.KriskrisID, "paid", att.Amount)
	dup, err = svc.ProcessWebhookSync(ctx, "dlv-2", "invoice.paid", att.KriskrisID, `{"event":"invoice.paid"}`)
	if dup || err != nil {
		t.Fatalf("paid = dup:%v err:%v", dup, err)
	}
	if st := kriskristest.InvoiceStatus(t, pool, seed.InvoiceID); st != "paid" {
		t.Fatalf("invoice = %q", st)
	}
	if pays := kriskristest.ListPayments(t, pool, seed.InvoiceID); len(pays) != 2 {
		t.Fatalf("payments = %+v", pays)
	}
	// Replay of the same delivery is a duplicate; re-confirm is a no-op.
	dup, err = svc.ProcessWebhookSync(ctx, "dlv-2", "invoice.paid", att.KriskrisID, `{}`)
	if !dup || err != nil {
		t.Fatalf("replay = dup:%v err:%v", dup, err)
	}
	if err := svc.ConfirmPaid(ctx, att.KriskrisID); err != nil {
		t.Fatalf("re-confirm: %v", err)
	}
	if pays := kriskristest.ListPayments(t, pool, seed.InvoiceID); len(pays) != 2 {
		t.Fatalf("payments after replay = %+v", pays)
	}
}

func TestWebhookAmountMismatchNotApplied(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "wh2"})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()
	res, _ := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID)
	att, _ := svc.Store().GetAttempt(ctx, res.AttemptID)
	fake.SetStatus(att.KriskrisID, "paid", att.Amount+1000) // inconsistent figure
	_, err := svc.ProcessWebhookSync(ctx, "dlv-x", "invoice.paid", att.KriskrisID, `{}`)
	if !errors.Is(err, kriskrisinfra.ErrAmountMismatch) {
		t.Fatalf("mismatch = %v", err)
	}
	if st := kriskristest.InvoiceStatus(t, pool, seed.InvoiceID); st != "unpaid" {
		t.Fatalf("invoice = %q", st)
	}
}

func TestPayBusinessErrors(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "biz1"})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()

	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "gobiz_offline"}}
	}
	if _, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID); !kriskrisdomain.IsBizCode(err, "gobiz_offline") {
		t.Fatalf("gobiz = %v", err)
	}
	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusPaymentRequired, map[string]any{"error": map[string]any{"code": "quota_exceeded"}}
	}
	if _, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID); !kriskrisdomain.IsBizCode(err, "quota_exceeded") {
		t.Fatalf("quota = %v", err)
	}
	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalid_amount"}}
	}
	if _, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID); !kriskrisdomain.IsBizCode(err, "invalid_amount") {
		t.Fatalf("amount = %v", err)
	}
}

func TestDuplicatePendingRetries(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "dup1"})
	svc := svcWithFake(t, fake, pool)
	var sleeps int
	svc.SetSleep(func(time.Duration) { sleeps++ })
	calls := 0
	// First create attempt fails duplicate_pending, then succeeds.
	fake.OnCreate = func(amount int64, label string) (int, any) {
		calls++
		if calls == 1 {
			return http.StatusConflict, map[string]any{"error": map[string]any{"code": "duplicate_pending"}}
		}
		// Delegate to the default path by creating directly through a second
		// fake-free route: emulate success with a synthetic invoice.
		return http.StatusCreated, map[string]any{"data": map[string]any{
			"id": "kk-retry-1", "qr_payload": "QR:kk-retry-1", "amount": amount,
			"status": "pending", "expires_at": time.Now().UTC().Add(15 * time.Minute).Format("2006-01-02T15:04:05Z"),
		}}
	}
	res, err := svc.Pay(context.Background(), seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatalf("pay after retry: %v", err)
	}
	if res.AttemptID == "" || sleeps != 1 {
		t.Fatalf("res=%+v sleeps=%d", res, sleeps)
	}
}

func TestStatus(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "st1", Total: 75000})
	other := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "st2"})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()

	res, err := svc.Status(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.InvoiceStatus != "unpaid" || res.Total != 75000 || res.Attempt != nil {
		t.Fatalf("fresh = %+v", res)
	}
	pay, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatal(err)
	}
	res, err = svc.Status(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil || res.Attempt == nil || res.Attempt.ID != pay.AttemptID || res.Attempt.Status != "pending" {
		t.Fatalf("after pay = %+v %v", res, err)
	}
	if _, err := svc.Status(ctx, other.InvoiceID, seed.CustomerID); !errors.Is(err, kriskrisapp.ErrInvoiceNotFound) {
		t.Fatalf("other's = %v", err)
	}
	if _, err := svc.Status(ctx, "inv-nope", ""); !errors.Is(err, kriskrisapp.ErrInvoiceNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestConfirmUnknownAttempt(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	svc := svcWithFake(t, fake, pool)
	if err := svc.ConfirmPaid(context.Background(), "kk-ghost"); !errors.Is(err, kriskrisapp.ErrUnknownAttempt) {
		t.Fatalf("ghost = %v", err)
	}
	dup, err := svc.ProcessWebhookSync(context.Background(), "dlv-g", "invoice.paid", "", `{}`)
	if dup || err != nil {
		t.Fatalf("empty kid = dup:%v err:%v", dup, err)
	}
	dup, err = svc.ProcessWebhookSync(context.Background(), "dlv-g2", "something.else", "kk-ghost", `{}`)
	if dup || err != nil {
		t.Fatalf("other event = dup:%v err:%v", dup, err)
	}
}

// TestConfirmPaidFiresPaidHook is the G3 additive assertion: a confirmed
// payment fires the apply-on-paid hook with the settled invoice id, while
// failures and re-confirms stay silent.
func TestConfirmPaidFiresPaidHook(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "hook1"})
	svc := svcWithFake(t, fake, pool)
	ctx := context.Background()

	var fired []string
	svc.SetPaidHook(func(_ context.Context, invoiceID string) {
		fired = append(fired, invoiceID)
	})
	res, err := svc.Pay(ctx, seed.InvoiceID, seed.CustomerID)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	att, err := svc.Store().GetAttempt(ctx, res.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	// Unpaid remote: no hook.
	if err := svc.ConfirmPaid(ctx, att.KriskrisID); !errors.Is(err, kriskrisapp.ErrNotPaidYet) {
		t.Fatalf("unpaid = %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("fired early: %v", fired)
	}
	// Paid remote: exactly one hook with the invoice id.
	fake.SetStatus(att.KriskrisID, "paid", att.Amount)
	if err := svc.ConfirmPaid(ctx, att.KriskrisID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(fired) != 1 || fired[0] != seed.InvoiceID {
		t.Fatalf("fired = %v", fired)
	}
	// Idempotent re-confirm: already-paid attempt returns before the hook.
	if err := svc.ConfirmPaid(ctx, att.KriskrisID); err != nil {
		t.Fatalf("re-confirm: %v", err)
	}
	if len(fired) != 1 {
		t.Fatalf("fired twice: %v", fired)
	}
}
