// Package infrastructure implements nas ports on SQL.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"boilerplate/api/internal/nas/domain"
	"boilerplate/api/internal/network/notify"
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

// Create inserts a NAS and emits a nas/upsert event.
func (r *SQLRepository) Create(ctx context.Context, n *domain.NAS) error {
	_, err := r.q.CreateBillingNAS(ctx, sqlcgen.CreateBillingNASParams{
		ID:        n.ID,
		Name:      n.Name,
		Ip:        n.IP,
		Secret:    n.Secret,
		CoaPort:   int32(n.CoAPort),
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrNASIPTaken
		}
		return err
	}
	r.events.Emit(ctx, notify.TypeNAS, notify.ActionUpsert, n.ID)
	return nil
}

// Get returns one NAS or domain.ErrNASNotFound.
func (r *SQLRepository) Get(ctx context.Context, nasID string) (*domain.NAS, error) {
	row, err := r.q.GetBillingNAS(ctx, nasID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNASNotFound
		}
		return nil, err
	}
	return toNAS(row)
}

// List returns a page of NAS rows, newest first.
func (r *SQLRepository) List(ctx context.Context, limit, offset int) ([]*domain.NAS, error) {
	rows, err := r.q.ListBillingNAS(ctx, sqlcgen.ListBillingNASParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.NAS, 0, len(rows))
	for _, row := range rows {
		n, err := toNAS(row)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// Count returns the number of NAS rows.
func (r *SQLRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountBillingNAS(ctx)
}

// Update persists a NAS and emits a nas/upsert event.
func (r *SQLRepository) Update(ctx context.Context, n *domain.NAS) error {
	_, err := r.q.UpdateBillingNAS(ctx, sqlcgen.UpdateBillingNASParams{
		ID:        n.ID,
		Name:      n.Name,
		Ip:        n.IP,
		Secret:    n.Secret,
		CoaPort:   int32(n.CoAPort),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNASNotFound
		}
		if isUniqueViolation(err) {
			return domain.ErrNASIPTaken
		}
		return err
	}
	r.events.Emit(ctx, notify.TypeNAS, notify.ActionUpsert, n.ID)
	return nil
}

// Delete removes a NAS and emits a nas/delete event.
func (r *SQLRepository) Delete(ctx context.Context, nasID string) error {
	if err := r.q.DeleteBillingNAS(ctx, nasID); err != nil {
		if isForeignKeyViolation(err) {
			return domain.ErrNASInUse
		}
		return err
	}
	r.events.Emit(ctx, notify.TypeNAS, notify.ActionDelete, nasID)
	return nil
}

func toNAS(row sqlcgen.Na) (*domain.NAS, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse nas created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse nas updated_at: %w", err)
	}
	return &domain.NAS{
		ID:        row.ID,
		Name:      row.Name,
		IP:        row.Ip,
		Secret:    row.Secret,
		CoAPort:   int(row.CoaPort),
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
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
