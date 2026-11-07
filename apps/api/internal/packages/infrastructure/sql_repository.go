// Package infrastructure implements package ports on SQL.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"boilerplate/api/internal/network/notify"
	"boilerplate/api/internal/packages/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// SQLRepository implements domain.Repository on sqlc queries.
type SQLRepository struct {
	q      *sqlcgen.Queries
	events *notify.Notifier
}

// NewSQLRepository builds a SQLRepository.
func NewSQLRepository(q *sqlcgen.Queries, events *notify.Notifier) *SQLRepository {
	return &SQLRepository{q: q, events: events}
}

// Create inserts a package and emits a package/upsert event.
func (r *SQLRepository) Create(ctx context.Context, p *domain.Package) error {
	_, err := r.q.CreateBillingPackage(ctx, sqlcgen.CreateBillingPackageParams{
		ID:                 p.ID,
		Name:               p.Name,
		Price:              p.Price,
		Period:             p.Period,
		RateLimit:          nullString(p.RateLimit),
		AddressPool:        nullString(p.AddressPool),
		ValiditySeconds:    nullInt64(p.ValiditySeconds),
		OnlineSecondsLimit: nullInt64(p.OnlineSecondsLimit),
		QuotaBytes:         nullInt64(p.QuotaBytes),
		QuotaReset:         p.QuotaReset,
		FupThresholdBytes:  nullInt64(p.FUPThresholdBytes),
		FupRateLimit:       nullString(p.FUPRateLimit),
		OnExhausted:        p.OnExhausted,
		IsPublic:           boolToInt(p.IsPublic),
		AllowSelfService:   boolToInt(p.AllowSelfService),
		UpgradeGroup:       nullString(p.UpgradeGroup),
		CreatedAt:          p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          p.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	r.events.Emit(ctx, notify.TypePackage, notify.ActionUpsert, p.ID)
	return nil
}

// Get returns one package or domain.ErrPackageNotFound.
func (r *SQLRepository) Get(ctx context.Context, pid string) (*domain.Package, error) {
	row, err := r.q.GetBillingPackage(ctx, pid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPackageNotFound
		}
		return nil, err
	}
	return toPackage(row)
}

// List returns a page of packages, newest first.
func (r *SQLRepository) List(ctx context.Context, limit, offset int) ([]*domain.Package, error) {
	rows, err := r.q.ListBillingPackages(ctx, sqlcgen.ListBillingPackagesParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Package, 0, len(rows))
	for _, row := range rows {
		p, err := toPackage(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Count returns the number of packages.
func (r *SQLRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountBillingPackages(ctx)
}

// Update persists a package and emits a package/upsert event.
func (r *SQLRepository) Update(ctx context.Context, p *domain.Package) error {
	_, err := r.q.UpdateBillingPackage(ctx, sqlcgen.UpdateBillingPackageParams{
		ID:                 p.ID,
		Name:               p.Name,
		Price:              p.Price,
		Period:             p.Period,
		RateLimit:          nullString(p.RateLimit),
		AddressPool:        nullString(p.AddressPool),
		ValiditySeconds:    nullInt64(p.ValiditySeconds),
		OnlineSecondsLimit: nullInt64(p.OnlineSecondsLimit),
		QuotaBytes:         nullInt64(p.QuotaBytes),
		QuotaReset:         p.QuotaReset,
		FupThresholdBytes:  nullInt64(p.FUPThresholdBytes),
		FupRateLimit:       nullString(p.FUPRateLimit),
		OnExhausted:        p.OnExhausted,
		IsPublic:           boolToInt(p.IsPublic),
		AllowSelfService:   boolToInt(p.AllowSelfService),
		UpgradeGroup:       nullString(p.UpgradeGroup),
		UpdatedAt:          p.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrPackageNotFound
		}
		return err
	}
	r.events.Emit(ctx, notify.TypePackage, notify.ActionUpsert, p.ID)
	return nil
}

// Delete removes a package and emits a package/delete event.
func (r *SQLRepository) Delete(ctx context.Context, pid string) error {
	if err := r.q.DeleteBillingPackage(ctx, pid); err != nil {
		if isForeignKeyViolation(err) {
			return domain.ErrPackageInUse
		}
		return err
	}
	r.events.Emit(ctx, notify.TypePackage, notify.ActionDelete, pid)
	return nil
}

func toPackage(row sqlcgen.Package) (*domain.Package, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse package created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse package updated_at: %w", err)
	}
	return &domain.Package{
		ID:                 row.ID,
		Name:               row.Name,
		Price:              row.Price,
		Period:             row.Period,
		RateLimit:          row.RateLimit.String,
		AddressPool:        row.AddressPool.String,
		ValiditySeconds:    nullToInt64Ptr(row.ValiditySeconds),
		OnlineSecondsLimit: nullToInt64Ptr(row.OnlineSecondsLimit),
		QuotaBytes:         nullToInt64Ptr(row.QuotaBytes),
		QuotaReset:         row.QuotaReset,
		FUPThresholdBytes:  nullToInt64Ptr(row.FupThresholdBytes),
		FUPRateLimit:       row.FupRateLimit.String,
		OnExhausted:        row.OnExhausted,
		IsPublic:           row.IsPublic != 0,
		AllowSelfService:   row.AllowSelfService != 0,
		UpgradeGroup:       row.UpgradeGroup.String,
		CreatedAt:          created,
		UpdatedAt:          updated,
	}, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullToInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func boolToInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FOREIGN KEY constraint failed") ||
		strings.Contains(msg, "foreign key constraint") ||
		strings.Contains(msg, "violates foreign key")
}
