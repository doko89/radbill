package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	notifydomain "boilerplate/api/internal/notify/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// SQLLogRepository implements domain.LogRepository on sqlc queries.
type SQLLogRepository struct {
	q *sqlcgen.Queries
}

// NewSQLLogRepository builds a SQLLogRepository.
func NewSQLLogRepository(q *sqlcgen.Queries) *SQLLogRepository {
	return &SQLLogRepository{q: q}
}

// Create inserts one log row.
func (r *SQLLogRepository) Create(ctx context.Context, e notifydomain.Entry) (notifydomain.Entry, error) {
	row, err := r.q.CreateNotificationLog(ctx, sqlcgen.CreateNotificationLogParams{
		ID:             e.ID,
		Kind:           e.Kind,
		Target:         e.Target,
		PppoeAccountID: nullString(e.PPPoEAccountID),
		InvoiceID:      nullString(e.InvoiceID),
		Status:         e.Status,
		Error:          nullString(e.Error),
		CreatedAt:      e.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return notifydomain.Entry{}, err
	}
	return toEntry(row)
}

func toEntry(row sqlcgen.NotificationLog) (notifydomain.Entry, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return notifydomain.Entry{}, fmt.Errorf("parse notification_log created_at: %w", err)
	}
	return notifydomain.Entry{
		ID: row.ID, Kind: row.Kind, Target: row.Target,
		PPPoEAccountID: row.PppoeAccountID.String, InvoiceID: row.InvoiceID.String,
		Status: row.Status, Error: row.Error.String, CreatedAt: created,
	}, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
