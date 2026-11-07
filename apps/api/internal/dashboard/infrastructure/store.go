// Package infrastructure implements the dashboard store on sqlc.
package infrastructure

import (
	"context"

	dashdomain "boilerplate/api/internal/dashboard/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// Store implements domain.Store.
type Store struct {
	q *sqlcgen.Queries
}

// NewStore builds a Store.
func NewStore(q *sqlcgen.Queries) *Store {
	return &Store{q: q}
}

// PaymentsSince returns payment amounts with paid_at >= since.
func (s *Store) PaymentsSince(ctx context.Context, since string) ([]int64, error) {
	return s.q.ListG4PaymentsSince(ctx, since)
}

// UnpaidTotals returns every unpaid invoice total + due date.
func (s *Store) UnpaidTotals(ctx context.Context) ([]dashdomain.Unpaid, error) {
	rows, err := s.q.ListG4UnpaidTotals(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dashdomain.Unpaid, 0, len(rows))
	for _, row := range rows {
		out = append(out, dashdomain.Unpaid{Total: row.Total, DueDate: row.DueDate})
	}
	return out, nil
}

// PPPoECounts returns account counts keyed by status.
func (s *Store) PPPoECounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.q.ListG4PPPoECounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

// OnlineSessions counts sessions with stopped_at IS NULL.
func (s *Store) OnlineSessions(ctx context.Context) (int64, error) {
	return s.q.CountBillingOnlineSessions(ctx)
}

// RecentPayments returns the newest settled payments.
func (s *Store) RecentPayments(ctx context.Context, limit int) ([]dashdomain.RecentPayment, error) {
	if limit <= 0 {
		limit = dashdomain.RecentPaymentsLimit
	}
	rows, err := s.q.ListG4RecentPayments(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]dashdomain.RecentPayment, 0, len(rows))
	for _, row := range rows {
		out = append(out, dashdomain.RecentPayment{
			ID: row.ID, Number: row.Number,
			Amount: row.Amount, PaidAt: row.PaidAt,
		})
	}
	return out, nil
}

// PendingAttempts counts payment_attempts with status pending.
func (s *Store) PendingAttempts(ctx context.Context) (int64, error) {
	return s.q.CountG4PendingAttempts(ctx)
}

// FailedAttemptsSince counts attempts parked failed at/after since.
func (s *Store) FailedAttemptsSince(ctx context.Context, since string) (int64, error) {
	return s.q.CountG4FailedAttemptsSince(ctx, since)
}
