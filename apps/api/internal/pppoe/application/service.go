// Package application hosts pppoe use cases.
package application

import (
	"context"
	"crypto/rand"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/pppoe/domain"
	"boilerplate/api/internal/shared/apperr"
	"boilerplate/api/internal/shared/id"
)

// PPPoEDTO is the public snake_case shape of a PPPoE account. Password is
// only set when the caller explicitly asked for reveal (admin + audit log).
type PPPoEDTO struct {
	ID         string  `json:"id"`
	CustomerID string  `json:"customer_id"`
	PackageID  string  `json:"package_id"`
	NASID      *string `json:"nas_id"`
	Username   string  `json:"username"`
	Password   *string `json:"password,omitempty"`
	Status     string  `json:"status"`
	ExpiresAt  *string `json:"expires_at"`
	StaticIP   *string `json:"static_ip"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// DetailsInput carries raw PPPoE fields.
type DetailsInput struct {
	CustomerID string
	PackageID  string
	NASID      string
	Username   string
	Password   string
	ExpiresAt  *time.Time
	StaticIP   string
}

// PortalDTO carries a one-time portal credential (shown once at create/reset).
type PortalDTO struct {
	AccountID string `json:"account_id"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// DisconnectDTO reports a disconnect action.
type DisconnectDTO struct {
	Disconnected int `json:"disconnected"`
	Total        int `json:"total"`
}

// Service implements pppoe use cases.
type Service struct {
	pppoe        domain.Repository
	portal       domain.PortalStore
	disconnecter domain.Disconnecter
	hasher       authdomain.Hasher
	now          func() time.Time
}

// NewService builds a Service.
func NewService(pppoe domain.Repository, portal domain.PortalStore, disconnecter domain.Disconnecter, hasher authdomain.Hasher) *Service {
	return &Service{pppoe: pppoe, portal: portal, disconnecter: disconnecter, hasher: hasher, now: time.Now}
}

// Create validates foreign keys and persists a PPPoE account.
func (s *Service) Create(ctx context.Context, in DetailsInput) (PPPoEDTO, error) {
	p, err := domain.NewPPPoE(id.New(), toDetails(in), s.now().UTC())
	if err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.checkRefs(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.pppoe.Create(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// Get returns one PPPoE account, with password only when reveal is true.
func (s *Service) Get(ctx context.Context, pid string, reveal bool) (PPPoEDTO, error) {
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, reveal), nil
}

// List returns a page of PPPoE accounts (never with passwords).
func (s *Service) List(ctx context.Context, limit, offset int) ([]PPPoEDTO, int64, error) {
	rows, err := s.pppoe.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.pppoe.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]PPPoEDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, toDTO(p, false))
	}
	return out, total, nil
}

