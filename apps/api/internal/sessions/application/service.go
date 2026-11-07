// Package application hosts session use cases.
package application

import (
	"context"
	"time"

	"boilerplate/api/internal/sessions/domain"
)

// SessionDTO is the public snake_case shape of a session.
type SessionDTO struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"session_id"`
	PPPoEAccountID string  `json:"pppoe_account_id"`
	NASID          string  `json:"nas_id"`
	FramedIP       *string `json:"framed_ip"`
	StartedAt      string  `json:"started_at"`
	StoppedAt      *string `json:"stopped_at"`
	BytesIn        int64   `json:"bytes_in"`
	BytesOut       int64   `json:"bytes_out"`
	TerminateCause *string `json:"terminate_cause"`
	UpdatedAt      string  `json:"updated_at"`
}

// Service implements session use cases.
type Service struct {
	sessions domain.Repository
}

// NewService builds a Service.
func NewService(sessions domain.Repository) *Service {
	return &Service{sessions: sessions}
}

// ListOnline returns open sessions plus the total count.
func (s *Service) ListOnline(ctx context.Context, limit, offset int) ([]SessionDTO, int64, error) {
	rows, err := s.sessions.ListOnline(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.sessions.CountOnline(ctx)
	if err != nil {
		return nil, 0, err
	}
	return toDTOs(rows), total, nil
}

// ListHistory returns all sessions, newest first, plus the total count.
func (s *Service) ListHistory(ctx context.Context, limit, offset int) ([]SessionDTO, int64, error) {
	rows, err := s.sessions.ListHistory(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.sessions.CountHistory(ctx)
	if err != nil {
		return nil, 0, err
	}
	return toDTOs(rows), total, nil
}

func toDTOs(rows []*domain.Session) []SessionDTO {
	out := make([]SessionDTO, 0, len(rows))
	for _, sess := range rows {
		out = append(out, toDTO(sess))
	}
	return out
}

func toDTO(s *domain.Session) SessionDTO {
	dto := SessionDTO{
		ID:             s.ID,
		SessionID:      s.SessionID,
		PPPoEAccountID: s.PPPoEAccountID,
		NASID:          s.NASID,
		FramedIP:       nullStr(s.FramedIP),
		StartedAt:      s.StartedAt.UTC().Format(time.RFC3339),
		BytesIn:        s.BytesIn,
		BytesOut:       s.BytesOut,
		TerminateCause: nullStr(s.TerminateCause),
		UpdatedAt:      s.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if s.StoppedAt != nil {
		stopped := s.StoppedAt.UTC().Format(time.RFC3339)
		dto.StoppedAt = &stopped
	}
	return dto
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
