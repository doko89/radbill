package domain_test

import (
	"testing"
	"time"

	"boilerplate/api/internal/radius"
	topupdomain "boilerplate/api/internal/topup/domain"
)

func TestIsPreset(t *testing.T) {
	for _, b := range []int64{topupdomain.Preset1GB, topupdomain.Preset5GB, topupdomain.Preset10GB} {
		if !topupdomain.IsPreset(b) {
			t.Fatalf("IsPreset(%d) = false", b)
		}
	}
	for _, b := range []int64{0, -1, 1, topupdomain.GB - 1, topupdomain.GB + 1, 2 * topupdomain.GB, 3 * topupdomain.GB, 20 * topupdomain.GB} {
		if topupdomain.IsPreset(b) {
			t.Fatalf("IsPreset(%d) = true", b)
		}
	}
}

func TestPriceForBytes(t *testing.T) {
	gb := int64(topupdomain.GB)
	tests := []struct {
		bytes, price, want int64
	}{
		{gb, 10000, 10000},
		{5 * gb, 10000, 50000},
		{10 * gb, 10000, 100000},
		{gb + 1, 10000, 20000}, // ceil
		{gb, 7500, 7500},
		{0, 10000, 0},
		{gb, 0, 0},
		{gb, -5, 0},
	}
	for _, tt := range tests {
		if got := topupdomain.PriceForBytes(tt.bytes, tt.price); got != tt.want {
			t.Fatalf("PriceForBytes(%d,%d) = %d, want %d", tt.bytes, tt.price, got, tt.want)
		}
	}
}

func TestItemDescriptionRoundTrip(t *testing.T) {
	for _, b := range []int64{topupdomain.Preset1GB, topupdomain.Preset5GB, topupdomain.Preset10GB} {
		desc := topupdomain.ItemDescription(b)
		got, ok := topupdomain.ParseItemDescription(desc)
		if !ok || got != b {
			t.Fatalf("round-trip %q = %d,%v", desc, got, ok)
		}
	}
	if got := topupdomain.ItemDescription(topupdomain.Preset5GB); got != "Top-up kuota 5 GB" {
		t.Fatalf("description = %q", got)
	}
	for _, desc := range []string{"", "Top-up kuota", "Top-up kuota 5", "Top-up kuota X GB", "Top-up kuota 0 GB", "Top-up kuota -1 GB", "Paket Bulanan"} {
		if _, ok := topupdomain.ParseItemDescription(desc); ok {
			t.Fatalf("ParseItemDescription(%q) ok", desc)
		}
	}
}

func TestRemainingBytes(t *testing.T) {
	q := int64(10)
	if got := topupdomain.RemainingBytes(nil, 5, 3); got != nil {
		t.Fatalf("nil quota = %v", got)
	}
	if got := *topupdomain.RemainingBytes(&q, 5, 3); got != 12 {
		t.Fatalf("remaining = %d", got)
	}
	if got := *topupdomain.RemainingBytes(&q, 0, 99); got != 0 {
		t.Fatalf("clamped = %d", got)
	}
}

func TestBucketStartModes(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 34, 56, 0, time.UTC)
	tests := []struct {
		name, reset, anchor, want string
		at                        time.Time
	}{
		{"none", "none", "", topupdomain.EternalBucket, now},
		{"none ignores anchor", "none", "2026-01-15T00:00:00Z", topupdomain.EternalBucket, now},
		{"daily", "daily", "", "2026-11-07T00:00:00Z", now},
		{"monthly", "monthly", "", "2026-11-01T00:00:00Z", now},
		{"empty falls back monthly", "", "", "2026-11-01T00:00:00Z", now},
		{"unknown falls back monthly", "weekly", "", "2026-11-01T00:00:00Z", now},
		{"cycle past anniversary", "billing_cycle", "2026-01-05T10:00:00Z", "2026-11-05T00:00:00Z", now},
		{"cycle ahead anniversary", "billing_cycle", "2026-01-20T10:00:00Z", "2026-10-20T00:00:00Z", now},
		{"cycle bad anchor", "billing_cycle", "nope", "2026-11-01T00:00:00Z", now},
	}
	for _, tt := range tests {
		if got := topupdomain.BucketStart(tt.reset, tt.anchor, tt.at); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	// 31st anchor clamps to short months: Nov 30 covers Dec-anniversary.
	at := time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)
	if got := topupdomain.BucketStart("billing_cycle", "2026-01-31T00:00:00Z", at); got != "2026-11-30T00:00:00Z" {
		t.Fatalf("clamped = %q", got)
	}
	if got := topupdomain.MonthStart(now); got != "2026-11-01T00:00:00Z" {
		t.Fatalf("MonthStart = %q", got)
	}
}

// TestBucketStartParityWithRadius pins the D1 contract: the billing bucket
// key must equal the radiusd enforcement bucket key for every mode.
func TestBucketStartParityWithRadius(t *testing.T) {
	if topupdomain.EternalBucket != radius.EternalBucket {
		t.Fatalf("eternal %q != radius %q", topupdomain.EternalBucket, radius.EternalBucket)
	}
	resets := []string{"none", "daily", "monthly", "billing_cycle", "", "bogus"}
	anchors := []string{"", "2026-01-15T00:00:00Z", "2026-01-31T23:00:00Z", "junk"}
	ats := []time.Time{
		time.Date(2026, 11, 7, 12, 34, 56, 0, time.UTC),
		time.Date(2026, 2, 28, 23, 59, 59, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, reset := range resets {
		for _, anchor := range anchors {
			for _, at := range ats {
				want := radius.BucketStart(reset, anchor, at)
				if got := topupdomain.BucketStart(reset, anchor, at); got != want {
					t.Fatalf("BucketStart(%q,%q,%v) = %q, radius %q", reset, anchor, at, got, want)
				}
			}
		}
	}
}

func TestNewTopupValidation(t *testing.T) {
	now := time.Now().UTC()
	if _, err := topupdomain.NewTopup("t1", "a1", topupdomain.Preset5GB, "2026-11-01T00:00:00Z", "inv1", now); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, args := range map[string]struct {
		id, acc, period, inv string
		bytes                int64
	}{
		"empty id":      {"", "a1", "p", "i", topupdomain.Preset1GB},
		"empty account": {"t", "", "p", "i", topupdomain.Preset1GB},
		"bad preset":    {"t", "a1", "p", "i", 123},
		"empty period":  {"t", "a1", "", "i", topupdomain.Preset1GB},
		"empty invoice": {"t", "a1", "p", "", topupdomain.Preset1GB},
	} {
		if _, err := topupdomain.NewTopup(args.id, args.acc, args.bytes, args.period, args.inv, now); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}
