// Package infrastructure implements session ports on SQL.
package infrastructure

import (
	"context"
	"fmt"
	"time"

	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/sessions/domain"
)

// SQLRepository implements domain.Repository on sqlc queries.
type SQLRepository struct {
	q *sqlcgen.Queries
}

// NewSQLRepository builds a SQLRepository.
func NewSQLRepository(q *sqlcgen.Queries) *SQLRepository {
	return &SQLRepository{q: q}
}

// ListOnline returns open sessions, newest first.
func (r *SQLRepository) ListOnline(ctx context.Context, limit, offset int) ([]*domain.Session, error) {
	rows, err := r.q.ListBillingOnlineSessions(ctx, sqlcgen.ListBillingOnlineSessionsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	return toSessions(rows)
}

// CountOnline returns the number of open sessions.
func (r *SQLRepository) CountOnline(ctx context.Context) (int64, error) {
	return r.q.CountBillingOnlineSessions(ctx)
}

// ListHistory returns all sessions, newest first.
func (r *SQLRepository) ListHistory(ctx context.Context, limit, offset int) ([]*domain.Session, error) {
	rows, err := r.q.ListBillingSessionHistory(ctx, sqlcgen.ListBillingSessionHistoryParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	return toSessions(rows)
}

// CountHistory returns the number of sessions.
func (r *SQLRepository) CountHistory(ctx context.Context) (int64, error) {
	return r.q.CountBillingSessions(ctx)
}

func toSessions(rows []sqlcgen.RadiusSession) ([]*domain.Session, error) {
	out := make([]*domain.Session, 0, len(rows))
	for _, row := range rows {
		s, err := toSession(row)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func toSession(row sqlcgen.RadiusSession) (*domain.Session, error) {
	started, err := time.Parse(time.RFC3339, row.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse session started_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse session updated_at: %w", err)
	}
	s := &domain.Session{
		ID:             row.ID,
		SessionID:      row.SessionID,
		PPPoEAccountID: row.PppoeAccountID,
		NASID:          row.NasID,
		FramedIP:       row.FramedIp.String,
		StartedAt:      started,
		BytesIn:        row.BytesIn,
		BytesOut:       row.BytesOut,
		TerminateCause: row.TerminateCause.String,
		UpdatedAt:      updated,
	}
	if row.StoppedAt.Valid {
		stopped, err := time.Parse(time.RFC3339, row.StoppedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse session stopped_at: %w", err)
		}
		s.StoppedAt = &stopped
	}
	return s, nil
}
