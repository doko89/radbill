// Package domain holds the topup bounded-context core: quota top-up
// pricing, preset validation, and the usage-bucket math shared with D1.
package domain

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// GB is one gibibyte in bytes. Presets and prices are computed on it.
const GB = 1073741824

// Top-up size presets (bytes). Anything else is rejected with ErrBadPreset.
const (
	Preset1GB  = 1 * GB
	Preset5GB  = 5 * GB
	Preset10GB = 10 * GB
)

// SettingsKeyPricePerGB is the rupiah-per-GB price key in settings.
const SettingsKeyPricePerGB = "topup_price_per_gb"

// DefaultPricePerGB applies when the setting is missing or invalid.
const DefaultPricePerGB = 10000

// EternalBucket is the single usage bucket for quota_reset=none. It mirrors
// radius.EternalBucket (D1 contract): top-ups for such packages must be
// stored under this same key.
const EternalBucket = "1970-01-01T00:00:00Z"

var (
	// ErrAccountNotFound covers unknown accounts AND other customers'
	// accounts (IDOR-safe 404, never 403).
	ErrAccountNotFound = errors.New("topup: pppoe account not found")
	// ErrBadPreset is returned when bytes is not a {1,5,10}GB preset.
	ErrBadPreset = errors.New("topup: bytes must be a 1, 5, or 10 GB preset")
	// ErrNoQuotaPackage is returned when the account package has no quota.
	ErrNoQuotaPackage = errors.New("topup: package has no quota")
	// ErrInvoiceNotFound is returned when the paid invoice is unknown.
	ErrInvoiceNotFound = errors.New("topup: invoice not found")
)

// IsPreset reports whether bytes is exactly a {1,5,10}GB preset.
func IsPreset(bytes int64) bool {
	switch bytes {
	case Preset1GB, Preset5GB, Preset10GB:
		return true
	default:
		return false
	}
}

// PriceForBytes returns ceil(bytes/GB)*pricePerGB in rupiah.
func PriceForBytes(bytes, pricePerGB int64) int64 {
	if bytes <= 0 || pricePerGB <= 0 {
		return 0
	}
	return ((bytes + GB - 1) / GB) * pricePerGB
}

// ItemDescription renders the invoice line for a top-up. ApplyOnInvoicePaid
// recognises top-up invoices by this prefix (see ParseItemDescription).
func ItemDescription(bytes int64) string {
	return fmt.Sprintf("Top-up kuota %d GB", bytes/GB)
}

// ParseItemDescription extracts top-up bytes from an invoice line.
// ok=false means the line is not a top-up line.
func ParseItemDescription(desc string) (bytes int64, ok bool) {
	n, found := strings.CutPrefix(desc, "Top-up kuota ")
	if !found {
		return 0, false
	}
	n, found = strings.CutSuffix(n, " GB")
	if !found {
		return 0, false
	}
	gb, err := strconv.ParseInt(n, 10, 64)
	if err != nil || gb <= 0 {
		return 0, false
	}
	return gb * GB, true
}

// BucketStart returns the usage-bucket key (RFC3339 UTC) covering now for
// the given quota_reset mode. anchor is the account created_at (RFC3339),
// used only by billing_cycle. It is a line-by-line mirror of
// radius.BucketStart (D1 owns that copy; this package must not import the
// radiusd domain): none -> eternal bucket, daily -> UTC day start,
// monthly/""/unknown -> UTC month start, billing_cycle -> most recent
// monthly anniversary of the creation day (clamped to month length).
func BucketStart(reset, anchor string, now time.Time) string {
	u := now.UTC()
	switch reset {
	case "none":
		return EternalBucket
	case "daily":
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	case "billing_cycle":
		if b, ok := cycleBucket(anchor, u); ok {
			return b
		}
		return monthStart(u)
	case "monthly", "":
		return monthStart(u)
	default:
		return monthStart(u)
	}
}

// MonthStart returns midnight UTC of the first of the month (RFC3339).
func MonthStart(now time.Time) string { return monthStart(now.UTC()) }

