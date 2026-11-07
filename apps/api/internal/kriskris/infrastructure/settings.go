package kriskrisinfra

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Settings/secret contract (spec identical to C1, no shared import):
//
//   - AES-256-GCM with key = SHA256(SETTINGS_ENC_KEY).
//   - Stored format: "enc:v1:<base64(nonce|ciphertext)>" (12-byte GCM nonce).
//   - Encrypted keys: kriskris_api_key, kriskris_api_secret,
//     kriskris_callback_secret. Plain key: kriskris_base_url.
//   - Env overrides (when set non-empty, env wins — handy for tests):
//     KRISKRIS_API_KEY, KRISKRIS_API_SECRET, KRISKRIS_CALLBACK_SECRET,
//     KRISKRIS_BASE_URL, KRISKRIS_MODE. SETTINGS_ENC_KEY supplies the
//     decryption key.
const (
	SettingAPIKey       = "kriskris_api_key"
	SettingAPISecret    = "kriskris_api_secret"
	SettingCallbackSec  = "kriskris_callback_secret"
	SettingBaseURL      = "kriskris_base_url"
	SettingMode         = "kriskris_mode"
	encPrefix           = "enc:v1:"
	settingsReadTimeout = 5 * time.Second
)

// Confirmation modes (guide Bab 2): webhook (callback primary + poller
// backup, needs a public URL) or poll (poller only, for private networks
// that cannot receive callbacks).
const (
	ModeWebhook = "webhook"
	ModePoll    = "poll"
)

// Config is the resolved kriskris configuration.
type Config struct {
	BaseURL        string
	APIKey         string
	APISecret      string
	CallbackSecret string
	// Mode is ModeWebhook or ModePoll (empty behaves as webhook).
	Mode string
}

// Validate requires every field (kriskris cannot operate half-configured).
// Poll mode drops the callback-secret requirement (no webhooks to verify).
func (c Config) Validate() error {
	switch {
	case c.BaseURL == "":
		return errors.New("kriskris: missing base url (KRISKRIS_BASE_URL / kriskris_base_url)")
	case c.APIKey == "":
		return errors.New("kriskris: missing api key (KRISKRIS_API_KEY / kriskris_api_key)")
	case c.APISecret == "":
		return errors.New("kriskris: missing api secret (KRISKRIS_API_SECRET / kriskris_api_secret)")
	case c.Mode != "" && c.Mode != ModeWebhook && c.Mode != ModePoll:
		return fmt.Errorf("kriskris: bad mode %q (KRISKRIS_MODE / kriskris_mode must be webhook|poll)", c.Mode)
	case c.Mode != ModePoll && c.CallbackSecret == "":
		return errors.New("kriskris: missing callback secret (KRISKRIS_CALLBACK_SECRET / kriskris_callback_secret)")
	}
	return nil
}

// Redacted returns c with secrets masked, safe for logs.
func (c Config) Redacted() Config {
	mask := func(s string) string {
		if s == "" {
			return ""
		}
		return "***"
	}
	out := c
	out.APIKey = mask(c.APIKey)
	out.APISecret = mask(c.APISecret)
	out.CallbackSecret = mask(c.CallbackSecret)
	return out
}

// LoadConfig resolves Config from env first, then the settings table.
func LoadConfig(pool *sql.DB) (Config, error) {
	return LoadConfigWithEnv(pool, os.Getenv)
}

// LoadConfigWithEnv is LoadConfig with an injectable env lookup (tests).
func LoadConfigWithEnv(pool *sql.DB, getenv func(string) string) (Config, error) {
	cfg := Config{BaseURL: strings.TrimSpace(getenv("KRISKRIS_BASE_URL"))}
	if cfg.BaseURL == "" && pool != nil {
		v, err := readSetting(pool, SettingBaseURL)
		if err != nil {
			return Config{}, err
		}
		cfg.BaseURL = strings.TrimSpace(v)
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(getenv("KRISKRIS_MODE")))
	if cfg.Mode == "" && pool != nil {
		v, err := readSetting(pool, SettingMode)
		if err != nil {
			return Config{}, err
		}
		cfg.Mode = strings.ToLower(strings.TrimSpace(v))
	}
	encKey := getenv("SETTINGS_ENC_KEY")
	for _, item := range []struct {
		envKey, settingKey string
		dest               *string
	}{
		{"KRISKRIS_API_KEY", SettingAPIKey, &cfg.APIKey},
		{"KRISKRIS_API_SECRET", SettingAPISecret, &cfg.APISecret},
		{"KRISKRIS_CALLBACK_SECRET", SettingCallbackSec, &cfg.CallbackSecret},
	} {
		if v := getenv(item.envKey); v != "" {
			*item.dest = v
			continue
		}
		if pool == nil {
			continue
		}
		stored, err := readSetting(pool, item.settingKey)
		if err != nil {
			return Config{}, err
		}
		if stored == "" {
			continue
		}
		plain, err := DecryptValue(encKey, stored)
		if err != nil {
			return Config{}, fmt.Errorf("kriskris: decrypt %s: %w", item.settingKey, err)
		}
		*item.dest = plain
	}
	return cfg, nil
}

func readSetting(pool *sql.DB, key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), settingsReadTimeout)
	defer cancel()
	var v string
	if err := pool.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("kriskris: read setting %s: %w", key, err)
	}
	return v, nil
}

// EncryptValue encrypts plaintext for the settings table. Exported so tests
// and operator tooling can produce valid enc:v1 values.
func EncryptValue(encKey, plaintext string) (string, error) {
	aead, err := gcmFor(encKey)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("kriskris: rand nonce: %w", err)
	}
	sealed := aead.Seal(append([]byte{}, nonce...), nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptValue decrypts an enc:v1:... settings value. A value without the
// prefix is returned as-is (treats legacy plain values as readable rather
// than failing closed at the wrong layer — callers still Validate).
func DecryptValue(encKey, stored string) (string, error) {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored, nil
	}
	aead, err := gcmFor(encKey)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", fmt.Errorf("kriskris: base64 settings value: %w", err)
	}
	if len(raw) < aead.NonceSize() {
		return "", errors.New("kriskris: settings value too short")
	}
	nonce, ct := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errors.New("kriskris: settings decrypt failed (wrong SETTINGS_ENC_KEY?)")
	}
	return string(plain), nil
}

func gcmFor(encKey string) (cipher.AEAD, error) {
	if encKey == "" {
		return nil, errors.New("kriskris: SETTINGS_ENC_KEY is empty")
	}
	sum := sha256.Sum256([]byte(encKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
