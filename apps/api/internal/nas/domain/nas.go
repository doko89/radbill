// Package domain holds the nas bounded-context core.
package domain

import (
	"context"
	"errors"
	"net"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// DefaultCoAPort is the UDP port for CoA/Disconnect (RFC 5176).
const DefaultCoAPort = 3799

var (
	// ErrNASNotFound is returned when the NAS does not exist.
	ErrNASNotFound = errors.New("nas not found")
	// ErrNASIPTaken is returned on duplicate NAS IPs.
	ErrNASIPTaken = errors.New("nas ip already registered")
	// ErrNASInUse is returned when deleting a NAS with sessions or accounts.
	ErrNASInUse = errors.New("nas still referenced")
)

// NAS is a MikroTik router acting as RADIUS client.
type NAS struct {
	ID        string
	Name      string
	IP        string
	Secret    string
	CoAPort   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewNAS builds a validated NAS. Empty coaPort selects 3799.
func NewNAS(id, name, ip, secret string, coaPort int, now time.Time) (*NAS, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	n := &NAS{ID: id, CreatedAt: now, UpdatedAt: now}
	if err := n.SetDetails(name, ip, secret, coaPort, now); err != nil {
		return nil, err
	}
	return n, nil
}

// SetDetails validates and replaces mutable fields.
func (n *NAS) SetDetails(name, ip, secret string, coaPort int, now time.Time) error {
	if name == "" {
		return apperr.Invalid("name", "required")
	}
	if len(name) > 120 {
		return apperr.Invalid("name", "max 120 characters")
	}
	if net.ParseIP(ip) == nil {
		return apperr.Invalid("ip", "must be a valid IP address")
	}
	if secret == "" {
		return apperr.Invalid("secret", "required")
	}
	if coaPort == 0 {
		coaPort = DefaultCoAPort
	}
	if coaPort < 1 || coaPort > 65535 {
		return apperr.Invalid("coa_port", "must be between 1 and 65535")
	}
	n.Name = name
	n.IP = ip
	n.Secret = secret
	n.CoAPort = coaPort
	n.UpdatedAt = now
	return nil
}

// Repository persists NAS rows. Implemented in infrastructure.
type Repository interface {
	Create(ctx context.Context, n *NAS) error
	Get(ctx context.Context, id string) (*NAS, error)
	List(ctx context.Context, limit, offset int) ([]*NAS, error)
	Count(ctx context.Context) (int64, error)
	Update(ctx context.Context, n *NAS) error
	Delete(ctx context.Context, id string) error
}