func monthStart(u time.Time) string {
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

func cycleBucket(anchor string, now time.Time) (string, bool) {
	t, err := time.Parse(time.RFC3339, anchor)
	if err != nil {
		return "", false
	}
	day := t.UTC().Day()
	cand := cycleDay(now.Year(), now.Month(), day)
	if cand.After(now) {
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		cand = cycleDay(prev.Year(), prev.Month(), day)
	}
	return cand.Format(time.RFC3339), true
}

func cycleDay(year int, month time.Month, day int) time.Time {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// RemainingBytes returns quota+topup-used clamped at zero, or nil when the
// package has no quota (unmetered). Pure helper shared with portal summary.
func RemainingBytes(quota *int64, topupBytes, usedBytes int64) *int64 {
	if quota == nil {
		return nil
	}
	rem := *quota + topupBytes - usedBytes
	if rem < 0 {
		rem = 0
	}
	return &rem
}

// Topup is one purchased quota increment bound to its paid invoice.
type Topup struct {
	ID             string
	PPPoEAccountID string
	Bytes          int64
	PeriodStart    string
	InvoiceID      string
	CreatedAt      time.Time
}

// NewTopup builds a validated top-up row.
func NewTopup(id, accountID string, bytes int64, periodStart, invoiceID string, now time.Time) (*Topup, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	if accountID == "" {
		return nil, apperr.Invalid("pppoe_account_id", "required")
	}
	if !IsPreset(bytes) {
		return nil, ErrBadPreset
	}
	if periodStart == "" {
		return nil, apperr.Invalid("period_start", "required")
	}
	if invoiceID == "" {
		return nil, apperr.Invalid("invoice_id", "required")
	}
	return &Topup{
		ID: id, PPPoEAccountID: accountID, Bytes: bytes,
		PeriodStart: periodStart, InvoiceID: invoiceID, CreatedAt: now,
	}, nil
}

// Account is the top-up slice of a PPPoE account.
type Account struct {
	ID         string
	CustomerID string
	PackageID  string
	CreatedAt  string // RFC3339, bucket anchor for billing_cycle
}

// Package is the top-up slice of a package.
type Package struct {
	ID         string
	QuotaBytes *int64
	QuotaReset string
}

// Invoice is the paid-detection slice of an invoice.
type Invoice struct {
	ID             string
	PPPoEAccountID string
	Status         string
	Paid           bool
}

// Item is one invoice line.
type Item struct {
	Description string
	Qty         int64
	Price       int64
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

// AccountStore resolves top-up-relevant account+package rows.
type AccountStore interface {
	GetAccount(ctx context.Context, id string) (*Account, error)
	GetPackage(ctx context.Context, id string) (*Package, error)
}

// PriceStore resolves topup_price_per_gb (default 10000).
type PriceStore interface {
	PricePerGB(ctx context.Context) (int64, error)
}

// TopupStore persists quota_topups rows.
type TopupStore interface {
	// FindByInvoice returns the top-up for an invoice, or (nil, nil).
	FindByInvoice(ctx context.Context, invoiceID string) (*Topup, error)
	Insert(ctx context.Context, t *Topup) error
	// SumByAccountPeriod sums top-up bytes in one bucket.
	SumByAccountPeriod(ctx context.Context, accountID, periodStart string) (int64, error)
}

// InvoiceStore reads invoices+items for apply-on-paid.
type InvoiceStore interface {
	GetInvoice(ctx context.Context, id string) (*Invoice, error)
	ListItems(ctx context.Context, invoiceID string) ([]Item, error)
}

// InvoiceCreator creates the unpaid top-up invoice (billing adapter).
type InvoiceCreator interface {
	Create(ctx context.Context, pppoeID string, items []ItemInput, dueDate string) (CreatedInvoice, error)
}

// Emitter publishes cache-refresh events. Satisfied by network/notify.
type Emitter interface {
	Emit(ctx context.Context, typ, action, id string)
}
