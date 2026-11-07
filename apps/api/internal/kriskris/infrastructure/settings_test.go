package kriskrisinfra_test

import (
	"strings"
	"testing"

	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
	kriskristest "boilerplate/api/internal/kriskris/testkit"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	stored, err := kriskrisinfra.EncryptValue("operator-key-123", "sk-live-abc")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("stored = %q, want enc:v1: prefix", stored)
	}
	plain, err := kriskrisinfra.DecryptValue("operator-key-123", stored)
	if err != nil || plain != "sk-live-abc" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	if _, err := kriskrisinfra.DecryptValue("wrong-key", stored); err == nil {
		t.Fatal("wrong key should fail")
	}
	if _, err := kriskrisinfra.DecryptValue("", stored); err == nil {
		t.Fatal("empty key should fail")
	}
	if _, err := kriskrisinfra.EncryptValue("", "x"); err == nil {
		t.Fatal("encrypt with empty key should fail")
	}
	// Nonces differ per encryption.
	again, _ := kriskrisinfra.EncryptValue("operator-key-123", "sk-live-abc")
	if again == stored {
		t.Fatal("expected random nonce")
	}
}

func TestLoadConfigEnvWins(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	env := map[string]string{
		"KRISKRIS_BASE_URL":        "https://env.example",
		"KRISKRIS_API_KEY":         "env-key",
		"KRISKRIS_API_SECRET":      "env-secret",
		"KRISKRIS_CALLBACK_SECRET": "env-cb",
	}
	cfg, err := kriskrisinfra.LoadConfigWithEnv(pool, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.BaseURL != "https://env.example" || cfg.APIKey != "env-key" ||
		cfg.APISecret != "env-secret" || cfg.CallbackSecret != "env-cb" {
		t.Fatalf("cfg = %+v", cfg.Redacted())
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestLoadConfigSettingsFallback(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	encKey := "settings-enc-key"
	mustEnc := func(plain string) string {
		t.Helper()
		stored, err := kriskrisinfra.EncryptValue(encKey, plain)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		return stored
	}
	kriskristest.SetSetting(t, pool, "kriskris_base_url", "https://settings.example")
	kriskristest.SetSetting(t, pool, "kriskris_api_key", mustEnc("db-key"))
	kriskristest.SetSetting(t, pool, "kriskris_api_secret", mustEnc("db-secret"))
	kriskristest.SetSetting(t, pool, "kriskris_callback_secret", mustEnc("db-cb"))
	cfg, err := kriskrisinfra.LoadConfigWithEnv(pool, func(k string) string {
		if k == "SETTINGS_ENC_KEY" {
			return encKey
		}
		return ""
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.BaseURL != "https://settings.example" || cfg.APIKey != "db-key" ||
		cfg.APISecret != "db-secret" || cfg.CallbackSecret != "db-cb" {
		t.Fatalf("cfg = %+v", cfg.Redacted())
	}
	// Wrong SETTINGS_ENC_KEY surfaces a decrypt error, not silent garbage.
	if _, err := kriskrisinfra.LoadConfigWithEnv(pool, func(k string) string {
		if k == "SETTINGS_ENC_KEY" {
			return "wrong"
		}
		return ""
	}); err == nil {
		t.Fatal("wrong enc key should error")
	}
}

func TestLoadConfigMode(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	base := map[string]string{
		"KRISKRIS_BASE_URL":   "https://env.example",
		"KRISKRIS_API_KEY":    "env-key",
		"KRISKRIS_API_SECRET": "env-secret",
	}
	// Poll mode needs no callback secret.
	env := map[string]string{}
	for k, v := range base {
		env[k] = v
	}
	env["KRISKRIS_MODE"] = "poll"
	cfg, err := kriskrisinfra.LoadConfigWithEnv(pool, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Mode != kriskrisinfra.ModePoll {
		t.Fatalf("mode = %q", cfg.Mode)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("poll without cb secret should validate: %v", err)
	}
	// Webhook (default) still requires it.
	cfg2, err := kriskrisinfra.LoadConfigWithEnv(pool, func(k string) string { return base[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg2.Mode != "" {
		t.Fatalf("unset mode = %q", cfg2.Mode)
	}
	if err := cfg2.Validate(); err == nil {
		t.Fatal("webhook without cb secret should not validate")
	}
	// Bad mode rejected.
	bad := kriskrisinfra.Config{BaseURL: "x", APIKey: "k", APISecret: "s", CallbackSecret: "c", Mode: "smoke"}
	if err := bad.Validate(); err == nil {
		t.Fatal("bad mode should not validate")
	}
	// Mode falls back to the settings table.
	kriskristest.SetSetting(t, pool, "kriskris_mode", "poll")
	cfg3, err := kriskrisinfra.LoadConfigWithEnv(pool, func(k string) string { return base[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg3.Mode != kriskrisinfra.ModePoll {
		t.Fatalf("table mode = %q", cfg3.Mode)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	cfg, err := kriskrisinfra.LoadConfigWithEnv(pool, func(string) string { return "" })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("empty config should not validate")
	}
	if cfg.Redacted().APIKey != "" {
		t.Fatal("redacted empty should stay empty")
	}
}
