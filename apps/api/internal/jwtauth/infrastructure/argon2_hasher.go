// Package infrastructure implements jwtauth ports on SQL and argon2id.
package infrastructure

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (RFC 9106 first recommendation).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

// Argon2Hasher implements domain.Hasher with argon2id PHC strings.
type Argon2Hasher struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen int
}

// NewArgon2Hasher builds a Hasher with production parameters.
func NewArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{time: argonTime, memory: argonMemory, threads: argonThreads, keyLen: argonKeyLen, saltLen: argonSaltLen}
}

// NewTestArgon2Hasher builds a Hasher with tiny parameters for fast unit tests.
func NewTestArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{time: 1, memory: 8 * 1024, threads: 1, keyLen: 16, saltLen: 8}
}

// Hash hashes password into a $argon2id$ PHC string.
func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2 salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, h.keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// Verify reports whether password matches the PHC hash. Empty hashes never match.
func (h *Argon2Hasher) Verify(password, hash string) bool {
	if hash == "" {
		return false
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
