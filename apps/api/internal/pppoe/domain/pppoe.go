// Package domain holds the pppoe bounded-context core.
package domain

import (
	"context"
	"errors"
	"net"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// PPPoE account statuses.
const (
	StatusActive   = "active"
	StatusIsolated = "isolated"
	StatusDisabled = "disabled"
)

var (
	// ErrPPPoENotFound is returned when the PPPoE account does not exist.
	ErrPPPoENotFound = errors.New("pppoe account not found")
	// ErrUsernameTaken is returned on duplicate PPPoE usernames.
	ErrUsernameTaken = errors.New("pppoe username already taken")
	// ErrBadTransition is returned for illegal status transitions.
	ErrBadTransition = errors.New("illegal status transition")
	// ErrUnknownCustomer is returned when customer_id does not exist.
	ErrUnknownCustomer = errors.New("unknown customer")
	// ErrUnknownPackage is returned when package_id does not exist.
	ErrUnknownPackage = errors.New("unknown package")
	// ErrUnknownNAS is returned when nas_id does not exist.
	ErrUnknownNAS = errors.New("unknown nas")
	// ErrPortalExists is returned when the customer already has a portal account.
	ErrPortalExists = errors.New("customer already has a portal account")
	// ErrNoPortalAccount is returned when the customer has no portal account.
	ErrNoPortalAccount = errors.New("customer has no portal account")
	// ErrPPPoEInUse is returned when deleting an account with sessions/invoices.
	ErrPPPoEInUse = errors.New("pppoe account still has sessions or invoices")
)

// PPPoE is a MikroTik login owned by a customer under one package.
// Password is PLAINTEXT by product decision (radiusd needs it for PAP/CHAP):
// it must never be logged and is only returned via explicit ?reveal=true.
type PPPoE struct {
	ID         string
	CustomerID string
	PackageID  string
	NASID      string
	Username   string
	Password   string
	Status     string
	ExpiresAt  *time.Time
	StaticIP   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Details carries validated mutable PPPoE fields.
type Details struct {
	CustomerID string
	PackageID  string
	NASID      string
	Username   string
	Password   string
	ExpiresAt  *time.Time
	StaticIP   string
}

// NewPPPoE builds a validated active PPPoE account.
func NewPPPoE(id string, d Details, now time.Time) (*PPPoE, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	p := &PPPoE{ID: id, Status: StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := p.SetDetails(d, now); err != nil {
		return nil, err
	}
	return p, nil
}

// SetDetails validates and replaces mutable fields (not status).
func (p *PPPoE) SetDetails(d Details, now time.Time) error {
	if d.CustomerID == "" {
		return apperr.Invalid("customer_id", "required")
	}
	if d.PackageID == "" {
		return apperr.Invalid("package_id", "required")
	}
	if len(d.Username) < 3 || len(d.Username) > 64 {
		return apperr.Invalid("username", "must be 3..64 characters")
	}
	if len(d.Password) < 4 || len(d.Password) > 128 {
		return apperr.Invalid("password", "must be 4..128 characters")
	}
	if d.StaticIP != "" && net.ParseIP(d.StaticIP) == nil {
		return apperr.Invalid("static_ip", "must be a valid IP address")
	}
	p.CustomerID = d.CustomerID
	p.PackageID = d.PackageID
	p.NASID = d.NASID
	p.Username = d.Username
	p.Password = d.Password
	p.ExpiresAt = d.ExpiresAt
	p.StaticIP = d.StaticIP
	p.UpdatedAt = now
	return nil
}

// Isolate moves an active (or already isolated) account to isolated.
func (p *PPPoE) Isolate(now time.Time) error {
	switch p.Status {
	case StatusActive, StatusIsolated:
		p.Status = StatusIsolated
		p.UpdatedAt = now
		return nil
	default:
		return ErrBadTransition
	}
}

// Activate moves an isolated or disabled account to active.
func (p *PPPoE) Activate(now time.Time) error {
	switch p.Status {
	case StatusActive:
		return nil
	case StatusIsolated, StatusDisabled:
		p.Status = StatusActive
		p.UpdatedAt = now
		return nil
	default:
		return ErrBadTransition
	}
}

// Disable moves any account to disabled.
func (p *PPPoE) Disable(now time.Time) {
	p.Status = StatusDisabled
	p.UpdatedAt = now
}

// Extend sets a new expiry; it must be in the future.
func (p *PPPoE) Extend(expiresAt time.Time, now time.Time) error {
	if !expiresAt.After(now) {
		return apperr.Invalid("expires_at", "must be in the future")
	}
	p.ExpiresAt = &expiresAt
	p.UpdatedAt = now
	return nil
}

// ChangePackage switches the package.
func (p *PPPoE) ChangePackage(packageID string, now time.Time) error {
	if packageID == "" {
		return apperr.Invalid("package_id", "required")
	}
	p.PackageID = packageID
	p.UpdatedAt = now
	return nil
}

// Repository persists PPPoE accounts. Implemented in infrastructure.
type Repository interface {
	Create(ctx context.Context, p *PPPoE) error
	Get(ctx context.Context, id string) (*PPPoE, error)
	GetByUsername(ctx context.Context, username string) (*PPPoE, error)
	List(ctx context.Context, limit, offset int) ([]*PPPoE, error)
	Count(ctx context.Context) (int64, error)
	Update(ctx context.Context, p *PPPoE) error
	Delete(ctx context.Context, id string) error
	// ResetQuota clears usage counters for the account.
	ResetQuota(ctx context.Context, id string) error
	// CustomerExists/PackageExists/NASExists validate foreign keys.
	CustomerExists(ctx context.Context, id string) bool
	PackageExists(ctx context.Context, id string) bool
	NASExists(ctx context.Context, id string) bool
}

// PortalStore creates and resets customer portal logins.
// Implemented in infrastructure (accounts + customers in one transaction).
type PortalStore interface {
	// CreatePortalAccount creates a role=user account and links it to the
	// customer. It returns the account id.
	CreatePortalAccount(ctx context.Context, customerID, username, passwordHash string, now time.Time) (string, error)
	// ResetPortalPassword replaces the portal password (hash) and revokes
	// refresh tokens. It returns the portal username.
	ResetPortalPassword(ctx context.Context, customerID, passwordHash string, now time.Time) (string, error)
}

// Disconnecter drops live RADIUS sessions. Implemented in infrastructure
// on top of network/coa; it returns the number of ACKed disconnects.
type Disconnecter interface {
	DisconnectAll(ctx context.Context, pppoeID string) (disconnected, total int, err error)
}
