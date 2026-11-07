// Package infrastructure implements customer ports on SQL.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"boilerplate/api/internal/customers/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// SQLRepository implements domain.Repository on sqlc queries.
type SQLRepository struct {
	q *sqlcgen.Queries
}

// NewSQLRepository builds a SQLRepository.
func NewSQLRepository(q *sqlcgen.Queries) *SQLRepository {
	return &SQLRepository{q: q}
}

// Create inserts a customer.
func (r *SQLRepository) Create(ctx context.Context, c *domain.Customer) error {
	_, err := r.q.CreateBillingCustomer(ctx, sqlcgen.CreateBillingCustomerParams{
		ID:        c.ID,
		AccountID: nullString(c.AccountID),
		Name:      c.Name,
		Phone:     nullString(c.Phone),
		Address:   nullString(c.Address),
		Notes:     nullString(c.Notes),
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// Get returns one customer or domain.ErrCustomerNotFound.
func (r *SQLRepository) Get(ctx context.Context, cid string) (*domain.Customer, error) {
	row, err := r.q.GetBillingCustomer(ctx, cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrCustomerNotFound
		}
		return nil, err
	}
	return toCustomer(row)
}

// List returns a page of customers, newest first.
func (r *SQLRepository) List(ctx context.Context, limit, offset int) ([]*domain.Customer, error) {
	rows, err := r.q.ListBillingCustomers(ctx, sqlcgen.ListBillingCustomersParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Customer, 0, len(rows))
	for _, row := range rows {
		c, err := toCustomer(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Count returns the number of customers.
func (r *SQLRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountBillingCustomers(ctx)
}

// Update persists customer details or returns ErrCustomerNotFound.
func (r *SQLRepository) Update(ctx context.Context, c *domain.Customer) error {
	_, err := r.q.UpdateBillingCustomer(ctx, sqlcgen.UpdateBillingCustomerParams{
		ID:        c.ID,
		AccountID: nullString(c.AccountID),
		Name:      c.Name,
		Phone:     nullString(c.Phone),
		Address:   nullString(c.Address),
		Notes:     nullString(c.Notes),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrCustomerNotFound
		}
		return err
	}
	return nil
}

// Delete removes a customer; referenced rows map to ErrCustomerInUse.
func (r *SQLRepository) Delete(ctx context.Context, cid string) error {
	if err := r.q.DeleteBillingCustomer(ctx, cid); err != nil {
		if isForeignKeyViolation(err) {
			return domain.ErrCustomerInUse
		}
		return err
	}
	return nil
}

func toCustomer(row sqlcgen.Customer) (*domain.Customer, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse customer created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse customer updated_at: %w", err)
	}
	return &domain.Customer{
		ID:        row.ID,
		AccountID: row.AccountID.String,
		Name:      row.Name,
		Phone:     row.Phone.String,
		Address:   row.Address.String,
		Notes:     row.Notes.String,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
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
