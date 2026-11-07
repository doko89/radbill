// Package infrastructure implements scheduler ports on sqlc and the G1
// billing/pppoe/settings contexts.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"boilerplate/api/internal/platform/db/sqlcgen"
	scheddomain "boilerplate/api/internal/scheduler/domain"
)

// Store implements domain.Store on sqlc queries.
type Store struct {
	q *sqlcgen.Queries
}

// NewStore builds a Store.
func NewStore(q *sqlcgen.Queries) *Store {
	return &Store{q: q}
}

// ListBillable returns active/isolated accounts ordered by id.
func (s *Store) ListBillable(ctx context.Context, limit, offset int) ([]scheddomain.BillableAccount, error) {
	rows, err := s.q.ListBillablePPPoE(ctx, sqlcgen.ListBillablePPPoEParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]scheddomain.BillableAccount, 0, len(rows))
	for _, row := range rows {
		acct, err := toAccount(row)
		if err != nil {
			return nil, err
		}
		out = append(out, acct)
	}
	return out, nil
}

// GetAccount returns one account.
func (s *Store) GetAccount(ctx context.Context, id string) (scheddomain.BillableAccount, error) {
	row, err := s.q.GetBillingPPPoE(ctx, id)
	if err != nil {
		return scheddomain.BillableAccount{}, err
	}
	return toAccount(row)
}

// GetPackage returns the package slice needed for invoicing.
func (s *Store) GetPackage(ctx context.Context, id string) (scheddomain.PackageInfo, error) {
	row, err := s.q.GetBillingPackage(ctx, id)
	if err != nil {
		return scheddomain.PackageInfo{}, err
	}
	return scheddomain.PackageInfo{ID: row.ID, Name: row.Name, Price: row.Price}, nil
}

// GetCustomer returns the customer slice needed for notifications.
func (s *Store) GetCustomer(ctx context.Context, id string) (scheddomain.CustomerInfo, error) {
	row, err := s.q.GetBillingCustomer(ctx, id)
	if err != nil {
		return scheddomain.CustomerInfo{}, err
	}
	return scheddomain.CustomerInfo{ID: row.ID, Name: row.Name, Phone: row.Phone.String}, nil
}

// HasUnpaid reports whether the account has an unpaid invoice.
func (s *Store) HasUnpaid(ctx context.Context, pppoeID string) (bool, error) {
	_, err := s.q.GetUnpaidInvoiceByPPPoE(ctx, pppoeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// LatestIssuedAt returns the newest issue time, or nil when never invoiced.
func (s *Store) LatestIssuedAt(ctx context.Context, pppoeID string) (*time.Time, error) {
	row, err := s.q.GetLatestInvoiceByPPPoE(ctx, pppoeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	issued, err := time.Parse(time.RFC3339, row.IssuedAt)
	if err != nil {
		return nil, fmt.Errorf("parse invoice issued_at: %w", err)
	}
	return &issued, nil
}

// ListOverdue returns unpaid invoices due before the given date, oldest first.
func (s *Store) ListOverdue(ctx context.Context, beforeDate string, limit, offset int) ([]scheddomain.InvoiceInfo, error) {
	rows, err := s.q.ListOverdueUnpaidInvoices(ctx, sqlcgen.ListOverdueUnpaidInvoicesParams{
		DueDate: beforeDate,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]scheddomain.InvoiceInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheddomain.InvoiceInfo{
			ID: row.ID, Number: row.Number, PPPoEAccountID: row.PppoeAccountID,
			CustomerID: row.CustomerID, Total: row.Total, DueDate: row.DueDate,
		})
	}
	return out, nil
}

// RemindedSince reports whether a reminder was logged for the invoice at or
// after the given RFC3339 timestamp.
func (s *Store) RemindedSince(ctx context.Context, invoiceID, since string) (bool, error) {
	n, err := s.q.CountNotificationLogSince(ctx, sqlcgen.CountNotificationLogSinceParams{
		Kind:      "reminder",
		InvoiceID: sql.NullString{String: invoiceID, Valid: true},
		CreatedAt: since,
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func toAccount(row sqlcgen.PppoeAccount) (scheddomain.BillableAccount, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return scheddomain.BillableAccount{}, fmt.Errorf("parse pppoe created_at: %w", err)
	}
	return scheddomain.BillableAccount{
		ID: row.ID, CustomerID: row.CustomerID, PackageID: row.PackageID,
		Username: row.Username, Status: row.Status, CreatedAt: created,
	}, nil
}
