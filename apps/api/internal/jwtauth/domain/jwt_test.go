package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignVerifyRoundtrip(t *testing.T) {
	secret := []byte("test-secret-min-32-chars-long-ok")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		claims Claims
	}{
		{"admin without customer", Claims{Subject: "a1", Role: RoleAdmin, ExpiresAt: now.Add(15 * time.Minute), IssuedAt: now}},
		{"user with customer", Claims{Subject: "u1", Role: RoleUser, CustomerID: "c9", ExpiresAt: now.Add(15 * time.Minute), IssuedAt: now}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := SignAccessToken(secret, tt.claims)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			got, err := VerifyAccessToken(secret, token, now)
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if got.Subject != tt.claims.Subject || got.Role != tt.claims.Role || got.CustomerID != tt.claims.CustomerID {
				t.Fatalf("claims mismatch: %+v", got)
			}
			if !got.ExpiresAt.Equal(tt.claims.ExpiresAt) || !got.IssuedAt.Equal(tt.claims.IssuedAt) {
				t.Fatalf("time mismatch: %+v", got)
			}
		})
	}
}

func TestVerifyRejects(t *testing.T) {
	secret := []byte("test-secret-min-32-chars-long-ok")
	other := []byte("other-secret-min-32-chars-long-ok!")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	valid, err := SignAccessToken(secret, Claims{Subject: "a1", Role: RoleAdmin, ExpiresAt: now.Add(time.Minute), IssuedAt: now})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	expired, err := SignAccessToken(secret, Claims{Subject: "a1", Role: RoleAdmin, ExpiresAt: now.Add(-time.Second), IssuedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("sign expired: %v", err)
	}
	forged := valid[:len(valid)-2] + "xx"
	tests := []struct {
		name  string
		token string
		key   []byte
		at    time.Time
	}{
		{"forged signature", forged, secret, now},
		{"wrong secret", valid, other, now},
		{"expired", expired, secret, now},
		{"empty", "", secret, now},
		{"two parts", "a.b", secret, now},
		{"four parts", "a.b.c.d", secret, now},
		{"bad payload encoding", valid[:strings.LastIndex(valid, ".")+1] + "!!!", secret, now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := VerifyAccessToken(tt.key, tt.token, tt.at); !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("expected ErrTokenInvalid, got %v", err)
			}
		})
	}
}

func TestVerifyRejectsEmptySubject(t *testing.T) {
	secret := []byte("test-secret-min-32-chars-long-ok")
	now := time.Now().UTC()
	token, err := SignAccessToken(secret, Claims{Role: RoleAdmin, ExpiresAt: now.Add(time.Minute), IssuedAt: now})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := VerifyAccessToken(secret, token, now); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}
