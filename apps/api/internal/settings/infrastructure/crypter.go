// Package infrastructure implements settings ports: AES-GCM secret
// encryption and the SQL repository.
package infrastructure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	setdomain "boilerplate/api/internal/settings/domain"
)

// encPrefix marks encrypted values: enc:v1:<base64(nonce|ciphertext)>.
// The wire format is a cross-agent contract (C2 kriskris reimplements it
// without importing this package); do not change it.
const encPrefix = "enc:v1:"

// nonceSize is the GCM nonce length (12 bytes, random per encryption).
const nonceSize = 12

// Crypter implements domain.Crypter with AES-256-GCM.
// The 32-byte key is SHA256(SETTINGS_ENC_KEY).
type Crypter struct {
	key [32]byte
}

// NewCrypter builds a Crypter from the raw env/file key material.
func NewCrypter(envKey string) *Crypter {
	return &Crypter{key: sha256.Sum256([]byte(envKey))}
}

// Encrypt seals plaintext with a random nonce.
func (c *Crypter) Encrypt(plaintext string) (string, error) {
	gcm, err := c.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	buf := make([]byte, 0, nonceSize+len(sealed))
	buf = append(buf, nonce...)
	buf = append(buf, sealed...)
	return encPrefix + base64.StdEncoding.EncodeToString(buf), nil
}

// Decrypt opens a value produced by Encrypt. Empty input means "unset" and
// returns "". Anything else without the prefix is rejected, never returned.
func (c *Crypter) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	raw, ok := strings.CutPrefix(stored, encPrefix)
	if !ok {
		return "", setdomain.ErrNotEncrypted
	}
	buf, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", setdomain.ErrDecryptFailed
	}
	if len(buf) < nonceSize+16 {
		return "", setdomain.ErrDecryptFailed
	}
	gcm, err := c.aead()
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, buf[:nonceSize], buf[nonceSize:], nil)
	if err != nil {
		return "", setdomain.ErrDecryptFailed
	}
	return string(plain), nil
}

func (c *Crypter) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
