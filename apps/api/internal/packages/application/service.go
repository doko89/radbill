// Package application hosts package use cases.
package application

import (
	"context"
	"time"

	"boilerplate/api/internal/packages/domain"
	"boilerplate/api/internal/shared/id"
)

// PackageDTO is the public snake_case shape of a package.
type PackageDTO struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Price              int64   `json:"price"`
	Period             string  `json:"period"`
	Type               string  `json:"type"`
	RateLimit          *string `json:"rate_limit"`
	AddressPool        *string `json:"address_pool"`
	ValiditySeconds    *int64  `json:"validity_seconds"`
	OnlineSecondsLimit *int64  `json:"online_seconds_limit"`
	QuotaBytes         *int64  `json:"quota_bytes"`
	QuotaReset         string  `json:"quota_reset"`
	FUPThresholdBytes  *int64  `json:"fup_threshold_bytes"`
	FUPRateLimit       *string `json:"fup_rate_limit"`
	OnExhausted        string  `json:"on_exhausted"`
	IsPublic           bool    `json:"is_public"`
	AllowSelfService   bool    `json:"allow_self_service"`
	UpgradeGroup       *string `json:"upgrade_group"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

// DetailsInput carries raw package fields (pointers distinguish unset).
type DetailsInput struct {
	Name               string
	Price              int64
	Period             string
	Type               string
	RateLimit          string
	AddressPool        string
	ValiditySeconds    *int64
	OnlineSecondsLimit *int64
	QuotaBytes         *int64
	QuotaReset         string
	FUPThresholdBytes  *int64
	FUPRateLimit       string
	OnExhausted        string
	IsPublic           bool
	AllowSelfService   bool
	UpgradeGroup       string
}

// Service implements package use cases.
type Service struct {
	packages domain.Repository
	now      func() time.Time
}

// NewService builds a Service.
func NewService(packages domain.Repository) *Service {
	return &Service{packages: packages, now: time.Now}
}

// Create validates and persists a package.
func (s *Service) Create(ctx context.Context, in DetailsInput) (PackageDTO, error) {
	p, err := domain.NewPackage(id.New(), toDetails(in), s.now().UTC())
	if err != nil {
		return PackageDTO{}, err
	}
	if err := s.packages.Create(ctx, p); err != nil {
		return PackageDTO{}, err
	}
	return toDTO(p), nil
}

// Get returns one package.
func (s *Service) Get(ctx context.Context, pid string) (PackageDTO, error) {
	p, err := s.packages.Get(ctx, pid)
	if err != nil {
		return PackageDTO{}, err
	}
	return toDTO(p), nil
}

// List returns a page of packages plus the total count.
func (s *Service) List(ctx context.Context, limit, offset int) ([]PackageDTO, int64, error) {
	rows, err := s.packages.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.packages.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]PackageDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, toDTO(p))
	}
	return out, total, nil
}

// Update validates and persists package details.
func (s *Service) Update(ctx context.Context, pid string, in DetailsInput) (PackageDTO, error) {
	p, err := s.packages.Get(ctx, pid)
	if err != nil {
		return PackageDTO{}, err
	}
	if err := p.SetDetails(toDetails(in), s.now().UTC()); err != nil {
		return PackageDTO{}, err
	}
	if err := s.packages.Update(ctx, p); err != nil {
		return PackageDTO{}, err
	}
	return toDTO(p), nil
}

// Delete removes a package.
func (s *Service) Delete(ctx context.Context, pid string) error {
	return s.packages.Delete(ctx, pid)
}

func toDetails(in DetailsInput) domain.Details {
	return domain.Details{
		Name: in.Name, Price: in.Price, Period: in.Period, Type: in.Type,
		RateLimit: in.RateLimit, AddressPool: in.AddressPool,
		ValiditySeconds: in.ValiditySeconds, OnlineSecondsLimit: in.OnlineSecondsLimit,
		QuotaBytes: in.QuotaBytes, QuotaReset: in.QuotaReset,
		FUPThresholdBytes: in.FUPThresholdBytes, FUPRateLimit: in.FUPRateLimit,
		OnExhausted: in.OnExhausted, IsPublic: in.IsPublic,
		AllowSelfService: in.AllowSelfService, UpgradeGroup: in.UpgradeGroup,
	}
}

func toDTO(p *domain.Package) PackageDTO {
	return PackageDTO{
		ID: p.ID, Name: p.Name, Price: p.Price, Period: p.Period, Type: p.Type,
		RateLimit: nullStr(p.RateLimit), AddressPool: nullStr(p.AddressPool),
		ValiditySeconds: p.ValiditySeconds, OnlineSecondsLimit: p.OnlineSecondsLimit,
		QuotaBytes: p.QuotaBytes, QuotaReset: p.QuotaReset,
		FUPThresholdBytes: p.FUPThresholdBytes, FUPRateLimit: nullStr(p.FUPRateLimit),
		OnExhausted: p.OnExhausted, IsPublic: p.IsPublic,
		AllowSelfService: p.AllowSelfService, UpgradeGroup: nullStr(p.UpgradeGroup),
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
