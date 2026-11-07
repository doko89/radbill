// Package domain holds the jwtauth bounded-context core: panel accounts,
// JWT claims, RBAC permissions, and repository ports.
package domain

import (
	"context"
	"errors"
	"time"
)

// Roles for panel accounts.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Account statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Permissions enforced by the Require middleware.
const (
	PermCustomerRead  = "customer:read"
	PermCustomerWrite = "customer:write"
	PermPackageRead   = "package:read"
	PermPackageWrite  = "package:write"
	PermPPPoERead     = "pppoe:read"
	PermPPPoEWrite    = "pppoe:write"
	PermNASRead       = "nas:read"
	PermNASWrite      = "nas:write"
	PermInvoiceRead   = "invoice:read"
	PermInvoiceWrite  = "invoice:write"
	PermPaymentWrite  = "payment:write"
	PermSessionRead   = "session:read"
	PermSettingsWrite = "settings:write"
	PermAuditRead     = "audit:read"

	PermCustomerReadOwn = "customer:read_own"
	PermPPPoEReadOwn    = "pppoe:read_own"
	PermInvoiceReadOwn  = "invoice:read_own"
	PermPaymentWriteOwn = "payment:write_own"
	PermSessionReadOwn  = "session:read_own"
)

// RolePermissions maps a role to its granted permissions. New roles
// (cashier, technician) only add entries here; handlers check permissions,
// never roles.
var RolePermissions = map[string][]string{
	RoleAdmin: {
		PermCustomerRead, PermCustomerWrite,
		PermPackageRead, PermPackageWrite,
		PermPPPoERead, PermPPPoEWrite,
		PermNASRead, PermNASWrite,
		PermInvoiceRead, PermInvoiceWrite,
		PermPaymentWrite,
		PermSessionRead,
		PermSettingsWrite,
		PermAuditRead,
	},
	RoleUser: {
		PermCustomerReadOwn,
		PermPackageRead,
		PermPPPoEReadOwn,
		PermInvoiceReadOwn,
		PermPaymentWriteOwn,
		PermSessionReadOwn,
	},
}

// HasPermission reports whether role grants perm.
func HasPermission(role, perm string) bool {
	for _, p := range RolePermissions[role] {
		if p == perm {
			return true
		}
	}
	return false
}

// PermissionsFor returns the granted permissions for role (empty when unknown).
func PermissionsFor(role string) []string {
	out := RolePermissions[role]
	if out == nil {
		return []string{}
	}
	cp := make([]string, len(out))
	copy(cp, out)
	return cp
}

var (
	// ErrInvalidCredentials is returned when username/password do not match
	// or the account cannot log in. Handlers map it to 401.
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrAccountDisabled is returned when the account status is disabled.
	ErrAccountDisabled = errors.New("account disabled")
	// ErrAccountNotFound is returned when the account does not exist.
	ErrAccountNotFound = errors.New("account not found")
	// ErrTokenInvalid is returned for malformed, forged, or expired tokens.
	ErrTokenInvalid = errors.New("invalid or expired token")
	// ErrTooManyAttempts is returned when the login rate limit trips.
	ErrTooManyAttempts = errors.New("too many login attempts")
	// ErrUsernameTaken is returned when creating an account with a duplicate username.
	ErrUsernameTaken = errors.New("username already taken")
)

// Account is a panel login (admin operator or customer portal user).
type Account struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Disabled reports whether the account is disabled.
func (a *Account) Disabled() bool { return a.Status == StatusDisabled }

// Claims is the authenticated identity carried by the access token.
// CustomerID is empty for pure admins without a linked customer row;
// for role=user it always comes from the token, never from URL/body.
type Claims struct {
	Subject    string
	Role       string
	CustomerID string
	ExpiresAt  time.Time
	IssuedAt   time.Time
}

// RefreshToken is a stored single-use refresh token (only its sha256 is kept).
type RefreshToken struct {
	ID        string
	AccountID string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// Revoked reports whether the token was rotated or revoked.
func (t *RefreshToken) Revoked() bool { return t.RevokedAt != nil }

// Expired reports whether the token passed its expiry at now.
func (t *RefreshToken) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

// PasswordReset is a single-use short-lived reset token (only sha256 kept).
type PasswordReset struct {
	ID        string
	AccountID string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// Usable reports whether the reset token can still be consumed at now.
func (r *PasswordReset) Usable(now time.Time) bool {
	return r.UsedAt == nil && now.Before(r.ExpiresAt)
}

// AccountRepository persists panel accounts. Implemented in infrastructure.
type AccountRepository interface {
	GetByUsername(ctx context.Context, username string) (*Account, error)
	GetByID(ctx context.Context, id string) (*Account, error)
	Create(ctx context.Context, a *Account) error
	UpdatePassword(ctx context.Context, id, hash string, now time.Time) error
	UpdateStatus(ctx context.Context, id, status string, now time.Time) error
	Count(ctx context.Context) (int64, error)
	// CustomerIDForAccount returns the linked customers.id, or "" when none.
	CustomerIDForAccount(ctx context.Context, accountID string) (string, error)
}

// RefreshTokenRepository persists refresh tokens. Implemented in infrastructure.
type RefreshTokenRepository interface {
	Create(ctx context.Context, t *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	Revoke(ctx context.Context, hash string, now time.Time) error
	RevokeAllForAccount(ctx context.Context, accountID string, now time.Time) error
}

// PasswordResetRepository persists reset tokens. Implemented in infrastructure.
type PasswordResetRepository interface {
	Create(ctx context.Context, r *PasswordReset) error
	GetByHash(ctx context.Context, hash string) (*PasswordReset, error)
	MarkUsed(ctx context.Context, id string, now time.Time) error
}

// Hasher hashes and verifies panel passwords (argon2id).
type Hasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) bool
}
