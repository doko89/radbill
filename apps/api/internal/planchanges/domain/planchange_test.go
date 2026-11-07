package domain_test

import (
	"testing"
	"time"

	planchangesdomain "boilerplate/api/internal/planchanges/domain"
)

func TestMonthBounds(t *testing.T) {
	days, rem := planchangesdomain.MonthBounds(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if days != 30 || rem != 24 {
		t.Fatalf("nov7 = %d/%d", days, rem)
	}
	days, rem = planchangesdomain.MonthBounds(time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))
	if days != 30 || rem != 1 {
		t.Fatalf("nov30 = %d/%d", days, rem)
	}
	days, rem = planchangesdomain.MonthBounds(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if days != 28 || rem != 28 {
		t.Fatalf("feb1 = %d/%d", days, rem)
	}
}

func TestNextMonthStart(t *testing.T) {
	got := planchangesdomain.NextMonthStart(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next = %v", got)
	}
	got = planchangesdomain.NextMonthStart(time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC))
	if want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("year roll = %v", got)
	}
}

func TestProrata(t *testing.T) {
	tests := []struct {
		diff     int64
		rem, day int
		want     int64
	}{
		{50000, 24, 30, 40000}, // exact
		{50000, 15, 30, 25000}, // half
		{10000, 1, 30, 333},    // 333.33 rounds down
		{10000, 29, 30, 9667},  // 9666.67 rounds up
		{100, 15, 30, 50},      // small exact
		{1, 1, 30, 0},          // rounds to zero (no invoice)
		{0, 15, 30, 0},         // equal price
		{-5, 15, 30, 0},        // negative guarded
		{50000, 0, 30, 0},      // no days guarded
		{50000, 15, 0, 0},      // no month guarded
	}
	for _, tt := range tests {
		if got := planchangesdomain.Prorata(tt.diff, tt.rem, tt.day); got != tt.want {
			t.Fatalf("Prorata(%d,%d,%d) = %d, want %d", tt.diff, tt.rem, tt.day, got, tt.want)
		}
	}
}

func TestNewPlanChangeValidation(t *testing.T) {
	now := time.Now().UTC()
	if _, err := planchangesdomain.NewPlanChange("c1", "a1", "p1", "p2", now, planchangesdomain.StatusPending, now); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if _, err := planchangesdomain.NewPlanChange("c1", "a1", "p1", "p1", now, planchangesdomain.StatusPending, now); err != planchangesdomain.ErrSamePackage {
		t.Fatalf("same = %v", err)
	}
	if _, err := planchangesdomain.NewPlanChange("c1", "a1", "p1", "p2", now, "bogus", now); err == nil {
		t.Fatal("bad status accepted")
	}
	if _, err := planchangesdomain.NewPlanChange("", "a1", "p1", "p2", now, planchangesdomain.StatusPending, now); err == nil {
		t.Fatal("empty id accepted")
	}
}
