// Package domain holds the settings bounded-context core: key/value settings
// with encrypted secrets and a typed view for scheduler/notify consumers.
package domain

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaskedSecret is the placeholder shown by the API instead of secret values.
// Secrets are write-only: submitting MaskedSecret for a secret key leaves the
// stored value unchanged.
const MaskedSecret = "***"

// SecretKeys must be stored encrypted (enc:v1:...) and are never displayed.
// This set is a cross-agent contract (C2 kriskris reimplements the spec
// without importing this package); do not rename.
var SecretKeys = map[string]bool{
	"kriskris_api_key":         true,
	"kriskris_api_secret":      true,
	"kriskris_callback_secret": true,
	"smtp_pass":                true,
}

// IsSecret reports whether key is stored encrypted.
func IsSecret(key string) bool { return SecretKeys[key] }

// Known setting keys consumed by scheduler/notify/kriskris.
const (
	KeyBillingCycleMode = "billing_cycle_mode" // activation|fixed
	KeyBillingCycleDay  = "billing_cycle_day"  // 1..31
	KeyGraceDays        = "grace_days"         // >= 0
	KeyCompanyName      = "company_name"
	KeyNotifProvider    = "notif_provider" // log|smtp
	KeyNotifFrom        = "notif_from"
	KeySMTPHost         = "smtp_host"
	KeySMTPPort         = "smtp_port"
	KeySMTPUser         = "smtp_user"
	KeySMTPPass         = "smtp_pass"
	KeyKriskrisBaseURL  = "kriskris_base_url"
	KeyKriskrisMode     = "kriskris_mode" // webhook|poll
	KeyTplIssued        = "tpl_invoice_issued"
	KeyTplReminder      = "tpl_reminder"
	KeyTplIsolated      = "tpl_isolated"
	KeyTplPaid          = "tpl_paid"
)

// Billing cycle modes.
const (
	CycleActivation = "activation"
	CycleFixed      = "fixed"
)

// Notification providers.
const (
	NotifLog  = "log"
	NotifSMTP = "smtp"
)

// kriskris confirmation modes.
const (
	KriskrisWebhook = "webhook"
	KriskrisPoll    = "poll"
)

var (
	// ErrSettingNotFound is returned when the key does not exist.
	ErrSettingNotFound = errors.New("setting not found")
	// ErrNotEncrypted is returned when decrypting a value without the enc:v1: prefix.
	ErrNotEncrypted = errors.New("value is not encrypted")
	// ErrDecryptFailed is returned when decryption fails (wrong key or tampered data).
	ErrDecryptFailed = errors.New("decrypt failed")
)

// Setting is one raw key/value row. Value is the stored form: secrets are
// enc:v1:<base64(nonce|ciphertext)>, everything else is plain.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// Repository persists raw settings. Implemented in infrastructure.
type Repository interface {
	Get(ctx context.Context, key string) (Setting, error)
	List(ctx context.Context) ([]Setting, error)
	Upsert(ctx context.Context, key, value string, now time.Time) (Setting, error)
}

// Crypter encrypts and decrypts secret values. Implemented in infrastructure
// with AES-256-GCM; key = SHA256(SETTINGS_ENC_KEY).
type Crypter interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(stored string) (string, error)
}

// ValidateValue checks known keys. Unknown keys pass through so new settings
// never require a code change. Callers trim key/value before storing.
func ValidateValue(key, value string) error {
	switch key {
	case KeyBillingCycleMode:
		if value != CycleActivation && value != CycleFixed {
			return errors.New("must be activation|fixed")
		}
	case KeyBillingCycleDay:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 31 {
			return errors.New("must be an integer 1..31")
		}
	case KeyGraceDays:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return errors.New("must be an integer >= 0")
		}
	case KeyNotifProvider:
		if value != NotifLog && value != NotifSMTP {
			return errors.New("must be log|smtp")
		}
	case KeySMTPPort:
		if value == "" {
			return nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("must be an integer 1..65535")
		}
	case KeyKriskrisBaseURL:
		if value == "" {
			return nil
		}
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("must be an http(s) URL")
		}
	case KeyKriskrisMode:
		if value != KriskrisWebhook && value != KriskrisPoll {
			return errors.New("must be webhook|poll")
		}
	}
	return nil
}

