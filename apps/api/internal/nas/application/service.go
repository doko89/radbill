// Package application hosts nas use cases.
package application

import (
	"context"
	"time"

	"boilerplate/api/internal/nas/domain"
	"boilerplate/api/internal/network/coa"
	"boilerplate/api/internal/shared/id"
)

// NASDTO is the public snake_case shape of a NAS. The shared secret is
// never serialized: it is write-only.
type NASDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	CoAPort   int    `json:"coa_port"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// DetailsInput carries raw NAS fields.
type DetailsInput struct {
	Name    string
	IP      string
	Secret  string
	CoAPort int
}

// CoAResult reports a test-coa probe outcome.
type CoAResult struct {
	Reachable    bool   `json:"reachable"`
	ResponseCode int    `json:"response_code,omitempty"`
	Response     string `json:"response,omitempty"`
}

// Service implements nas use cases.
type Service struct {
	nas    domain.Repository
	sender *coa.Sender
	now    func() time.Time
}

// NewService builds a Service.
func NewService(nas domain.Repository, sender *coa.Sender) *Service {
	return &Service{nas: nas, sender: sender, now: time.Now}
}

// Create validates and persists a NAS.
func (s *Service) Create(ctx context.Context, in DetailsInput) (NASDTO, error) {
	n, err := domain.NewNAS(id.New(), in.Name, in.IP, in.Secret, in.CoAPort, s.now().UTC())
	if err != nil {
		return NASDTO{}, err
	}
	if err := s.nas.Create(ctx, n); err != nil {
		return NASDTO{}, err
	}
	return toDTO(n), nil
}

// Get returns one NAS (without secret).
func (s *Service) Get(ctx context.Context, nasID string) (NASDTO, error) {
	n, err := s.nas.Get(ctx, nasID)
	if err != nil {
		return NASDTO{}, err
	}
	return toDTO(n), nil
}

// List returns a page of NAS rows plus the total count.
func (s *Service) List(ctx context.Context, limit, offset int) ([]NASDTO, int64, error) {
	rows, err := s.nas.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.nas.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]NASDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, toDTO(n))
	}
	return out, total, nil
}

// Update validates and persists NAS details.
func (s *Service) Update(ctx context.Context, nasID string, in DetailsInput) (NASDTO, error) {
	n, err := s.nas.Get(ctx, nasID)
	if err != nil {
		return NASDTO{}, err
	}
	if err := n.SetDetails(in.Name, in.IP, in.Secret, in.CoAPort, s.now().UTC()); err != nil {
		return NASDTO{}, err
	}
	if err := s.nas.Update(ctx, n); err != nil {
		return NASDTO{}, err
	}
	return toDTO(n), nil
}

// Delete removes a NAS.
func (s *Service) Delete(ctx context.Context, nasID string) error {
	return s.nas.Delete(ctx, nasID)
}

// TestCoA probes the NAS CoA endpoint. Unreachable NAS devices report
// reachable=false instead of an error.
func (s *Service) TestCoA(ctx context.Context, nasID string) (CoAResult, error) {
	n, err := s.nas.Get(ctx, nasID)
	if err != nil {
		return CoAResult{}, err
	}
	code, ok, err := s.sender.Probe(ctx, coa.NAS{Address: n.IP, Port: n.CoAPort, Secret: n.Secret})
	if err != nil || !ok {
		return CoAResult{Reachable: false}, nil
	}
	return CoAResult{Reachable: true, ResponseCode: int(code), Response: code.String()}, nil
}

func toDTO(n *domain.NAS) NASDTO {
	return NASDTO{
		ID:        n.ID,
		Name:      n.Name,
		IP:        n.IP,
		CoAPort:   n.CoAPort,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
