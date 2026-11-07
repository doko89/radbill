// Package domain holds the scheduler bounded-context core: billing period
// math, job status, and the ports the jobs run against.
package domain

import "time"

// Jobs run by the scheduler.
const (
	JobGenerate = "generate"
	JobIsolate  = "isolate"
)

// PeriodStart returns the start of the current billing period (00:00 UTC).
// Mode "fixed" anchors to dayOfMonth; anything else anchors to the account's
// creation day (activation mode). Anchors are clamped to the month length,
// so day 31 in February means the last day of February.
func PeriodStart(mode string, dayOfMonth int, createdAt, now time.Time) time.Time {
	anchor := dayOfMonth
	if mode != "fixed" {
		anchor = createdAt.UTC().Day()
	}
	if anchor < 1 {
		anchor = 1
	}
	if anchor > 31 {
		anchor = 31
	}
	now = now.UTC()
	start := monthAnchor(now.Year(), now.Month(), anchor)
	if start.After(now) {
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		start = monthAnchor(prev.Year(), prev.Month(), anchor)
	}
	return start
}

// PeriodLabel renders the period for invoice descriptions (YYYY-MM).
func PeriodLabel(start time.Time) string {
	return start.UTC().Format("2006-01")
}

func monthAnchor(year int, month time.Month, anchor int) time.Time {
	if last := lastDayOfMonth(year, month); anchor > last {
		anchor = last
	}
	return time.Date(year, month, anchor, 0, 0, 0, 0, time.UTC)
}

func lastDayOfMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Status is the last-run record of one job.
type Status struct {
	Job       string
	LastRun   time.Time // zero when the job never ran
	LastOK    bool
	LastError string
	Runs      int64
}
