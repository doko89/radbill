// Package infrastructure implements the reports store on sqlc.
package infrastructure

import (
	"context"

	"boilerplate/api/internal/platform/db/sqlcgen"
	reportsdomain "boilerplate/api/internal/reports/domain"
)

// Store implements domain.Store.
type Store struct {
	q *sqlcgen.Queries
}

// NewStore builds a Store.
func NewStore(q *sqlcgen.Queries) *Store {
	return &Store{q: q}
}

// PaymentsInRange returns payments with paid_at in [from, to).
func (s *Store) PaymentsInRange(ctx context.Context, from, to string, limit int) ([]reportsdomain.Payment, error) {
	rows, err := s.q.ListG3PaymentsInRange(ctx, sqlcgen.ListG3PaymentsInRangeParams{
		PaidAt:   from,
		PaidAt_2: to,
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.Payment, 0, len(rows))
	for _, row := range rows {
		out = append(out, reportsdomain.Payment{Amount: row.Amount, PaidAt: row.PaidAt})
	}
	return out, nil
}

// SessionsInRange returns sessions with started_at in [from, to).
func (s *Store) SessionsInRange(ctx context.Context, from, to string, limit int) ([]reportsdomain.Session, error) {
	rows, err := s.q.ListG3SessionsInRange(ctx, sqlcgen.ListG3SessionsInRangeParams{
		StartedAt:   from,
		StartedAt_2: to,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, reportsdomain.Session{
			StartedAt: row.StartedAt, BytesIn: row.BytesIn, BytesOut: row.BytesOut,
		})
	}
	return out, nil
}

// UnpaidInvoices returns every unpaid invoice with its owner name.
func (s *Store) UnpaidInvoices(ctx context.Context, limit int) ([]reportsdomain.UnpaidInvoice, error) {
	rows, err := s.q.ListG3UnpaidInvoices(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.UnpaidInvoice, 0, len(rows))
	for _, row := range rows {
		out = append(out, reportsdomain.UnpaidInvoice{
			ID: row.ID, Number: row.Number, CustomerName: row.CustomerName,
			Total: row.Total, DueDate: row.DueDate,
		})
	}
	return out, nil
}

// AccountStatuses pages every PPPoE account status slice.
func (s *Store) AccountStatuses(ctx context.Context, limit, offset int) ([]reportsdomain.AccountStatus, error) {
	rows, err := s.q.ListG3PPPoEStatus(ctx, sqlcgen.ListG3PPPoEStatusParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.AccountStatus, 0, len(rows))
	for _, row := range rows {
		out = append(out, reportsdomain.AccountStatus{
			Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}
