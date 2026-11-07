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
	inv, err := c.CreateInvoice(context.Background(), 86500, "INV-001")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Server picks the unique code: base echoed, code in range, total consistent.
	if inv.ID == "" || inv.QRPayload == "" || inv.BaseAmount != 86500 || inv.Status != "pending" || inv.ExpiresAt == "" {
		t.Fatalf("invoice: %+v", inv)
	}
	if !kriskrisdomain.ValidUniqueCode(inv.UniqueCode) || inv.Amount != inv.BaseAmount+inv.UniqueCode {
		t.Fatalf("server totals inconsistent: %+v", inv)
	}
	if !fake.AllSignaturesOK() {
		t.Fatal("fake rejected client signature")
	}
	// Exact body fields we send (pinned: amount=base, label — nothing else).
	var body map[string]any
	if err := json.Unmarshal(fake.LastCreateBody(), &body); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if body["amount"] != float64(86500) || body["label"] != "INV-001" {
		t.Fatalf("create body = %v", body)
	}
	if len(body) != 2 {
		t.Fatalf("create body has extra fields: %v", body)
	}
}

func TestCreateCancelGetRoundTrip(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := newClient(t, fake)
	ctx := context.Background()
	inv, err := c.CreateInvoice(ctx, 10000, "INV-RT")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := c.GetInvoice(ctx, inv.ID)
	if err != nil || got.Amount != inv.Amount || got.BaseAmount != 10000 || got.UniqueCode != inv.UniqueCode {
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
	a, _ := c.CreateInvoice(ctx, 10000, "INV-A")
	b, _ := c.CreateInvoice(ctx, 10000, "INV-B")
	fake.SetStatus(a.ID, "paid", a.Amount)
	rows, err := c.BatchStatus(ctx, []string{a.ID, b.ID, "kk-nope"})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("batch rows = %+v", rows)
	}
	byID := map[string]kriskrisinfra.BatchItem{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if !byID[a.ID].Found || byID[a.ID].Status != "paid" || !byID[b.ID].Found || byID[b.ID].Status != "pending" {
		t.Fatalf("batch = %+v", rows)
	}
	if byID["kk-nope"].Found {
		t.Fatalf("unknown id should be found=false: %+v", rows)
	}
	// Batch goes over GET with a signed query (guide Bab 4).
	var sawGET bool
	for _, r := range fake.Requests {
		if r.Method == http.MethodGet && r.Path == "/api/v1/external/invoices/status" && r.SignatureOK {
			sawGET = true
		}
	}
	if !sawGET {
		t.Fatal("no signed GET status request observed")
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
		{http.StatusUnprocessableEntity, kriskrisdomain.CodeDuplicatePending},
		{http.StatusUnprocessableEntity, kriskrisdomain.CodeGobizOffline},
		{http.StatusUnprocessableEntity, kriskrisdomain.CodeQuotaExceeded},
		{http.StatusUnprocessableEntity, kriskrisdomain.CodeInvalidAmount},
	}
	for _, tc := range cases {
		fake.OnCreate = func(amount int64, label string) (int, any) {
			return tc.status, map[string]any{
				"ok": false, "error": "biz " + tc.code,
				"data": map[string]any{"code": tc.code},
			}
		}
		_, err := c.CreateInvoice(ctx, 10000, "INV-BIZ")
		if !kriskrisdomain.IsBizCode(err, tc.code) {
			t.Fatalf("code %s: err = %v", tc.code, err)
		}
	}
	// Legacy shapes are tolerated.
	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "gobiz_offline"}}
	}
	if _, err := c.CreateInvoice(ctx, 10000, "INV-BIZ"); !kriskrisdomain.IsBizCode(err, "gobiz_offline") {
		t.Fatalf("legacy error: %v", err)
	}
	// 429 maps to rate_limited.
	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusTooManyRequests, map[string]any{
			"ok": false, "error": "slow down", "data": map[string]any{"retry_after": 30},
		}
	}
	if _, err := c.CreateInvoice(ctx, 10000, "INV-BIZ"); !kriskrisdomain.IsBizCode(err, kriskrisdomain.CodeRateLimited) {
		t.Fatalf("429: %v", err)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	c := kriskrisinfra.NewClient(fake.Server.URL, "test-key", "wrong-secret")
	if _, err := c.CreateInvoice(context.Background(), 10000, "INV-S"); err == nil {
		t.Fatal("wrong secret should fail")
	}
}
