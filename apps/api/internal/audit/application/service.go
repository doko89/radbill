// Package application hosts the audit use case: synchronous,
// best-effort logging of admin mutations plus filtered listing.
//
// Best-effort contract (Fase 4, agen E1/G4): Log inserts synchronously
// inside the request, but a failure must never fail the request.
// Presentation call sites log the returned error to the server log
// (structured) and still return the successful response.
package application

import (
	"context"
	"time"

	auditdomain "boilerplate/api/internal/audit/domain"
	"boilerplate/api/internal/shared/id"
)

// LogInput carries one audit event. ActorUsername is resolved from
// ActorID inside Log (best-effort); callers never supply it.
type LogInput struct {
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Detail     string
	IP         string
}

// Service implements audit use cases.
type Service struct {
	store auditdomain.Store
	now   func() time.Time
}

// NewService builds a Service.
func NewService(store auditdomain.Store) *Service {
	return &Service{store: store, now: time.Now}
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

// Log inserts one entry synchronously. The actor username is looked up
// best-effort (lookup failure keeps ""); the insert error is returned
// so the caller can log it — callers must NOT fail the request.
func (s *Service) Log(ctx context.Context, in LogInput) error {
	e := &auditdomain.Entry{
		ID:         id.New(),
		ActorID:    in.ActorID,
		Action:     in.Action,
		TargetType: in.TargetType,
		TargetID:   in.TargetID,
		Detail:     in.Detail,
		IP:         in.IP,
		CreatedAt:  s.now().UTC(),
	}
	if in.ActorID != "" {
		if username, err := s.store.UsernameForAccount(ctx, in.ActorID); err == nil {
			e.ActorUsername = username
		}
	}
	return s.store.Insert(ctx, e)
}

// List returns filtered entries, newest first, with the total count.
func (s *Service) List(ctx context.Context, f auditdomain.Filter) ([]auditdomain.Entry, int64, error) {
	return s.store.List(ctx, f)
}
