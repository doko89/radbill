// Package infrastructure implements the audit store on sqlc.
package infrastructure

import (
	"context"
	"database/sql"
	"time"

	auditdomain "boilerplate/api/internal/audit/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// endOfTime is the open-ended upper bound for audit listing (the G4
// list/count queries use inclusive <=, so an empty ?to maps here).
const endOfTime = "9999-12-31T23:59:59Z"

// Store implements domain.Store.
type Store struct {
	q *sqlcgen.Queries
}

// NewStore builds a Store.
func NewStore(q *sqlcgen.Queries) *Store {
	return &Store{q: q}
}

// Insert persists one entry. Empty ActorID is stored as NULL.
func (s *Store) Insert(ctx context.Context, e *auditdomain.Entry) error {
	_, err := s.q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		ID:            e.ID,
		ActorID:       sql.NullString{String: e.ActorID, Valid: e.ActorID != ""},
		ActorUsername: e.ActorUsername,
		Action:        e.Action,
		TargetType:    e.TargetType,
		TargetID:      e.TargetID,
		Detail:        e.Detail,
		Ip:            e.IP,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// List returns filtered entries, newest first, with the total count.
func (s *Store) List(ctx context.Context, f auditdomain.Filter) ([]auditdomain.Entry, int64, error) {
	to := f.To
	if to == "" {
		to = endOfTime
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	// Valid is always true here: the G4 filter matches an empty actor
	// or action via LIKE '%%', but LIKE with a NULL pattern matches
	// nothing, so a NULL param would wrongly return zero rows.
	// (Column2 is sqlc's inferred name for the action LIKE operand.)
	rows, err := s.q.ListAuditLogs(ctx, sqlcgen.ListAuditLogsParams{
		ActorID:     sql.NullString{String: f.Actor, Valid: true},
		Column2:     sql.NullString{String: f.Action, Valid: true},
		CreatedAt:   f.From,
		CreatedAt_2: to,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.q.CountAuditLogs(ctx, sqlcgen.CountAuditLogsParams{
		ActorID:     sql.NullString{String: f.Actor, Valid: true},
		Column2:     sql.NullString{String: f.Action, Valid: true},
		CreatedAt:   f.From,
		CreatedAt_2: to,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]auditdomain.Entry, 0, len(rows))
	for _, row := range rows {
		createdAt, _ := time.Parse(time.RFC3339, row.CreatedAt)
		out = append(out, auditdomain.Entry{
			ID:            row.ID,
			ActorID:       row.ActorID.String,
			ActorUsername: row.ActorUsername,
			Action:        row.Action,
			TargetType:    row.TargetType,
			TargetID:      row.TargetID,
			Detail:        row.Detail,
			IP:            row.Ip,
			CreatedAt:     createdAt.UTC(),
		})
	}
	return out, total, nil
}

// UsernameForAccount resolves the display username. Best-effort: the
// service ignores any error (unknown accounts keep "").
func (s *Store) UsernameForAccount(ctx context.Context, accountID string) (string, error) {
	acct, err := s.q.GetBillingAccountByID(ctx, accountID)
	if err != nil {
		return "", err
	}
	return acct.Username, nil
}