// Update validates and persists PPPoE details.
func (s *Service) Update(ctx context.Context, pid string, in DetailsInput) (PPPoEDTO, error) {
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	if err := p.SetDetails(toDetails(in), s.now().UTC()); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.checkRefs(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.pppoe.Update(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// Delete removes a PPPoE account.
func (s *Service) Delete(ctx context.Context, pid string) error {
	return s.pppoe.Delete(ctx, pid)
}

// Isolate moves the account to isolated.
func (s *Service) Isolate(ctx context.Context, pid string) (PPPoEDTO, error) {
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	if err := p.Isolate(s.now().UTC()); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.pppoe.Update(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// Activate moves the account to active.
func (s *Service) Activate(ctx context.Context, pid string) (PPPoEDTO, error) {
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	if err := p.Activate(s.now().UTC()); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.pppoe.Update(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// Disable moves the account to disabled.
func (s *Service) Disable(ctx context.Context, pid string) (PPPoEDTO, error) {
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	p.Disable(s.now().UTC())
	if err := s.pppoe.Update(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// Extend moves expiry forward by days from max(now, current expiry).
func (s *Service) Extend(ctx context.Context, pid string, days int) (PPPoEDTO, error) {
	if days < 1 || days > 3650 {
		return PPPoEDTO{}, apperr.Invalid("days", "must be between 1 and 3650")
	}
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	now := s.now().UTC()
	base := now
	if p.ExpiresAt != nil && p.ExpiresAt.After(now) {
		base = *p.ExpiresAt
	}
	if err := p.Extend(base.AddDate(0, 0, days), now); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.pppoe.Update(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// ChangePackage switches the package of the account.
func (s *Service) ChangePackage(ctx context.Context, pid, packageID string) (PPPoEDTO, error) {
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PPPoEDTO{}, err
	}
	if !s.pppoe.PackageExists(ctx, packageID) {
		return PPPoEDTO{}, domain.ErrUnknownPackage
	}
	if err := p.ChangePackage(packageID, s.now().UTC()); err != nil {
		return PPPoEDTO{}, err
	}
	if err := s.pppoe.Update(ctx, p); err != nil {
		return PPPoEDTO{}, err
	}
	return toDTO(p, false), nil
}

// ResetQuota clears usage counters for the account.
func (s *Service) ResetQuota(ctx context.Context, pid string) error {
	if _, err := s.pppoe.Get(ctx, pid); err != nil {
		return err
	}
	return s.pppoe.ResetQuota(ctx, pid)
}

// Disconnect drops live sessions of the account via CoA/Disconnect.
func (s *Service) Disconnect(ctx context.Context, pid string) (DisconnectDTO, error) {
	if _, err := s.pppoe.Get(ctx, pid); err != nil {
		return DisconnectDTO{}, err
	}
	disconnected, total, err := s.disconnecter.DisconnectAll(ctx, pid)
	if err != nil {
		return DisconnectDTO{}, err
	}
	return DisconnectDTO{Disconnected: disconnected, Total: total}, nil
}

// CreatePortalAccount creates a portal login for the PPPoE owner's customer.
// An empty password generates a random one; it is returned exactly once.
func (s *Service) CreatePortalAccount(ctx context.Context, pid, username, password string) (PortalDTO, error) {
	if username == "" {
		return PortalDTO{}, apperr.Invalid("username", "required")
	}
	if password == "" {
		var err error
		password, err = randomPassword(12)
		if err != nil {
			return PortalDTO{}, err
		}
	}
	if len(password) < 8 {
		return PortalDTO{}, apperr.Invalid("password", "minimum 8 characters")
	}
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PortalDTO{}, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return PortalDTO{}, err
	}
	accountID, err := s.portal.CreatePortalAccount(ctx, p.CustomerID, username, hash, s.now().UTC())
	if err != nil {
		return PortalDTO{}, err
	}
	return PortalDTO{AccountID: accountID, Username: username, Password: password}, nil
}

// ResetPortalAccount replaces the portal password (random when empty).
func (s *Service) ResetPortalAccount(ctx context.Context, pid, password string) (PortalDTO, error) {
	if password == "" {
		var err error
		password, err = randomPassword(12)
		if err != nil {
			return PortalDTO{}, err
		}
	}
	if len(password) < 8 {
		return PortalDTO{}, apperr.Invalid("password", "minimum 8 characters")
	}
	p, err := s.pppoe.Get(ctx, pid)
	if err != nil {
		return PortalDTO{}, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return PortalDTO{}, err
	}
	username, err := s.portal.ResetPortalPassword(ctx, p.CustomerID, hash, s.now().UTC())
	if err != nil {
		return PortalDTO{}, err
	}
	return PortalDTO{Username: username, Password: password}, nil
}

func (s *Service) checkRefs(ctx context.Context, p *domain.PPPoE) error {
	if !s.pppoe.CustomerExists(ctx, p.CustomerID) {
		return domain.ErrUnknownCustomer
	}
	if !s.pppoe.PackageExists(ctx, p.PackageID) {
		return domain.ErrUnknownPackage
	}
	if p.NASID != "" && !s.pppoe.NASExists(ctx, p.NASID) {
		return domain.ErrUnknownNAS
	}
	return nil
}

func toDetails(in DetailsInput) domain.Details {
	return domain.Details{
		CustomerID: in.CustomerID, PackageID: in.PackageID, NASID: in.NASID,
		Username: in.Username, Password: in.Password,
		ExpiresAt: in.ExpiresAt, StaticIP: in.StaticIP,
	}
}

func toDTO(p *domain.PPPoE, reveal bool) PPPoEDTO {
	dto := PPPoEDTO{
		ID:         p.ID,
		CustomerID: p.CustomerID,
		PackageID:  p.PackageID,
		NASID:      nullStr(p.NASID),
		Username:   p.Username,
		Status:     p.Status,
		StaticIP:   nullStr(p.StaticIP),
		CreatedAt:  p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  p.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if reveal {
		dto.Password = &p.Password
	}
	if p.ExpiresAt != nil {
		exp := p.ExpiresAt.UTC().Format(time.RFC3339)
		dto.ExpiresAt = &exp
	}
	return dto
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

const passwordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"

// randomPassword builds an unambiguous alphanumeric password.
func randomPassword(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = passwordAlphabet[int(buf[i])%len(passwordAlphabet)]
	}
	return string(buf), nil
}
