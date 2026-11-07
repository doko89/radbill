// Package domain holds the order bounded-context core.
package domain

import (
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// Order statuses and their allowed transitions:
// open -> done, open -> cancelled. Terminal states never change.
type Status string

const (
	StatusOpen      Status = "open"
	StatusDone      Status = "done"
	StatusCancelled Status = "cancelled"
)

// MaxQty bounds order quantity.
const MaxQty = 9999

// Order is the aggregate root of the order context.
type Order struct {
	ID        string
	UserID    string
	Item      string
	Qty       int64
	Status    Status
	CreatedAt time.Time
}

// NewOrder builds a validated open order.
func NewOrder(id, userID, item string, qty int64, now time.Time) (*Order, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	if userID == "" {
		return nil, apperr.Invalid("userId", "required")
	}
	if item == "" {
		return nil, apperr.Invalid("item", "required")
	}
	if len(item) > 120 {
		return nil, apperr.Invalid("item", "max 120 characters")
	}
	if qty < 1 || qty > MaxQty {
		return nil, apperr.Invalid("qty", "must be between 1 and 9999")
	}
	return &Order{ID: id, UserID: userID, Item: item, Qty: qty, Status: StatusOpen, CreatedAt: now}, nil
}

// Done moves an open order to done.
func (o *Order) Done() error {
	if o.Status != StatusOpen {
		return ErrNotOpen
	}
	o.Status = StatusDone
	return nil
}

// Cancel moves an open order to cancelled.
func (o *Order) Cancel() error {
	if o.Status != StatusOpen {
		return ErrNotOpen
	}
	o.Status = StatusCancelled
	return nil
}
