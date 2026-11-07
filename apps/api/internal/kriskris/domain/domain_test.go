package kriskrisdomain

import (
	"fmt"
	"testing"
	"time"
)

// Fixed vectors below were computed independently with python3 hashlib/hmac
// (not with this package), so they pin the canonical format, not our code.

func TestCanonicalRequestVector(t *testing.T) {
	body := []byte(`{"amount":86500,"label":"INV-001"}`)
	got := CanonicalRequest("2026-11-07T20:00:00Z", "POST", "/api/v1/external/invoices", "", body)
	want := "2026-11-07T20:00:00Z\nPOST\n/api/v1/external/invoices\n\nf562fd76d25a8bf0d97c21ed7d01509017cce9654040446cea01d4305970ee57"
	if got != want {
		t.Fatalf("canonical mismatch:\n got %q\nwant %q", got, want)
	}
	if sig := SignRequest("test-secret-key", got); sig != "4995f929ac513ed9880effb355187bfa3cd2e9d3a196c5a3086493fb9f6eab60" {
		t.Fatalf("signature = %s, want 4995f929...", sig)
	}
}

func TestCanonicalRequestGETEmptyBody(t *testing.T) {
	// sha256("") = e3b0c44... (well-known empty hash).
	got := CanonicalRequest("2026-11-07T20:00:00Z", "get", "/api/v1/external/invoices/abc", "", nil)
	want := "2026-11-07T20:00:00Z\nGET\n/api/v1/external/invoices/abc\n\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("canonical mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestCanonicalRequestBatchQueryVector(t *testing.T) {
	// Batch GET signs the raw query as the 4th line (guide Bab 4).
	got := CanonicalRequest("2026-11-07T20:00:00Z", "GET",
		"/api/v1/external/invoices/status", "ids=kk-1,kk-2", nil)
	if sig := SignRequest("test-secret-key", got); sig != "c05526b1fdfa44c3045534f5ed085418076eed3198e88ec389e31bc27eaa9aec" {
		t.Fatalf("batch signature = %s", sig)
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	body := []byte(`{"delivery_id":"dlv-1","event":"invoice.paid"}`)
	good := "sha256=94faa3fd5e0038df03818f76399b23d6b23ea7fb8d01809f1ab907bb16aa0e6b" // python3 vector
	if !VerifyWebhookSignature("cb-secret", body, good) {
		t.Fatal("valid signature rejected")
	}
	bad := []string{
		"",
		"94faa3fd5e0038df03818f76399b23d6b23ea7fb8d01809f1ab907bb16aa0e6b",        // missing prefix
		"sha256=94faa3fd5e0038df03818f76399b23d6b23ea7fb8d01809f1ab907bb16aa0e00", // bit flip
		"sha256=zzzz",
		"sha256=94fa", // short
	}
	for _, h := range bad {
		if VerifyWebhookSignature("cb-secret", body, h) {
			t.Fatalf("invalid header %q accepted", h)
		}
	}
	if VerifyWebhookSignature("wrong-secret", body, good) {
		t.Fatal("wrong secret accepted")
	}
	if VerifyWebhookSignature("", body, good) {
		t.Fatal("empty secret accepted")
	}
	if VerifyWebhookSignature("cb-secret", []byte(`{"delivery_id":"dlv-2"}`), good) {
		t.Fatal("tampered body accepted")
	}
}

func TestTimestampFresh(t *testing.T) {
	now := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	for _, d := range []time.Duration{-5 * time.Minute, -time.Minute, 0, time.Minute, 5 * time.Minute} {
		if !TimestampFresh(now.Add(d), now) {
			t.Fatalf("offset %v should be fresh", d)
		}
	}
	for _, d := range []time.Duration{-6 * time.Minute, 6 * time.Minute, time.Hour} {
		if TimestampFresh(now.Add(d), now) {
			t.Fatalf("offset %v should be stale", d)
		}
	}
}

func TestAmountMath(t *testing.T) {
	if TotalWithUniqueCode(86500, 50) != 86550 {
		t.Fatal("total math wrong")
	}
	for _, c := range []int64{10, 50, 99} {
		if !ValidUniqueCode(c) {
			t.Fatalf("code %d should be valid", c)
		}
	}
	for _, c := range []int64{0, 9, 100, -1} {
		if ValidUniqueCode(c) {
			t.Fatalf("code %d should be invalid", c)
		}
	}
}

func TestExtensionHelpers(t *testing.T) {
	now := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	if got := ExtensionBase(&future, now); !got.Equal(future) {
		t.Fatalf("future base = %v", got)
	}
	past := now.Add(-time.Hour)
	if got := ExtensionBase(&past, now); !got.Equal(now) {
		t.Fatalf("past base = %v, want now", got)
	}
	if got := ExtensionBase(nil, now); !got.Equal(now) {
		t.Fatalf("nil base = %v, want now", got)
	}
	v := int64(3600)
	if ExtensionPeriod(&v) != time.Hour {
		t.Fatal("validity period wrong")
	}
	zero := int64(0)
	if ExtensionPeriod(&zero) != DefaultExtension || ExtensionPeriod(nil) != DefaultExtension {
		t.Fatal("fallback period wrong")
	}
}

func TestBizError(t *testing.T) {
	err := fmt.Errorf("wrap: %w", NewBizError(CodeGobizOffline, "outlet down"))
	if !IsBizCode(err, CodeGobizOffline) {
		t.Fatal("BizCode did not survive wrapping")
	}
	if BizCode(fmt.Errorf("plain")) != "" {
		t.Fatal("plain error should have no code")
	}
	if NewBizError("", "") != nil {
		t.Fatal("empty code should be nil")
	}
}
