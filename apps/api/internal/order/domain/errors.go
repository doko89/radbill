package domain

import "errors"

var (
	// ErrOrderNotFound is returned when the order is missing or belongs to another user.
	ErrOrderNotFound = errors.New("order not found")
	// ErrNotOpen is returned when transitioning a non-open order.
	ErrNotOpen = errors.New("only open orders can change status")
)
