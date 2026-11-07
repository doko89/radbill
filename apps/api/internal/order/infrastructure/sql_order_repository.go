// Package infrastructure implements order ports on SQL.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"boilerplate/api/internal/order/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

// SQLOrderRepository implements domain.Repository on sqlc queries.
// It runs on both postgres and sqlite (compatible SQL subset).
type SQLOrderRepository struct {
	q *sqlcgen.Queries
}

// NewSQLOrderRepository builds a SQLOrderRepository.
func NewSQLOrderRepository(q *sqlcgen.Queries) *SQLOrderRepository {
	return &SQLOrderRepository{q: q}
}

// Create inserts an order.
func (r *SQLOrderRepository) Create(ctx context.Context, o *domain.Order) error {
	_, err := r.q.CreateOrder(ctx, sqlcgen.CreateOrderParams{
		ID:        o.ID,
		UserID:    o.UserID,
		Item:      o.Item,
		Qty:       o.Qty,
		Status:    string(o.Status),
		CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// Get returns one order of the user or domain.ErrOrderNotFound.
func (r *SQLOrderRepository) Get(ctx context.Context, id, userID string) (*domain.Order, error) {
	row, err := r.q.GetOrder(ctx, sqlcgen.GetOrderParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, err
	}
	return toOrder(row)
}

// ListByUser returns the user's orders, newest first.
func (r *SQLOrderRepository) ListByUser(ctx context.Context, userID string) ([]*domain.Order, error) {
	rows, err := r.q.ListOrdersByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Order, 0, len(rows))
	for _, row := range rows {
		o, err := toOrder(row)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// Update persists the order status.
func (r *SQLOrderRepository) Update(ctx context.Context, o *domain.Order) error {
	_, err := r.q.UpdateOrderStatus(ctx, sqlcgen.UpdateOrderStatusParams{
		ID:     o.ID,
		UserID: o.UserID,
		Status: string(o.Status),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrOrderNotFound
		}
		return err
	}
	return nil
}

func toOrder(row sqlcgen.Order) (*domain.Order, error) {
	createdAt, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse order created_at: %w", err)
	}
	return &domain.Order{
		ID:        row.ID,
		UserID:    row.UserID,
		Item:      row.Item,
		Qty:       row.Qty,
		Status:    domain.Status(row.Status),
		CreatedAt: createdAt,
	}, nil
}
