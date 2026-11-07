package domain

import "context"

// Repository persists orders scoped per user. Implemented in infrastructure.
type Repository interface {
	Create(ctx context.Context, o *Order) error
	Get(ctx context.Context, id, userID string) (*Order, error)
	ListByUser(ctx context.Context, userID string) ([]*Order, error)
	Update(ctx context.Context, o *Order) error
}
