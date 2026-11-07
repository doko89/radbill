// Package domain holds the packages bounded-context core.
package domain

import (
	"context"
	"errors"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// Quota reset modes.
const (
	QuotaResetNone         = "none"
	QuotaResetDaily        = "daily"
	QuotaResetMonthly      = "monthly"
	QuotaResetBillingCycle = "billing_cycle"
)

// On-exhausted actions.
const (
	OnExhaustedReject   = "reject"
	OnExhaustedIsolate  = "isolate"
	OnExhaustedThrottle = "throttle"
)

// Package types: basis pembatasan utama paket.
const (
	TypeTime  = "time"
	TypeQuota = "quota"
	TypeFUP   = "fup"
)

var (
	// ErrPackageNotFound is returned when the package does not exist.
	ErrPackageNotFound = errors.New("package not found")
	// ErrPackageInUse is returned when deleting a package with PPPoE accounts.
	ErrPackageInUse = errors.New("package still has pppoe accounts")
)

// Package is a sellable combo of rate, validity, quota, and FUP rules.
// Type selects the primary limit basis; per-type field completeness is
// enforced by the UI, the API only validates the enum (empty = legacy,
// normalized to TypeTime) so older clients keep working.
type Package struct {
	ID                 string
	Name               string
	Price              int64
	Period             string
	Type               string
	RateLimit          string
	AddressPool        string
	ValiditySeconds    *int64
	OnlineSecondsLimit *int64
	QuotaBytes         *int64
	QuotaReset         string
	FUPThresholdBytes  *int64
	FUPRateLimit       string
	OnExhausted        string
	IsPublic           bool
	AllowSelfService   bool
	UpgradeGroup       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Details carries validated mutable package fields.
type Details struct {
	Name               string
	Price              int64
	Period             string
	Type               string
	RateLimit          string
	AddressPool        string
	ValiditySeconds    *int64
	OnlineSecondsLimit *int64
	QuotaBytes         *int64
	QuotaReset         string
	FUPThresholdBytes  *int64
	FUPRateLimit       string
	OnExhausted        string
	IsPublic           bool
	AllowSelfService   bool
	UpgradeGroup       string
}

// NewPackage builds a validated package.
func NewPackage(id string, d Details, now time.Time) (*Package, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	p := &Package{ID: id, CreatedAt: now, UpdatedAt: now}
	if err := p.SetDetails(d, now); err != nil {
		return nil, err
	}
	return p, nil
}

// SetDetails validates and replaces mutable fields.
func (p *Package) SetDetails(d Details, now time.Time) error {
	if d.Name == "" {
		return apperr.Invalid("name", "required")
	}
	if len(d.Name) > 120 {
		return apperr.Invalid("name", "max 120 characters")
	}
	if d.Price < 0 {
		return apperr.Invalid("price", "must be >= 0")
	}
	if d.Period == "" {
		return apperr.Invalid("period", "required")
	}
	if len(d.Period) > 32 {
		return apperr.Invalid("period", "max 32 characters")
	}
	switch d.Type {
	case "", TypeTime:
		d.Type = TypeTime
	case TypeQuota, TypeFUP:
	default:
		return apperr.Invalid("type", "must be time|quota|fup")
	}
	if len(d.RateLimit) > 64 {
		return apperr.Invalid("rate_limit", "max 64 characters")
	}
	if len(d.AddressPool) > 64 {
		return apperr.Invalid("address_pool", "max 64 characters")
	}
	for field, v := range map[string]*int64{
		"validity_seconds": d.ValiditySeconds, "online_seconds_limit": d.OnlineSecondsLimit,
		"quota_bytes": d.QuotaBytes, "fup_threshold_bytes": d.FUPThresholdBytes,
	} {
		if v != nil && *v <= 0 {
			return apperr.Invalid(field, "must be > 0")
		}
	}
	switch d.QuotaReset {
	case "", QuotaResetNone:
		d.QuotaReset = QuotaResetNone
	case QuotaResetDaily, QuotaResetMonthly, QuotaResetBillingCycle:
	default:
		return apperr.Invalid("quota_reset", "must be none|daily|monthly|billing_cycle")
	}
	if len(d.FUPRateLimit) > 64 {
		return apperr.Invalid("fup_rate_limit", "max 64 characters")
	}
	switch d.OnExhausted {
	case "", OnExhaustedReject:
		d.OnExhausted = OnExhaustedReject
	case OnExhaustedIsolate, OnExhaustedThrottle:
	default:
		return apperr.Invalid("on_exhausted", "must be reject|isolate|throttle")
	}
	if len(d.UpgradeGroup) > 64 {
		return apperr.Invalid("upgrade_group", "max 64 characters")
	}
	p.Name = d.Name
	p.Price = d.Price
	p.Period = d.Period
	p.Type = d.Type
	p.RateLimit = d.RateLimit
	p.AddressPool = d.AddressPool
	p.ValiditySeconds = d.ValiditySeconds
	p.OnlineSecondsLimit = d.OnlineSecondsLimit
	p.QuotaBytes = d.QuotaBytes
	p.QuotaReset = d.QuotaReset
	p.FUPThresholdBytes = d.FUPThresholdBytes
	p.FUPRateLimit = d.FUPRateLimit
	p.OnExhausted = d.OnExhausted
	p.IsPublic = d.IsPublic
	p.AllowSelfService = d.AllowSelfService
	p.UpgradeGroup = d.UpgradeGroup
	p.UpdatedAt = now
	return nil
}

// Repository persists packages. Implemented in infrastructure.
type Repository interface {
	Create(ctx context.Context, p *Package) error
	Get(ctx context.Context, id string) (*Package, error)
	List(ctx context.Context, limit, offset int) ([]*Package, error)
	Count(ctx context.Context) (int64, error)
	Update(ctx context.Context, p *Package) error
	Delete(ctx context.Context, id string) error
}
