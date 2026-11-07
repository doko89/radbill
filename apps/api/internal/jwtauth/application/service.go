// Package application hosts jwtauth use cases: login, refresh, logout,
// profile, and password reset.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/shared/apperr"
	"boilerplate/api/internal/shared/id"
)

// ResetTokenTTL is the lifetime of a password-reset token.
const ResetTokenTTL = 30 * time.Minute

// MinPasswordLen is the minimum panel password length.
const MinPasswordLen = 8

// refreshBytes is 256 bits for refresh and reset tokens.
const refreshBytes = 32

// UserDTO is the public shape of an authenticated panel user.
type UserDTO struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	CustomerID  *string  `json:"customer_id"`
	Permissions []string `json:"permissions"`
}

// SessionDTO is the login/refresh result: a short-lived access token plus
// the user profile. The refresh token travels in an httpOnly cookie.
type SessionDTO struct {
	AccessToken string  `json:"access_token"`
	ExpiresIn   int64   `json:"expires_in"`
	User        UserDTO `json:"user"`
}

// Service implements panel authentication use cases.
type Service struct {
	accounts   authdomain.AccountRepository
	refresh    authdomain.RefreshTokenRepository
	resets     authdomain.PasswordResetRepository
	hasher     authdomain.Hasher
	limiter    *LoginLimiter
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

// NewService builds a Service.
func NewService(
	accounts authdomain.AccountRepository,
	refresh authdomain.RefreshTokenRepository,
	resets authdomain.PasswordResetRepository,
	hasher authdomain.Hasher,
	jwtSecret string,
	accessTTL, refreshTTL time.Duration,
) *Service {
	return &Service{
		accounts:   accounts,
		refresh:    refresh,
		resets:     resets,
		hasher:     hasher,
		limiter:    NewLoginLimiter(5, 15*time.Minute),
		jwtSecret:  []byte(jwtSecret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		now:        time.Now,
	}
}

// Login verifies credentials and issues an access token plus a raw refresh
// token (the caller stores the refresh token in an httpOnly cookie).
func (s *Service) Login(ctx context.Context, username, password, ip string) (SessionDTO, string, error) {
	if username == "" {
		return SessionDTO{}, "", apperr.Invalid("username", "required")
	}
	if password == "" {
		return SessionDTO{}, "", apperr.Invalid("password", "required")
	}
	if err := s.limiter.Check(username, ip); err != nil {
		return SessionDTO{}, "", err
	}
	now := s.now().UTC()
	acct, err := s.accounts.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, authdomain.ErrAccountNotFound) {
			s.limiter.Fail(username, ip)
			return SessionDTO{}, "", authdomain.ErrInvalidCredentials
		}
		return SessionDTO{}, "", err
	}
	if acct.Disabled() {
		return SessionDTO{}, "", authdomain.ErrAccountDisabled
	}
	if !s.hasher.Verify(password, acct.PasswordHash) {
		s.limiter.Fail(username, ip)
		return SessionDTO{}, "", authdomain.ErrInvalidCredentials
	}
	s.limiter.Reset(username, ip)
	return s.issueSession(ctx, acct, now)
}

// Refresh rotates a refresh token and issues a new session.
// Reuse of a revoked token revokes the whole family (theft detection).
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (SessionDTO, string, error) {
	if rawRefresh == "" {
		return SessionDTO{}, "", authdomain.ErrTokenInvalid
	}
	now := s.now().UTC()
	stored, err := s.refresh.GetByHash(ctx, sha256Hex(rawRefresh))
	if err != nil {
		return SessionDTO{}, "", authdomain.ErrTokenInvalid
	}
	if stored.Revoked() {
		_ = s.refresh.RevokeAllForAccount(ctx, stored.AccountID, now)
		return SessionDTO{}, "", authdomain.ErrTokenInvalid
	}
	if stored.Expired(now) {
		return SessionDTO{}, "", authdomain.ErrTokenInvalid
	}
	acct, err := s.accounts.GetByID(ctx, stored.AccountID)
	if err != nil {
		return SessionDTO{}, "", authdomain.ErrTokenInvalid
	}
	if acct.Disabled() {
		_ = s.refresh.RevokeAllForAccount(ctx, acct.ID, now)
		return SessionDTO{}, "", authdomain.ErrAccountDisabled
	}
	if err := s.refresh.Revoke(ctx, stored.TokenHash, now); err != nil {
		return SessionDTO{}, "", err
	}
	return s.issueSession(ctx, acct, now)
}

