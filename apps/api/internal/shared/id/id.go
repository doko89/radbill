// Package id generates opaque unique identifiers for aggregates.
package id

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns a random 32-char hex id. It panics if the OS CSPRNG fails,
// which is unrecoverable for identifier generation.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("id: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
