package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"boilerplate/api/internal/platform/db/sqlcgen"
	setdomain "boilerplate/api/internal/settings/domain"
)

// SQLRepository implements domain.Repository on sqlc queries.
type SQLRepository struct {
	q *sqlcgen.Queries
}

// NewSQLRepository builds a SQLRepository.
func NewSQLRepository(q *sqlcgen.Queries) *SQLRepository {
	return &SQLRepository{q: q}
}

// Get returns one setting or domain.ErrSettingNotFound.
func (r *SQLRepository) Get(ctx context.Context, key string) (setdomain.Setting, error) {
	row, err := r.q.GetBillingSetting(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return setdomain.Setting{}, setdomain.ErrSettingNotFound
		}
		return setdomain.Setting{}, err
	}
	return toSetting(row)
}

// List returns every setting ordered by key.
func (r *SQLRepository) List(ctx context.Context) ([]setdomain.Setting, error) {
	rows, err := r.q.ListBillingSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]setdomain.Setting, 0, len(rows))
	for _, row := range rows {
		s, err := toSetting(row)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Upsert inserts or replaces one setting.
func (r *SQLRepository) Upsert(ctx context.Context, key, value string, now time.Time) (setdomain.Setting, error) {
	row, err := r.q.UpsertBillingSetting(ctx, sqlcgen.UpsertBillingSettingParams{
		Key:       key,
		Value:     value,
		UpdatedAt: now.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return setdomain.Setting{}, err
	}
	return toSetting(row)
}

func toSetting(row sqlcgen.Setting) (setdomain.Setting, error) {
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return setdomain.Setting{}, err
	}
	return setdomain.Setting{Key: row.Key, Value: row.Value, UpdatedAt: updated}, nil
}