// Typed is the validated, defaulted view consumed by scheduler and notify.
// Missing or invalid values fall back to defaults (see TypedFromMap) so a bad
// setting can never crash background jobs.
type Typed struct {
	BillingCycleMode string
	BillingCycleDay  int
	GraceDays        int
	CompanyName      string
	NotifProvider    string
	NotifFrom        string
	SMTPHost         string
	SMTPPort         int // 0 = unset (callers use 587)
	SMTPUser         string
	SMTPPass         string
	KriskrisBaseURL  string
	TplIssued        string
	TplReminder      string
	TplIsolated      string
	TplPaid          string
}

// DefaultTyped returns the defaults matching the schema seeds.
func DefaultTyped() Typed {
	return Typed{
		BillingCycleMode: CycleActivation,
		BillingCycleDay:  1,
		GraceDays:        3,
		CompanyName:      "Radbill",
		NotifProvider:    NotifLog,
		KriskrisBaseURL:  "https://kriskris.id",
		TplIssued:        "Halo {{.Nama}}, tagihan {{.Nomor}} sebesar Rp{{.Total}} telah terbit. Jatuh tempo {{.JatuhTempo}}. - {{.Perusahaan}}",
		TplReminder:      "Halo {{.Nama}}, tagihan {{.Nomor}} sebesar Rp{{.Total}} sudah jatuh tempo {{.JatuhTempo}}. Segera bayar agar layanan tidak diisolir. - {{.Perusahaan}}",
		TplIsolated:      "Halo {{.Nama}}, layanan {{.Username}} diisolir karena tagihan {{.Nomor}} belum lunas. Bayar untuk mengaktifkan kembali. - {{.Perusahaan}}",
		TplPaid:          "Halo {{.Nama}}, pembayaran tagihan {{.Nomor}} sebesar Rp{{.Total}} diterima. Terima kasih. - {{.Perusahaan}}",
	}
}

// TypedFromMap builds a Typed from decrypted key/value pairs, applying
// defaults for missing or invalid entries. It never fails.
func TypedFromMap(all map[string]string) Typed {
	t := DefaultTyped()
	if v, ok := all[KeyBillingCycleMode]; ok && (v == CycleActivation || v == CycleFixed) {
		t.BillingCycleMode = v
	}
	if v, ok := all[KeyBillingCycleDay]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 1 && n <= 31 {
			t.BillingCycleDay = n
		}
	}
	if v, ok := all[KeyGraceDays]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			t.GraceDays = n
		}
	}
	if v, ok := all[KeyCompanyName]; ok && v != "" {
		t.CompanyName = v
	}
	if v, ok := all[KeyNotifProvider]; ok && (v == NotifLog || v == NotifSMTP) {
		t.NotifProvider = v
	}
	if v, ok := all[KeyNotifFrom]; ok {
		t.NotifFrom = v
	}
	if v, ok := all[KeySMTPHost]; ok {
		t.SMTPHost = strings.TrimSpace(v)
	}
	if v, ok := all[KeySMTPPort]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 1 && n <= 65535 {
			t.SMTPPort = n
		}
	}
	if v, ok := all[KeySMTPUser]; ok {
		t.SMTPUser = v
	}
	if v, ok := all[KeySMTPPass]; ok {
		t.SMTPPass = v
	}
	if v, ok := all[KeyKriskrisBaseURL]; ok && v != "" {
		t.KriskrisBaseURL = v
	}
	if v, ok := all[KeyTplIssued]; ok && v != "" {
		t.TplIssued = v
	}
	if v, ok := all[KeyTplReminder]; ok && v != "" {
		t.TplReminder = v
	}
	if v, ok := all[KeyTplIsolated]; ok && v != "" {
		t.TplIsolated = v
	}
	if v, ok := all[KeyTplPaid]; ok && v != "" {
		t.TplPaid = v
	}
	return t
}
