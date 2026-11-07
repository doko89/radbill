package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"boilerplate/api/internal/auth/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// SQLUserRepository implements domain.Repository on sqlc queries.
// It runs on both postgres and sqlite (compatible SQL subset).
type SQLUserRepository struct {
	q *sqlcgen.Queries
}

// NewSQLUserRepository builds a SQLUserRepository.
func NewSQLUserRepository(q *sqlcgen.Queries) *SQLUserRepository {
	return &SQLUserRepository{q: q}
}

// FindByUsername returns the user or domain.ErrUserNotFound.
func (r *SQLUserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	row, err := r.q.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return toUser(row)
}

// FindByID returns the user or domain.ErrUserNotFound.
func (r *SQLUserRepository) FindByID(ctx context.Context, id string) (*domain.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return toUser(row)
}

// Create inserts a user or returns domain.ErrUsernameTaken on conflict.
func (r *SQLUserRepository) Create(ctx context.Context, u *domain.User) error {
	_, err := r.q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:           u.ID,
		Username:     u.Username,
		PasswordHash: u.PasswordHash,
		CreatedAt:    u.CreatedAt.UTC().Format(time.RFC3339),
		AvatarUrl:    u.AvatarURL,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrUsernameTaken
		}
		return err
	}
	return nil
}

// Count returns the total number of users.
func (r *SQLUserRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountUsers(ctx)
}

// FindUserByOIDC returns the user linked to the identity or domain.ErrOIDCNotFound.
func (r *SQLUserRepository) FindUserByOIDC(ctx context.Context, issuer, sub string) (*domain.User, error) {
	link, err := r.q.GetOIDCLink(ctx, sqlcgen.GetOIDCLinkParams{Issuer: issuer, Sub: sub})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrOIDCNotFound
		}
		return nil, err
	}
	return r.FindByID(ctx, link.UserID)
}

// SaveOIDCLink persists the user-to-identity binding.
func (r *SQLUserRepository) SaveOIDCLink(ctx context.Context, link *domain.OIDCLink) error {
	return r.q.CreateOIDCLink(ctx, sqlcgen.CreateOIDCLinkParams{
		UserID:    link.UserID,
		Issuer:    link.Issuer,
		Sub:       link.Sub,
		CreatedAt: link.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func toUser(row sqlcgen.User) (*domain.User, error) {
	createdAt, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse user created_at: %w", err)
	}
	return &domain.User{
		ID:           row.ID,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		CreatedAt:    createdAt,
		AvatarURL:    row.AvatarUrl,
	}, nil
}

// SetAvatarURL updates the user's avatar URL.
func (r *SQLUserRepository) SetAvatarURL(ctx context.Context, userID, url string) error {
	return r.q.UpdateUserAvatar(ctx, sqlcgen.UpdateUserAvatarParams{AvatarUrl: url, ID: userID})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "UNIQUE constraint")
}
