// Package coa sends RADIUS Change-of-Authorization and Disconnect packets
// (RFC 5176, UDP 3799) to MikroTik NAS devices. It is shared by the billing
// API (manual disconnect, CoA test) and reused by radiusd in Fase 3 for
// quota/FUP enforcement without new dependencies.
package coa

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

// DefaultTimeout bounds one CoA/Disconnect exchange.
const DefaultTimeout = 5 * time.Second

// NAS describes a CoA/Disconnect target.
type NAS struct {
	// Address is the NAS IP or hostname.
	Address string
	// Port is the CoA UDP port (3799 when 0).
	Port int
	// Secret is the RADIUS shared secret.
	Secret string
}

// Addr returns host:port for the NAS CoA endpoint.
func (n NAS) Addr() string {
	port := n.Port
	if port == 0 {
		port = 3799
	}
	return net.JoinHostPort(n.Address, strconv.Itoa(port))
}

// Sender exchanges CoA/Disconnect packets with NAS devices.
type Sender struct {
	client  *radius.Client
	timeout time.Duration
}

// NewSender builds a Sender with the given per-request timeout
// (DefaultTimeout when timeout <= 0).
func NewSender(timeout time.Duration) *Sender {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Sender{
		client:  &radius.Client{Net: "udp", Retry: 0, MaxPacketErrors: 5},
		timeout: timeout,
	}
}

// BuildDisconnect creates a Disconnect-Request packet carrying User-Name
// and Acct-Session-Id without touching the network (unit-test friendly).
func BuildDisconnect(secret, username, sessionID string) (*radius.Packet, error) {
	pkt := radius.New(radius.CodeDisconnectRequest, []byte(secret))
	if username != "" {
		if err := rfc2865.UserName_SetString(pkt, username); err != nil {
			return nil, fmt.Errorf("coa: set User-Name: %w", err)
		}
	}
	if sessionID != "" {
		if err := rfc2866.AcctSessionID_SetString(pkt, sessionID); err != nil {
			return nil, fmt.Errorf("coa: set Acct-Session-Id: %w", err)
		}
	}
	return pkt, nil
}

// BuildCoA creates a CoA-Request packet carrying User-Name and
// Acct-Session-Id, then applies mutate for extra attributes
// (e.g. Mikrotik-Rate-Limit for FUP). No network I/O happens here.
func BuildCoA(secret, username, sessionID string, mutate func(*radius.Packet) error) (*radius.Packet, error) {
	pkt := radius.New(radius.CodeCoARequest, []byte(secret))
	if username != "" {
		if err := rfc2865.UserName_SetString(pkt, username); err != nil {
			return nil, fmt.Errorf("coa: set User-Name: %w", err)
		}
	}
	if sessionID != "" {
		if err := rfc2866.AcctSessionID_SetString(pkt, sessionID); err != nil {
			return nil, fmt.Errorf("coa: set Acct-Session-Id: %w", err)
		}
	}
	if mutate != nil {
		if err := mutate(pkt); err != nil {
			return nil, err
		}
	}
	return pkt, nil
}

// Exchange sends pkt to addr and waits for the NAS response.
func (s *Sender) Exchange(ctx context.Context, pkt *radius.Packet, addr string) (*radius.Packet, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := s.client.Exchange(ctx, pkt, addr)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Disconnect sends a Disconnect-Request for the session and returns the
// NAS response code (41 ACK disconnects, 42 NAK means unknown session).
func (s *Sender) Disconnect(ctx context.Context, nas NAS, username, sessionID string) (radius.Code, error) {
	pkt, err := BuildDisconnect(nas.Secret, username, sessionID)
	if err != nil {
		return 0, err
	}
	resp, err := s.Exchange(ctx, pkt, nas.Addr())
	if err != nil {
		return 0, err
	}
	return resp.Code, nil
}

// ChangeOfAuth sends a CoA-Request for the session with extra attributes
// from mutate, returning the NAS response code (44 ACK / 45 NAK).
func (s *Sender) ChangeOfAuth(ctx context.Context, nas NAS, username, sessionID string, mutate func(*radius.Packet) error) (radius.Code, error) {
	pkt, err := BuildCoA(nas.Secret, username, sessionID, mutate)
	if err != nil {
		return 0, err
	}
	resp, err := s.Exchange(ctx, pkt, nas.Addr())
	if err != nil {
		return 0, err
	}
	return resp.Code, nil
}

// Probe tests NAS reachability with a harmless CoA-Request for a dummy
// session. Any response (even NAK) proves the NAS answers; only a network
// error reports unreachable. It returns the response code and true when
// the NAS answered.
func (s *Sender) Probe(ctx context.Context, nas NAS) (radius.Code, bool, error) {
	pkt, err := BuildCoA(nas.Secret, "radbill-probe", "radbill-test", nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := s.Exchange(ctx, pkt, nas.Addr())
	if err != nil {
		return 0, false, err
	}
	return resp.Code, true, nil
}
