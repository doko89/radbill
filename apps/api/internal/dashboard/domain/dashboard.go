// Package domain holds the dashboard bounded-context core: the admin
// dashboard and metrics shapes plus the store port.
//
// Fase 4 (agen E1/G4, ADITIF). The JSON shapes below are the E3 contract:
// E3 codes the frontend dashboard against them, so field names are
// frozen. Pure: no fiber/sql/os imports.
package domain

import "context"

// RecentPaymentsLimit caps the dashboard recent-payments widget.
const RecentPaymentsLimit = 5

// Accounts counts PPPoE accounts per status.
type Accounts struct {
	Active   int64 `json:"active"`
	Isolated int64 `json:"isolated"`
	Disabled int64 `json:"disabled"`
}

// RecentPayment is one settled payment with its invoice number.
type RecentPayment struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Amount int64  `json:"amount"`
	PaidAt string `json:"paid_at"`
}

// Dashboard is GET /api/v1/admin/dashboard: revenue month-to-date,
// outstanding receivables, overdue count, account mix, online sessions,
// and recent payments (newest first).
type Dashboard struct {
	RevenueMTD       int64           `json:"revenue_mtd"`
	OutstandingTotal int64           `json:"outstanding_total"`
	OverdueCount     int64           `json:"overdue_count"`
	Accounts         Accounts        `json:"accounts"`
	OnlineSessions   int64           `json:"online_sessions"`
	RecentPayments   []RecentPayment `json:"recent_payments"`
}

// SchedulerJob mirrors one scheduler status row (same JSON shape as
// GET /api/v1/admin/scheduler/status items).
type SchedulerJob struct {
	Job       string  `json:"job"`
	LastRun   *string `json:"last_run"`
	LastOK    bool    `json:"last_ok"`
	LastError string  `json:"last_error"`
	Runs      int64   `json:"runs"`
}

// SchedulerStatus mirrors GET /api/v1/admin/scheduler/status data.
type SchedulerStatus struct {
	Jobs []SchedulerJob `json:"jobs"`
}

// Metrics is GET /api/v1/admin/metrics: process uptime, DB
// reachability, scheduler jobs, pending QRIS attempts, and attempts
// parked failed in the trailing 24h.
type Metrics struct {
	UptimeSeconds     int64           `json:"uptime_seconds"`
	DbOK              bool            `json:"db_ok"`
	Scheduler         SchedulerStatus `json:"scheduler"`
	KriskrisPending   int64           `json:"kriskris_pending"`
	WebhooksFailed24h int64           `json:"webhooks_failed_24h"`
}

// Unpaid is the dashboard slice of an unpaid invoice.
type Unpaid struct {
	Total   int64
	DueDate string // YYYY-MM-DD
}

// Store abstracts dashboard database access. Implemented in
// infrastructure. Sums are computed in Go over scanned rows (portable
// across sqlite/postgres without COALESCE type quirks).
type Store interface {
	// PaymentsSince returns payment amounts with paid_at >= since.
	PaymentsSince(ctx context.Context, since string) ([]int64, error)
	// UnpaidTotals returns every unpaid invoice total + due date.
	UnpaidTotals(ctx context.Context) ([]Unpaid, error)
	// PPPoECounts returns account counts keyed by status.
	PPPoECounts(ctx context.Context) (map[string]int64, error)
	// OnlineSessions counts sessions with stopped_at IS NULL.
	OnlineSessions(ctx context.Context) (int64, error)
	// RecentPayments returns the newest settled payments.
	RecentPayments(ctx context.Context, limit int) ([]RecentPayment, error)
	// PendingAttempts counts payment_attempts with status pending.
	PendingAttempts(ctx context.Context) (int64, error)
	// FailedAttemptsSince counts attempts parked failed at/after since.
	FailedAttemptsSince(ctx context.Context, since string) (int64, error)
}
