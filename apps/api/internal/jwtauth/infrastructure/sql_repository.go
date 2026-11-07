package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// SQLAccountRepository implements domain.AccountRepository on sqlc queries.
type SQLAccountRepository struct {
	q *sqlcgen.Queries
}

// NewSQLAccountRepository builds a SQLAccountRepository.
func NewSQLAccountRepository(q *sqlcgen.Queries) *SQLAccountRepository {
	return &SQLAccountRepository{q: q}
}

// GetByUsername returns the account or domain.ErrAccountNotFound.
func (r *SQLAccountRepository) GetByUsername(ctx context.Context, username string) (*authdomain.Account, error) {
	row, err := r.q.GetBillingAccountByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, authdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccount(row)
}

// GetByID returns the account or domain.ErrAccountNotFound.
func (r *SQLAccountRepository) GetByID(ctx context.Context, id string) (*authdomain.Account, error) {
	row, err := r.q.GetBillingAccountByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, authdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccount(row)
}

// Create inserts an account; duplicate usernames map to ErrUsernameTaken.
func (r *SQLAccountRepository) Create(ctx context.Context, a *authdomain.Account) error {
	_, err := r.q.CreateBillingAccount(ctx, sqlcgen.CreateBillingAccountParams{
		ID:           a.ID,
		Username:     a.Username,
		PasswordHash: a.PasswordHash,
		Role:         a.Role,
		Status:       a.Status,
		CreatedAt:    a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    a.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil && isUniqueViolation(err) {
		return authdomain.ErrUsernameTaken
	}
	return err
}

// UpdatePassword replaces the password hash.
func (r *SQLAccountRepository) UpdatePassword(ctx context.Context, id, hash string, now time.Time) error {
	return r.q.UpdateBillingAccountPassword(ctx, sqlcgen.UpdateBillingAccountPasswordParams{
		ID:           id,
		PasswordHash: hash,
		UpdatedAt:    now.UTC().Format(time.RFC3339),
	})
}

// UpdateStatus replaces the account status.
func (r *SQLAccountRepository) UpdateStatus(ctx context.Context, id, status string, now time.Time) error {
	return r.q.UpdateBillingAccountStatus(ctx, sqlcgen.UpdateBillingAccountStatusParams{
		ID:        id,
		Status:    status,
		UpdatedAt: now.UTC().Format(time.RFC3339),
	})
}

// Count returns the number of accounts.
func (r *SQLAccountRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountBillingAccounts(ctx)
}

// CustomerIDForAccount returns the linked customer id or "" when none.
func (r *SQLAccountRepository) CustomerIDForAccount(ctx context.Context, accountID string) (string, error) {
	row, err := r.q.GetBillingCustomerByAccountID(ctx, sqlNullString(accountID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return row.ID, nil
}

// SQLRefreshTokenRepository implements domain.RefreshTokenRepository on sqlc.
type SQLRefreshTokenRepository struct {
	q *sqlcgen.Queries
}

// NewSQLRefreshTokenRepository builds a SQLRefreshTokenRepository.
func NewSQLRefreshTokenRepository(q *sqlcgen.Queries) *SQLRefreshTokenRepository {
	return &SQLRefreshTokenRepository{q: q}
}

// Create inserts a refresh token.
func (r *SQLRefreshTokenRepository) Create(ctx context.Context, t *authdomain.RefreshToken) error {
	_, err := r.q.CreateBillingRefreshToken(ctx, sqlcgen.CreateBillingRefreshTokenParams{
		ID:        t.ID,
		AccountID: t.AccountID,
		TokenHash: t.TokenHash,
		ExpiresAt: t.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// GetByHash returns the token or domain.ErrTokenInvalid when missing.
func (r *SQLRefreshTokenRepository) GetByHash(ctx context.Context, hash string) (*authdomain.RefreshToken, error) {
	row, err := r.q.GetBillingRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, authdomain.ErrTokenInvalid
		}
		return nil, err
	}
	return toRefreshToken(row)
}

// Revoke marks the token revoked.
func (r *SQLRefreshTokenRepository) Revoke(ctx context.Context, hash string, now time.Time) error {
	return r.q.RevokeBillingRefreshToken(ctx, sqlcgen.RevokeBillingRefreshTokenParams{
		TokenHash: hash,
		RevokedAt: sqlNullString(now.UTC().Format(time.RFC3339)),
	})
}

// RevokeAllForAccount revokes every active token of the account.
func (r *SQLRefreshTokenRepository) RevokeAllForAccount(ctx context.Context, accountID string, now time.Time) error {
	return r.q.RevokeBillingRefreshTokensForAccount(ctx, sqlcgen.RevokeBillingRefreshTokensForAccountParams{
		AccountID: accountID,
		RevokedAt: sqlNullString(now.UTC().Format(time.RFC3339)),
	})
}

// SQLPasswordResetRepository implements domain.PasswordResetRepository on sqlc.
type SQLPasswordResetRepository struct {
	q *sqlcgen.Queries
}

// NewSQLPasswordResetRepository builds a SQLPasswordResetRepository.
func NewSQLPasswordResetRepository(q *sqlcgen.Queries) *SQLPasswordResetRepository {
	return &SQLPasswordResetRepository{q: q}
}

// Create inserts a reset token.
func (r *SQLPasswordResetRepository) Create(ctx context.Context, pr *authdomain.PasswordReset) error {
	_, err := r.q.CreateBillingPasswordReset(ctx, sqlcgen.CreateBillingPasswordResetParams{
		ID:        pr.ID,
		AccountID: pr.AccountID,
		TokenHash: pr.TokenHash,
		ExpiresAt: pr.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt: pr.CreatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// GetByHash returns the reset token or domain.ErrTokenInvalid when missing.
func (r *SQLPasswordResetRepository) GetByHash(ctx context.Context, hash string) (*authdomain.PasswordReset, error) {
	row, err := r.q.GetBillingPasswordReset(ctx, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, authdomain.ErrTokenInvalid
		}
		return nil, err
	}
	return toPasswordReset(row)
}

// MarkUsed marks the reset token consumed.
func (r *SQLPasswordResetRepository) MarkUsed(ctx context.Context, id string, now time.Time) error {
	return r.q.MarkBillingPasswordResetUsed(ctx, sqlcgen.MarkBillingPasswordResetUsedParams{
		ID:     id,
		UsedAt: sqlNullString(now.UTC().Format(time.RFC3339)),
	})
}

func toAccount(row sqlcgen.Account) (*authdomain.Account, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse account created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse account updated_at: %w", err)
	}
	return &authdomain.Account{
		ID:           row.ID,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
		Status:       row.Status,
		CreatedAt:    created,
		UpdatedAt:    updated,
	}, nil
}

func toRefreshToken(row sqlcgen.RefreshToken) (*authdomain.RefreshToken, error) {
	expires, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse refresh expires_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse refresh created_at: %w", err)
	}
	t := &authdomain.RefreshToken{
		ID:        row.ID,
		AccountID: row.AccountID,
		TokenHash: row.TokenHash,
		ExpiresAt: expires,
		CreatedAt: created,
	}
	if row.RevokedAt.Valid {
		revoked, err := time.Parse(time.RFC3339, row.RevokedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse refresh revoked_at: %w", err)
		}
		t.RevokedAt = &revoked
	}
	return t, nil
}

func toPasswordReset(row sqlcgen.PasswordReset) (*authdomain.PasswordReset, error) {
	expires, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse reset expires_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse reset created_at: %w", err)
	}
	r := &authdomain.PasswordReset{
		ID:        row.ID,
		AccountID: row.AccountID,
		TokenHash: row.TokenHash,
		ExpiresAt: expires,
		CreatedAt: created,
	}
	if row.UsedAt.Valid {
		used, err := time.Parse(time.RFC3339, row.UsedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse reset used_at: %w", err)
		}
		r.UsedAt = &used
	}
	return r, nil
}

func sqlNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