// Logout revokes one refresh token. Unknown tokens are a no-op.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	if rawRefresh == "" {
		return nil
	}
	stored, err := s.refresh.GetByHash(ctx, sha256Hex(rawRefresh))
	if err != nil {
		return nil
	}
	return s.refresh.Revoke(ctx, stored.TokenHash, s.now().UTC())
}

// Me returns the profile for the access-token subject.
func (s *Service) Me(ctx context.Context, accountID string) (UserDTO, error) {
	acct, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return UserDTO{}, err
	}
	if acct.Disabled() {
		return UserDTO{}, authdomain.ErrAccountDisabled
	}
	return s.userDTO(ctx, acct)
}

// RequestPasswordReset creates a 30-minute reset token for username.
// Unknown or disabled accounts return an empty token without error
// (no account enumeration).
func (s *Service) RequestPasswordReset(ctx context.Context, username string) (string, error) {
	if username == "" {
		return "", apperr.Invalid("username", "required")
	}
	now := s.now().UTC()
	acct, err := s.accounts.GetByUsername(ctx, username)
	if err != nil || acct.Disabled() {
		return "", nil
	}
	raw, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.resets.Create(ctx, &authdomain.PasswordReset{
		ID:        id.New(),
		AccountID: acct.ID,
		TokenHash: sha256Hex(raw),
		ExpiresAt: now.Add(ResetTokenTTL),
		CreatedAt: now,
	}); err != nil {
		return "", err
	}
	return raw, nil
}

// ResetPassword consumes a reset token and sets a new password,
// revoking every refresh token of the account.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if rawToken == "" {
		return apperr.Invalid("token", "required")
	}
	if len(newPassword) < MinPasswordLen {
		return apperr.Invalid("new_password", "minimum 8 characters")
	}
	now := s.now().UTC()
	stored, err := s.resets.GetByHash(ctx, sha256Hex(rawToken))
	if err != nil || !stored.Usable(now) {
		return authdomain.ErrTokenInvalid
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.accounts.UpdatePassword(ctx, stored.AccountID, hash, now); err != nil {
		return err
	}
	if err := s.resets.MarkUsed(ctx, stored.ID, now); err != nil {
		return err
	}
	return s.refresh.RevokeAllForAccount(ctx, stored.AccountID, now)
}

// VerifyAccess verifies an access token and returns its claims.
func (s *Service) VerifyAccess(token string) (authdomain.Claims, error) {
	return authdomain.VerifyAccessToken(s.jwtSecret, token, s.now().UTC())
}

func (s *Service) issueSession(ctx context.Context, acct *authdomain.Account, now time.Time) (SessionDTO, string, error) {
	customerID, err := s.accounts.CustomerIDForAccount(ctx, acct.ID)
	if err != nil {
		return SessionDTO{}, "", err
	}
	access, err := authdomain.SignAccessToken(s.jwtSecret, authdomain.Claims{
		Subject:    acct.ID,
		Role:       acct.Role,
		CustomerID: customerID,
		ExpiresAt:  now.Add(s.accessTTL),
		IssuedAt:   now,
	})
	if err != nil {
		return SessionDTO{}, "", err
	}
	raw, err := randomToken()
	if err != nil {
		return SessionDTO{}, "", err
	}
	if err := s.refresh.Create(ctx, &authdomain.RefreshToken{
		ID:        id.New(),
		AccountID: acct.ID,
		TokenHash: sha256Hex(raw),
		ExpiresAt: now.Add(s.refreshTTL),
		CreatedAt: now,
	}); err != nil {
		return SessionDTO{}, "", err
	}
	user := UserDTO{
		ID:          acct.ID,
		Username:    acct.Username,
		Role:        acct.Role,
		Permissions: authdomain.PermissionsFor(acct.Role),
	}
	if customerID != "" {
		user.CustomerID = &customerID
	}
	return SessionDTO{AccessToken: access, ExpiresIn: int64(s.accessTTL / time.Second), User: user}, raw, nil
}

func (s *Service) userDTO(ctx context.Context, acct *authdomain.Account) (UserDTO, error) {
	customerID, err := s.accounts.CustomerIDForAccount(ctx, acct.ID)
	if err != nil {
		return UserDTO{}, err
	}
	user := UserDTO{
		ID:          acct.ID,
		Username:    acct.Username,
		Role:        acct.Role,
		Permissions: authdomain.PermissionsFor(acct.Role),
	}
	if customerID != "" {
		user.CustomerID = &customerID
	}
	return user, nil
}

func randomToken() (string, error) {
	var b [refreshBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
