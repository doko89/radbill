// Package domain holds the planchanges bounded-context core: self-service
// plan-change rules (PRD §6.5), prorata math, and period helpers.
package domain

import (
	"context"
	"errors"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// Plan-change statuses (CHECKed in DDL).
const (
	StatusPending   = "pending"
	StatusApplied   = "applied"
	StatusCancelled = "cancelled"
)

var (
	// ErrAccountNotFound covers unknown accounts AND other customers'
	// accounts (IDOR-safe 404, never 403).
	ErrAccountNotFound = errors.New("planchanges: pppoe account not found")
	// ErrPackageNotFound is returned when the target package is unknown.
	ErrPackageNotFound = errors.New("planchanges: target package not found")
	// ErrSamePackage is returned when target equals the current package.
	ErrSamePackage = errors.New("planchanges: already on that package")
	// ErrNotSelfService is returned when the target package forbids
	// self-service moves.
	ErrNotSelfService = errors.New("planchanges: package is not self-service")
	// ErrGroupMismatch is returned when the target upgrade_group differs.
	ErrGroupMismatch = errors.New("planchanges: package is in another upgrade group")
	// ErrHasUnpaid is returned when the account has an unpaid invoice.
	ErrHasUnpaid = errors.New("planchanges: account has an unpaid invoice")
	// ErrFrequencyExceeded is returned on the 2nd change within one period.
	ErrFrequencyExceeded = errors.New("planchanges: only one change per period")
	// ErrChangeNotFound covers unknown changes AND other customers'
	// changes (IDOR-safe 404).
	ErrChangeNotFound = errors.New("planchanges: plan change not found")
	// ErrNotPending is returned when cancelling a non-pending change.
	ErrNotPending = errors.New("planchanges: only pending changes can be cancelled")
)

// PlanChange records one self-service package move.
type PlanChange struct {
	ID             string
	PPPoEAccountID string
	FromPackage    string // package id
	ToPackage      string // package id
	EffectiveAt    time.Time
	Status         string
	CreatedAt      time.Time
}

// NewPlanChange builds a validated plan-change row.
func NewPlanChange(id, accountID, fromPackage, toPackage string, effectiveAt time.Time, status string, now time.Time) (*PlanChange, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	if accountID == "" {
		return nil, apperr.Invalid("pppoe_account_id", "required")
	}
	if fromPackage == "" || toPackage == "" {
		return nil, apperr.Invalid("to_package_id", "required")
	}
	if fromPackage == toPackage {
		return nil, ErrSamePackage
	}
	switch status {
	case StatusPending, StatusApplied, StatusCancelled:
	default:
		return nil, apperr.Invalid("status", "must be pending|applied|cancelled")
	}
	return &PlanChange{
		ID: id, PPPoEAccountID: accountID,
		FromPackage: fromPackage, ToPackage: toPackage,
		EffectiveAt: effectiveAt, Status: status, CreatedAt: now,
	}, nil
}

// Account is the plan-change slice of a PPPoE account.
type Account struct {
	ID         string
	CustomerID string
	PackageID  string
}

// Package is the plan-change slice of a package.
type Package struct {
	ID               string
	Name             string
	Price            int64
	AllowSelfService bool
	UpgradeGroup     string
}

// ItemInput carries one raw invoice line for creation.
type ItemInput struct {
	Description string
	Qty         int64
	Price       int64
}

// CreatedInvoice is the slice of a created invoice the portal needs.
type CreatedInvoice struct {
	ID     string
	Number string
	Total  int64
}

// AccountStore resolves accounts/packages and applies package switches.
type AccountStore interface {
	GetAccount(ctx context.Context, id string) (*Account, error)
	GetPackage(ctx context.Context, id string) (*Package, error)
	// SetPackage switches the account package and emits pppoe/upsert.
	SetPackage(ctx context.Context, accountID, packageID string, now time.Time) error
	// HasUnpaid reports whether the account has an unpaid invoice.
	HasUnpaid(ctx context.Context, accountID string) (bool, error)
}

// PlanStore persists plan_changes rows.
type PlanStore interface {
	Insert(ctx context.Context, c *PlanChange) error
	Get(ctx context.Context, id string) (*PlanChange, error)
	ListByAccount(ctx context.Context, accountID string, limit, offset int) ([]*PlanChange, error)
	ListMine(ctx context.Context, customerID string, limit, offset int) ([]*PlanChange, error)
	CountMine(ctx context.Context, customerID string) (int64, error)
	ListAll(ctx context.Context, limit, offset int) ([]*PlanChange, error)
	CountAll(ctx context.Context) (int64, error)
	// CountActiveSince counts pending+applied rows for the account
	// created at or after since (RFC3339, frequency rule).
	CountActiveSince(ctx context.Context, accountID, since string) (int64, error)
	// ListDue returns pending rows with effective_at <= at (RFC3339).
	ListDue(ctx context.Context, at string, limit int) ([]*PlanChange, error)
	SetStatus(ctx context.Context, id, status string) (*PlanChange, error)
}

// Billing creates prorata invoices (billing service adapter).
type Billing interface {
	Create(ctx context.Context, pppoeID string, items []ItemInput, dueDate string) (CreatedInvoice, error)
}

// Disconnecter drops live sessions. Satisfied by the pppoe CoA
// disconnecter; always best-effort for plan changes.
type Disconnecter interface {
	DisconnectAll(ctx context.Context, pppoeID string) (disconnected, total int, err error)
}

// MonthStart returns midnight UTC of the first of the month.
func MonthStart(now time.Time) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// NextMonthStart returns midnight UTC of the first of next month: the
// effective date of downgrades.
func NextMonthStart(now time.Time) time.Time {
	return MonthStart(now).AddDate(0, 1, 0)
}

// MonthBounds returns the days in the current UTC month and the remaining
// days including today (for prorata).
func MonthBounds(now time.Time) (daysInMonth, remainingIncludingToday int) {
	u := now.UTC()
	days := time.Date(u.Year(), u.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return days, days - u.Day() + 1
}

// Prorata returns the rounded-rupiah upgrade charge:
// round(diff * remaining / daysInMonth), half up.
func Prorata(diff int64, remainingDays, daysInMonth int) int64 {
	if diff <= 0 || remainingDays <= 0 || daysInMonth <= 0 {
		return 0
	}
	return (diff*int64(remainingDays) + int64(daysInMonth)/2) / int64(daysInMonth)
}
