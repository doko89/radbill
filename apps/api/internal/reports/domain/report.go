// Package domain holds the reports bounded-context core: JSON report
// shapes (dictated for D3) and the store port.
package domain

import (
	"context"
	"errors"
)

// Grouping modes for the revenue report.
const (
	GroupByDay   = "day"
	GroupByMonth = "month"
)

// Report range limits.
const (
	MaxRangeDays    = 366
	MaxGrowthMonths = 24
	MinGrowthMonths = 1
)

// Default windows when the caller omits from/to.
const (
	DefaultRangeDays    = 30
	DefaultGrowthMonths = 6
)

var (
	// ErrBadRange is returned for malformed or inverted date ranges.
	ErrBadRange = errors.New("reports: invalid from/to range")
	// ErrBadGroupBy is returned for group_by other than day|month.
	ErrBadGroupBy = errors.New("reports: group_by must be day|month")
	// ErrBadMonths is returned for months outside 1..24.
	ErrBadMonths = errors.New("reports: months must be 1..24")
)

// RevenuePoint is one revenue bucket: {period,total}. period is
// YYYY-MM-DD (day) or YYYY-MM (month), UTC.
type RevenuePoint struct {
	Period string `json:"period"`
	Total  int64  `json:"total"`
}

// Revenue is GET /admin/reports/revenue: {points,total}.
type Revenue struct {
	Points []RevenuePoint `json:"points"`
	Total  int64          `json:"total"`
}

// UsagePoint is one usage day: {date,bytes_in,bytes_out}.
type UsagePoint struct {
	Date     string `json:"date"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

// UsageTotals sums a usage report: {bytes_in,bytes_out}.
type UsageTotals struct {
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`
}

// Usage is GET /admin/reports/usage: {points,totals}.
type Usage struct {
	Points []UsagePoint `json:"points"`
	Totals UsageTotals  `json:"totals"`
}

// Receivable is one unpaid invoice with its owner.
type Receivable struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	CustomerName string `json:"customer_name"`
	Total        int64  `json:"total"`
	DueDate      string `json:"due_date"`
	DaysOverdue  int64  `json:"days_overdue"`
}

// Receivables is GET /admin/reports/receivables: {invoices,total}
// where total is the summed outstanding amount.
type Receivables struct {
	Invoices []Receivable `json:"invoices"`
	Total    int64        `json:"total"`
}

// GrowthPoint is one month: {period,new,churn,active}.
type GrowthPoint struct {
	Period string `json:"period"`
	New    int64  `json:"new"`
	Churn  int64  `json:"churn"`
	Active int64  `json:"active"`
}

// Growth is GET /admin/reports/growth: {points}.
type Growth struct {
	Points []GrowthPoint `json:"points"`
}

// Payment is the revenue slice of a payment row.
type Payment struct {
	Amount int64
	PaidAt string // RFC3339 UTC
}

// Session is the usage slice of a radius session row.
type Session struct {
	StartedAt string // RFC3339 UTC
	BytesIn   int64
	BytesOut  int64
}

// UnpaidInvoice is the receivables slice of an invoice row.
type UnpaidInvoice struct {
	ID           string
	Number       string
	CustomerName string
	Total        int64
	DueDate      string // YYYY-MM-DD
}

// AccountStatus is the growth slice of a PPPoE account row.
type AccountStatus struct {
	Status    string
	CreatedAt string // RFC3339 UTC
	UpdatedAt string // RFC3339 UTC
}

// Store abstracts report database access. Implemented in infrastructure.
type Store interface {
	// PaymentsInRange returns payments with paid_at in [from, to).
	PaymentsInRange(ctx context.Context, from, to string, limit int) ([]Payment, error)
	// SessionsInRange returns sessions with started_at in [from, to).
	SessionsInRange(ctx context.Context, from, to string, limit int) ([]Session, error)
	// UnpaidInvoices returns every unpaid invoice with its owner name.
	UnpaidInvoices(ctx context.Context, limit int) ([]UnpaidInvoice, error)
	// AccountStatuses pages every PPPoE account status slice.
	AccountStatuses(ctx context.Context, limit, offset int) ([]AccountStatus, error)
}
