package domain

import (
	"testing"
	"time"
)

func TestPeriodStart(t *testing.T) {
	nov15 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		mode    string
		day     int
		created time.Time
		now     time.Time
		want    string // YYYY-MM-DD
	}{
		{"fixed day 1 mid-month", "fixed", 1, nov15, nov15, "2026-11-01"},
		{"fixed future anchor goes prev month", "fixed", 20, nov15, nov15, "2026-10-20"},
		{"fixed anchor equals today", "fixed", 15, nov15, nov15, "2026-11-15"},
		{"fixed jan wraps to dec", "fixed", 20, nov15, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "2025-12-20"},
		{"fixed day 31 clamped in feb", "fixed", 31, nov15, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), "2026-02-28"},
		{"activation anchors to created day", "activation", 1, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), nov15, "2026-11-10"},
		{"activation future day goes prev month", "activation", 1, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), nov15, "2026-10-20"},
		{"unknown mode behaves like activation", "bogus", 1, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), nov15, "2026-11-10"},
		{"day clamped to 1", "fixed", 0, nov15, nov15, "2026-11-01"},
		{"day clamped to 31", "fixed", 99, nov15, time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), "2026-10-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PeriodStart(tt.mode, tt.day, tt.created, tt.now).Format("2006-01-02")
			if got != tt.want {
				t.Fatalf("PeriodStart = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPeriodLabel(t *testing.T) {
	if got := PeriodLabel(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); got != "2026-11" {
		t.Fatalf("label = %q", got)
	}
}
