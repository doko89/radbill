// Package infrastructure implements auth ports: hashing and SQL persistence.
package infrastructure

import "golang.org/x/crypto/bcrypt"

// BcryptHasher hashes passwords with bcrypt.
type BcryptHasher struct{}

// NewBcryptHasher builds a BcryptHasher.
func NewBcryptHasher() *BcryptHasher { return &BcryptHasher{} }

// Hash hashes password with the default cost.
func (BcryptHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Verify reports whether password matches hash.
func (BcryptHasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
