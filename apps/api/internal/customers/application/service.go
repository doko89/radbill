// Package application hosts customer use cases.
package application

import (
	"context"
	"errors"
	"time"

	"boilerplate/api/internal/customers/domain"
	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/shared/apperr"
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
	portal    domain.PortalStore
	hasher    authdomain.Hasher
	now       func() time.Time
}

// NewService builds a Service.
func NewService(customers domain.Repository) *Service {
	return &Service{customers: customers, now: time.Now}
}

// SetPortalStore enables the portal-account use cases (same store and
// hasher the pppoe context uses; must be wired before those methods).
func (s *Service) SetPortalStore(portal domain.PortalStore, hasher authdomain.Hasher) {
	s.portal = portal
	s.hasher = hasher
}

// PortalDTO carries a one-time portal credential (shown once at create/reset).
type PortalDTO struct {
	AccountID string `json:"account_id"`
	Username  string `json:"username"`
	Password  string `json:"password"`
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

// CreatePortalAccount creates a portal login directly for a customer.
// An empty password generates a random one; it is returned exactly once.
func (s *Service) CreatePortalAccount(ctx context.Context, cid, username, password string) (PortalDTO, error) {
	if username == "" {
		return PortalDTO{}, apperr.Invalid("username", "required")
	}
	if password == "" {
		password = id.New()[:12]
	}
	if len(password) < 8 {
		return PortalDTO{}, apperr.Invalid("password", "minimum 8 characters")
	}
	c, err := s.customers.Get(ctx, cid)
	if err != nil {
		return PortalDTO{}, err
	}
	if c.AccountID != "" {
		return PortalDTO{}, domain.ErrPortalExists
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return PortalDTO{}, err
	}
	accountID, err := s.portal.CreatePortalAccount(ctx, cid, username, hash, s.now().UTC())
	if err != nil {
		if errors.Is(err, authdomain.ErrUsernameTaken) {
			return PortalDTO{}, domain.ErrPortalUsernameTaken
		}
		return PortalDTO{}, err
	}
	return PortalDTO{AccountID: accountID, Username: username, Password: password}, nil
}

// ResetPortalAccount replaces the portal password (random when empty).
func (s *Service) ResetPortalAccount(ctx context.Context, cid, password string) (PortalDTO, error) {
	if password == "" {
		password = id.New()[:12]
	}
	if len(password) < 8 {
		return PortalDTO{}, apperr.Invalid("password", "minimum 8 characters")
	}
	c, err := s.customers.Get(ctx, cid)
	if err != nil {
		return PortalDTO{}, err
	}
	if c.AccountID == "" {
		return PortalDTO{}, domain.ErrNoPortalAccount
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return PortalDTO{}, err
	}
	username, err := s.portal.ResetPortalPassword(ctx, cid, hash, s.now().UTC())
	if err != nil {
		return PortalDTO{}, err
	}
	return PortalDTO{Username: username, Password: password}, nil
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
