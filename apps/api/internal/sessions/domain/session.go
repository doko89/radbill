// Package domain holds the sessions bounded-context core.
package domain

import (
	"context"
	"time"
)

// Session is one RADIUS accounting session row.
type Session struct {
	ID             string
	SessionID      string
	PPPoEAccountID string
	NASID          string
	FramedIP       string
	StartedAt      time.Time
	StoppedAt      *time.Time
	BytesIn        int64
	BytesOut       int64
	TerminateCause string
	UpdatedAt      time.Time
}

// Online reports whether the session is still open.
func (s *Session) Online() bool { return s.StoppedAt == nil }

// Repository reads sessions. Implemented in infrastructure.
type Repository interface {
	ListOnline(ctx context.Context, limit, offset int) ([]*Session, error)
	CountOnline(ctx context.Context) (int64, error)
	ListHistory(ctx context.Context, limit, offset int) ([]*Session, error)
	CountHistory(ctx context.Context) (int64, error)
}
