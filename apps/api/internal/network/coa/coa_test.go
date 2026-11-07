package coa

import (
	"errors"
	"net"
	"testing"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

var errBoom = errors.New("boom")

func TestBuildDisconnect(t *testing.T) {
	pkt, err := BuildDisconnect("s3cret", "cust01", "sess-1")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if pkt.Code != radius.CodeDisconnectRequest {
		t.Fatalf("code = %v, want Disconnect-Request", pkt.Code)
	}
	if got := rfc2865.UserName_GetString(pkt); got != "cust01" {
		t.Fatalf("User-Name = %q", got)
	}
	if got := rfc2866.AcctSessionID_GetString(pkt); got != "sess-1" {
		t.Fatalf("Acct-Session-Id = %q", got)
	}
	if _, err := pkt.Encode(); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestBuildCoA(t *testing.T) {
	tests := []struct {
		name      string
		username  string
		sessionID string
		mutate    func(*radius.Packet) error
		wantUser  string
		wantSess  string
	}{
		{"full", "cust02", "sess-2", nil, "cust02", "sess-2"},
		{"empty idents", "", "", nil, "", ""},
		{"mutate adds framed ip", "c", "s", func(p *radius.Packet) error {
			return rfc2865.FramedIPAddress_Set(p, net.IPv4(10, 0, 0, 5))
		}, "c", "s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt, err := BuildCoA("s3cret", tt.username, tt.sessionID, tt.mutate)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if pkt.Code != radius.CodeCoARequest {
				t.Fatalf("code = %v, want CoA-Request", pkt.Code)
			}
			if got := rfc2865.UserName_GetString(pkt); got != tt.wantUser {
				t.Fatalf("User-Name = %q, want %q", got, tt.wantUser)
			}
			if got := rfc2866.AcctSessionID_GetString(pkt); got != tt.wantSess {
				t.Fatalf("Acct-Session-Id = %q, want %q", got, tt.wantSess)
			}
			if _, err := pkt.Encode(); err != nil {
				t.Fatalf("encode: %v", err)
			}
		})
	}
	if _, err := BuildCoA("s", "u", "i", func(*radius.Packet) error { return errBoom }); err != errBoom {
		t.Fatalf("mutate error = %v, want boom", err)
	}
}

func TestNASAddr(t *testing.T) {
	if got := (NAS{Address: "10.1.1.1"}).Addr(); got != "10.1.1.1:3799" {
		t.Fatalf("addr = %q", got)
	}
	if got := (NAS{Address: "10.1.1.1", Port: 3798}).Addr(); got != "10.1.1.1:3798" {
		t.Fatalf("addr = %q", got)
	}
}
