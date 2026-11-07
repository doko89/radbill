package kriskrisinfra_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
	kriskristest "boilerplate/api/internal/kriskris/testkit"
)

func newClient(t *testing.T, fake *kriskristest.FakeKriskris) *kriskrisinfra.Client {
	t.Helper()
	c := kriskrisinfra.NewClient(fake.Server.URL, "test-key", "test-secret")
	c.Now = func() time.Time { return time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC) }
	return c
}

func TestCreateInvoiceSignsAndDecodes(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := newClient(t, fake)
	inv, err := c.CreateInvoice(context.Background(), 86550, "INV-001", 15)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if inv.ID == "" || inv.QRPayload == "" || inv.Amount != 86550 || inv.Status != "pending" || inv.ExpiresAt == "" {
		t.Fatalf("invoice: %+v", inv)
	}
	if !fake.AllSignaturesOK() {
		t.Fatal("fake rejected client signature")
	}
	// Exact body fields we send (pinned: amount, label, expiry_minutes).
	var body map[string]any
	if err := json.Unmarshal(fake.LastCreateBody(), &body); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if body["amount"] != float64(86550) || body["label"] != "INV-001" || body["expiry_minutes"] != float64(15) {
		t.Fatalf("create body = %v", body)
	}
	if len(body) != 3 {
		t.Fatalf("create body has extra fields: %v", body)
	}
}

func TestCreateCancelGetRoundTrip(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := newClient(t, fake)
	ctx := context.Background()
	inv, err := c.CreateInvoice(ctx, 10050, "INV-RT", 15)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := c.GetInvoice(ctx, inv.ID)
	if err != nil || got.Amount != 10050 {
		t.Fatalf("get: %+v %v", got, err)
	}
	if err := c.CancelInvoice(ctx, inv.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if st, _, _ := fake.RemoteStatus(inv.ID); st != "cancelled" {
		t.Fatalf("remote status = %q", st)
	}
	// Unknown remote id: cancel is success (idempotent), get is not-found.
	if err := c.CancelInvoice(ctx, "kk-nope"); err != nil {
		t.Fatalf("cancel unknown: %v", err)
	}
	if _, err := c.GetInvoice(ctx, "kk-nope"); !errors.Is(err, kriskrisinfra.ErrRemoteNotFound) {
		t.Fatalf("get unknown = %v", err)
	}
}

func TestBatchStatus(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := newClient(t, fake)
	ctx := context.Background()
	a, _ := c.CreateInvoice(ctx, 10010, "INV-A", 15)
	b, _ := c.CreateInvoice(ctx, 10020, "INV-B", 15)
	fake.SetStatus(a.ID, "paid", 10010)
	rows, err := c.BatchStatus(ctx, []string{a.ID, b.ID, "kk-nope"})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("batch rows = %+v", rows)
	}
	byID := map[string]kriskrisinfra.BatchItem{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if byID[a.ID].Status != "paid" || byID[b.ID].Status != "pending" {
		t.Fatalf("batch = %+v", rows)
	}
	if got, err := c.BatchStatus(ctx, nil); len(got) != 0 || err != nil {
		t.Fatalf("empty batch = %+v %v", got, err)
	}
	big := make([]string, kriskrisdomain.MaxBatchIDs+1)
	for i := range big {
		big[i] = "kk-x"
	}
	if _, err := c.BatchStatus(ctx, big); err == nil {
		t.Fatal("oversize batch should error")
	}
}

func TestBusinessErrorMapping(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := newClient(t, fake)
	ctx := context.Background()
	cases := []struct {
		status int
		code   string
	}{
		{http.StatusConflict, kriskrisdomain.CodeDuplicatePending},
		{http.StatusServiceUnavailable, kriskrisdomain.CodeGobizOffline},
		{http.StatusPaymentRequired, kriskrisdomain.CodeQuotaExceeded},
		{http.StatusBadRequest, kriskrisdomain.CodeInvalidAmount},
	}
	for _, tc := range cases {
		fake.OnCreate = func(amount int64, label string) (int, any) {
			return tc.status, map[string]any{"error": map[string]any{"code": tc.code, "message": "biz " + tc.code}}
		}
		_, err := c.CreateInvoice(ctx, 10010, "INV-BIZ", 15)
		if !kriskrisdomain.IsBizCode(err, tc.code) {
			t.Fatalf("code %s: err = %v", tc.code, err)
		}
	}
	// Flat (non-envelope) error shape is also accepted.
	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusServiceUnavailable, map[string]any{"code": "gobiz_offline", "message": "flat"}
	}
	if _, err := c.CreateInvoice(ctx, 10010, "INV-BIZ", 15); !kriskrisdomain.IsBizCode(err, "gobiz_offline") {
		t.Fatalf("flat error: %v", err)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := kriskrisinfra.NewClient(fake.Server.URL, "test-key", "wrong-secret")
	if _, err := c.CreateInvoice(context.Background(), 10010, "INV-S", 15); err == nil {
		t.Fatal("wrong secret should fail")
	}
}
