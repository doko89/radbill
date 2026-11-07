package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"boilerplate/api/internal/billing/domain"
)

type hookRecorder struct {
	events []PaidEvent
	err    error
}

func (h *hookRecorder) NotifyPaid(_ context.Context, e PaidEvent) error {
	h.events = append(h.events, e)
	return h.err
}

func hookTestRef() *domain.PPPoERef {
	return &domain.PPPoERef{ID: "ppp1", CustomerID: "cust1", PackageID: "pkg1", Status: "active"}
}

func TestManualPaymentFiresPaidHook(t *testing.T) {
	repo := &fakeRepo{inv: unpaidInvoice()}
	svc := NewService(repo, &fakePPPoE{ref: hookTestRef()}, &fakePkg{})
	hook := &hookRecorder{}
	svc.SetPaidNotifier(hook)
	svc.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }

	dto, err := svc.ManualPayment(context.Background(), "inv1", 50000)
	if err != nil {
		t.Fatal(err)
	}
	if len(hook.events) != 1 {
		t.Fatalf("hook events = %d, want 1", len(hook.events))
	}
	e := hook.events[0]
	if e.InvoiceID != dto.ID || e.Number != "INV-1" || e.Total != 50000 ||
		e.CustomerID != "cust1" || e.PPPoEAccountID != "ppp1" {
		t.Fatalf("hook event = %+v", e)
	}
}

func TestManualPaymentHookErrorDoesNotFail(t *testing.T) {
	repo := &fakeRepo{inv: unpaidInvoice()}
	svc := NewService(repo, &fakePPPoE{ref: hookTestRef()}, &fakePkg{})
	svc.SetPaidNotifier(&hookRecorder{err: errors.New("boom")})
	if _, err := svc.ManualPayment(context.Background(), "inv1", 0); err != nil {
		t.Fatalf("hook error failed payment: %v", err)
	}
}

// TestFanoutNotifiersRunsEveryBranch is the G3 additive assertion: the
// scheduler notification and the top-up hook both fire; a failing branch
// neither blocks the next nor fails the payment.
func TestFanoutNotifiersRunsEveryBranch(t *testing.T) {
	good := &hookRecorder{}
	bad := &hookRecorder{err: errors.New("boom")}
	fanout := FanoutNotifiers(good, bad, nil)
	if err := fanout.NotifyPaid(context.Background(), PaidEvent{InvoiceID: "inv1"}); err == nil {
		t.Fatal("expected first error")
	}
	if len(good.events) != 1 || len(bad.events) != 1 {
		t.Fatalf("events = %d/%d", len(good.events), len(bad.events))
	}
	if err := FanoutNotifiers().NotifyPaid(context.Background(), PaidEvent{}); err != nil {
		t.Fatalf("empty fanout = %v", err)
	}
}
