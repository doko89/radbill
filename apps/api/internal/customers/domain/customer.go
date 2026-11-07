// Package domain holds the customers bounded-context core.
package domain

import (
	"context"
	"errors"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

var (
	// ErrCustomerNotFound is returned when the customer does not exist.
	ErrCustomerNotFound = errors.New("customer not found")
	// ErrCustomerInUse is returned when deleting a customer with PPPoE accounts.
	ErrCustomerInUse = errors.New("customer still has pppoe accounts")
	// ErrPortalExists is returned when the customer already has a portal account.
	ErrPortalExists = errors.New("customer already has a portal account")
	// ErrNoPortalAccount is returned when the customer has no portal account.
	ErrNoPortalAccount = errors.New("customer has no portal account")
	// ErrPortalUsernameTaken is returned on duplicate portal usernames.
	ErrPortalUsernameTaken = errors.New("portal username already taken")
)

// Customer is a billing customer (1:N with PPPoE accounts).
type Customer struct {
	ID        string
	AccountID string
	Name      string
	Phone     string
	Address   string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewCustomer builds a validated customer.
func NewCustomer(id, name, phone, address, notes string, now time.Time) (*Customer, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	c := &Customer{ID: id, CreatedAt: now, UpdatedAt: now}
	if err := c.SetDetails(name, phone, address, notes, now); err != nil {
		return nil, err
	}
	return c, nil
}

// SetDetails validates and replaces mutable fields.
func (c *Customer) SetDetails(name, phone, address, notes string, now time.Time) error {
	if name == "" {
		return apperr.Invalid("name", "required")
	}
	if len(name) > 200 {
		return apperr.Invalid("name", "max 200 characters")
	}
	if len(phone) > 32 {
		return apperr.Invalid("phone", "max 32 characters")
	}
	if len(address) > 500 {
		return apperr.Invalid("address", "max 500 characters")
	}
	if len(notes) > 2000 {
		return apperr.Invalid("notes", "max 2000 characters")
	}
	c.Name = name
	c.Phone = phone
	c.Address = address
	c.Notes = notes
	c.UpdatedAt = now
	return nil
}

// Repository persists customers. Implemented in infrastructure.
type Repository interface {
	Create(ctx context.Context, c *Customer) error
	Get(ctx context.Context, id string) (*Customer, error)
	List(ctx context.Context, limit, offset int) ([]*Customer, error)
	Count(ctx context.Context) (int64, error)
	Update(ctx context.Context, c *Customer) error
	Delete(ctx context.Context, id string) error
}

// PortalStore creates and resets customer portal logins.
// Implemented in infrastructure (accounts + customers in one transaction).
// The pppoe context implementation satisfies this interface structurally.
type PortalStore interface {
	// CreatePortalAccount creates a role=user account and links it to the
	// customer. It returns the account id.
	CreatePortalAccount(ctx context.Context, customerID, username, passwordHash string, now time.Time) (string, error)
	// ResetPortalPassword replaces the portal password (hash) and revokes
	// refresh tokens. It returns the portal username.
	ResetPortalPassword(ctx context.Context, customerID, passwordHash string, now time.Time) (string, error)
}
