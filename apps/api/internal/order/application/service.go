// Package application hosts order use cases.
package application

import (
	"context"
	"time"

	"boilerplate/api/internal/order/domain"
	"boilerplate/api/internal/shared/apperr"
	"boilerplate/api/internal/shared/id"
)

// OrderDTO is the public shape of an order.
type OrderDTO struct {
	ID        string `json:"id"`
	Item      string `json:"item"`
	Qty       int64  `json:"qty"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// CreateInput carries raw create-order fields.
type CreateInput struct {
	UserID string
	Item   string
	Qty    int64
}

// Service implements order use cases.
type Service struct {
	orders domain.Repository
	now    func() time.Time
}

// NewService builds a Service.
func NewService(orders domain.Repository) *Service {
	return &Service{orders: orders, now: time.Now}
}

// Create validates and persists a new open order.
func (s *Service) Create(ctx context.Context, in CreateInput) (OrderDTO, error) {
	o, err := domain.NewOrder(id.New(), in.UserID, in.Item, in.Qty, s.now().UTC())
	if err != nil {
		return OrderDTO{}, err
	}
	if err := s.orders.Create(ctx, o); err != nil {
		return OrderDTO{}, err
	}
	return toDTO(o), nil
}

// List returns the user's orders, newest first.
func (s *Service) List(ctx context.Context, userID string) ([]OrderDTO, error) {
	orders, err := s.orders.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]OrderDTO, 0, len(orders))
	for _, o := range orders {
		out = append(out, toDTO(o))
	}
	return out, nil
}

// SetStatus moves an order to done or cancelled.
func (s *Service) SetStatus(ctx context.Context, userID, orderID, status string) (OrderDTO, error) {
	o, err := s.orders.Get(ctx, orderID, userID)
	if err != nil {
		return OrderDTO{}, err
	}
	switch domain.Status(status) {
	case domain.StatusDone:
		err = o.Done()
	case domain.StatusCancelled:
		err = o.Cancel()
	default:
		return OrderDTO{}, apperr.Invalid("status", "must be done or cancelled")
	}
	if err != nil {
		return OrderDTO{}, err
	}
	if err := s.orders.Update(ctx, o); err != nil {
		return OrderDTO{}, err
	}
	return toDTO(o), nil
}

func toDTO(o *domain.Order) OrderDTO {
	return OrderDTO{
		ID:        o.ID,
		Item:      o.Item,
		Qty:       o.Qty,
		Status:    string(o.Status),
		CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339),
	}
}
