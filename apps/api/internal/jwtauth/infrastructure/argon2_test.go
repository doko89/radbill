package infrastructure

import (
	"strings"
	"testing"
)

func TestArgon2HashVerify(t *testing.T) {
	h := NewTestArgon2Hasher()
	passwords := []string{"correct-horse-battery", "p4ssw0rd!", "únïcödé-🔑", strings.Repeat("long-", 20)}
	for _, pw := range passwords {
		hash, err := h.Hash(pw)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if !strings.HasPrefix(hash, "$argon2id$") {
			t.Fatalf("hash %q missing argon2id prefix", hash)
		}
		if !h.Verify(pw, hash) {
			t.Errorf("correct password rejected")
		}
		if h.Verify(pw+"x", hash) {
			t.Errorf("wrong password accepted")
		}
	}
}

func TestArgon2SaltsDiffer(t *testing.T) {
	h := NewTestArgon2Hasher()
	a, _ := h.Hash("same")
	b, _ := h.Hash("same")
	if a == b {
		t.Fatalf("two hashes of the same password are identical (no salt)")
	}
}

func TestArgon2VerifyRejects(t *testing.T) {
	h := NewTestArgon2Hasher()
	hash, err := h.Hash("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	tests := []struct {
		name     string
		password string
		hash     string
	}{
		{"empty hash never matches", "secret123", ""},
		{"empty password", "", hash},
		{"garbage hash", "secret123", "not-a-hash"},
		{"wrong scheme", "secret123", "$bcrypt$v=1$m=1,t=1,p=1$c2FsdA$aGFzaA"},
		{"truncated", "secret123", "$argon2id$v=19$m=8192"},
		{"bad salt encoding", "secret123", "$argon2id$v=19$m=8192,t=1,p=1$!!!$aGFzaA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if h.Verify(tt.password, tt.hash) {
				t.Fatalf("Verify accepted %q", tt.name)
			}
		})
	}
}
