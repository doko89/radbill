// Package application hosts customer use cases.
package application

import (
	"context"
	"time"

	"boilerplate/api/internal/customers/domain"
	"boilerplate/api/internal/shared/id"
)

// CustomerDTO is the public snake_case shape of a customer.
type CustomerDTO struct {
	ID        string  `json:"id"`
	AccountID *string `json:"account_id"`
	Name      string  `json:"name"`
	Phone     *string `json:"phone"`
	Address   *string `json:"address"`
	Notes     *string `json:"notes"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// CreateInput carries raw create-customer fields.
type CreateInput struct {
	Name    string
	Phone   string
	Address string
	Notes   string
}

// UpdateInput carries raw update-customer fields.
type UpdateInput struct {
	Name    string
	Phone   string
	Address string
	Notes   string
}

// Service implements customer use cases.
type Service struct {
	customers domain.Repository
	now       func() time.Time
}

// NewService builds a Service.
func NewService(customers domain.Repository) *Service {
	return &Service{customers: customers, now: time.Now}
}

// Create validates and persists a customer.
func (s *Service) Create(ctx context.Context, in CreateInput) (CustomerDTO, error) {
	c, err := domain.NewCustomer(id.New(), in.Name, in.Phone, in.Address, in.Notes, s.now().UTC())
	if err != nil {
		return CustomerDTO{}, err
	}
	if err := s.customers.Create(ctx, c); err != nil {
		return CustomerDTO{}, err
	}
	return toDTO(c), nil
}

// Get returns one customer.
func (s *Service) Get(ctx context.Context, cid string) (CustomerDTO, error) {
	c, err := s.customers.Get(ctx, cid)
	if err != nil {
		return CustomerDTO{}, err
	}
	return toDTO(c), nil
}

// List returns a page of customers plus the total count.
func (s *Service) List(ctx context.Context, limit, offset int) ([]CustomerDTO, int64, error) {
	rows, err := s.customers.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.customers.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]CustomerDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, toDTO(c))
	}
	return out, total, nil
}

// Update validates and persists customer details.
func (s *Service) Update(ctx context.Context, cid string, in UpdateInput) (CustomerDTO, error) {
	c, err := s.customers.Get(ctx, cid)
	if err != nil {
		return CustomerDTO{}, err
	}
	if err := c.SetDetails(in.Name, in.Phone, in.Address, in.Notes, s.now().UTC()); err != nil {
		return CustomerDTO{}, err
	}
	if err := s.customers.Update(ctx, c); err != nil {
		return CustomerDTO{}, err
	}
	return toDTO(c), nil
}

// Delete removes a customer.
func (s *Service) Delete(ctx context.Context, cid string) error {
	return s.customers.Delete(ctx, cid)
}

func toDTO(c *domain.Customer) CustomerDTO {
	return CustomerDTO{
		ID:        c.ID,
		AccountID: nullStr(c.AccountID),
		Name:      c.Name,
		Phone:     nullStr(c.Phone),
		Address:   nullStr(c.Address),
		Notes:     nullStr(c.Notes),
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
