package infrastructure

import (
	"strings"
	"testing"

	setdomain "boilerplate/api/internal/settings/domain"
)

func TestCrypterRoundtrip(t *testing.T) {
	c := NewCrypter("test-settings-enc-key")
	for _, plain := range []string{"", "kris-key-123", "s3cr3t with spaces!", strings.Repeat("x", 512)} {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		if !strings.HasPrefix(enc, "enc:v1:") {
			t.Fatalf("encrypted %q missing enc:v1: prefix: %q", plain, enc)
		}
		if strings.Contains(enc, plain) && plain != "" {
			t.Fatalf("ciphertext leaks plaintext: %q", enc)
		}
		dec, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if dec != plain {
			t.Fatalf("roundtrip = %q, want %q", dec, plain)
		}
	}
}

func TestCrypterNonceRandom(t *testing.T) {
	c := NewCrypter("test-settings-enc-key")
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("two encryptions of the same value are identical (nonce reuse?)")
	}
}

func TestCrypterWrongKey(t *testing.T) {
	a := NewCrypter("key-one")
	b := NewCrypter("key-two")
	enc, err := a.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Decrypt(enc); err != setdomain.ErrDecryptFailed {
		t.Fatalf("wrong-key decrypt = %v, want ErrDecryptFailed", err)
	}
}

func TestCrypterTamper(t *testing.T) {
	c := NewCrypter("test-settings-enc-key")
	enc, err := c.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := c.Decrypt(tampered); err != setdomain.ErrDecryptFailed {
		t.Fatalf("tampered decrypt = %v, want ErrDecryptFailed", err)
	}
}

func TestCrypterRejectsPlaintext(t *testing.T) {
	c := NewCrypter("test-settings-enc-key")
	if _, err := c.Decrypt("just-plain"); err != setdomain.ErrNotEncrypted {
		t.Fatalf("plaintext decrypt = %v, want ErrNotEncrypted", err)
	}
	if got, err := c.Decrypt(""); err != nil || got != "" {
		t.Fatalf("empty decrypt = %q,%v, want \"\",nil", got, err)
	}
}
