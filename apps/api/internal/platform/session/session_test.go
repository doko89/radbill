package session

import (
	"testing"
	"time"
)

func TestIssueVerifyRoundtrip(t *testing.T) {
	m := New("test-secret-0123456789", time.Hour, false)
	token := m.Issue("user-1")
	got, err := m.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != "user-1" {
		t.Fatalf("expected user-1, got %q", got)
	}
}

func TestVerifyRejects(t *testing.T) {
	m := New("test-secret-0123456789", time.Hour, false)
	valid := m.Issue("user-1")

	cases := map[string]string{
		"empty":     "",
		"garbage":   "not-a-token",
		"truncated": valid[:len(valid)-4],
		"forged":    valid[:len(valid)-1] + "A",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Verify(token); err != ErrInvalid {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	other := New("different-secret-xyz", time.Hour, false)
	if _, err := other.Verify(valid); err != ErrInvalid {
		t.Fatalf("wrong secret: expected ErrInvalid, got %v", err)
	}
}

func TestVerifyExpired(t *testing.T) {
	m := New("test-secret-0123456789", -time.Second, false)
	token := m.Issue("user-1")
	if _, err := m.Verify(token); err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}
