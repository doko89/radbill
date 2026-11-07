// Package application hosts settings use cases: masked admin CRUD over
// key/value pairs with encrypted secrets, plus typed readers for jobs.
package application

import (
	"context"
	"errors"
	"strings"
	"time"

	setdomain "boilerplate/api/internal/settings/domain"
	"boilerplate/api/internal/shared/apperr"
)

// Service implements settings use cases.
type Service struct {
	repo  setdomain.Repository
	crypt setdomain.Crypter
	now   func() time.Time
}

// NewService builds a Service.
func NewService(repo setdomain.Repository, crypt setdomain.Crypter) *Service {
	return &Service{repo: repo, crypt: crypt, now: time.Now}
}

// List returns every setting with secret values replaced by "***".
func (s *Service) List(ctx context.Context) (map[string]string, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if setdomain.IsSecret(r.Key) {
			out[r.Key] = setdomain.MaskedSecret
			continue
		}
		out[r.Key] = r.Value
	}
	return out, nil
}

// Update upserts keys. Secrets are encrypted before storage; submitting "***"
// for a secret keeps the stored value, "" clears it. Known keys are validated.
func (s *Service) Update(ctx context.Context, vals map[string]string) (map[string]string, error) {
	now := s.now().UTC()
	for key, val := range vals {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, apperr.Invalid("key", "key must not be empty")
		}
		if setdomain.IsSecret(key) {
			if val == setdomain.MaskedSecret {
				continue
			}
			stored := ""
			if val != "" {
				enc, err := s.crypt.Encrypt(val)
				if err != nil {
					return nil, err
				}
				stored = enc
			}
			if _, err := s.repo.Upsert(ctx, key, stored, now); err != nil {
				return nil, err
			}
			continue
		}
		stored := val
		if trimmable(key) {
			stored = strings.TrimSpace(val)
		}
		if err := setdomain.ValidateValue(key, strings.TrimSpace(stored)); err != nil {
			return nil, apperr.Invalid(key, err.Error())
		}
		if _, err := s.repo.Upsert(ctx, key, stored, now); err != nil {
			return nil, err
		}
	}
	return s.List(ctx)
}

// trimmable reports whether surrounding whitespace is stripped before
// storage. Templates, unknown keys, and passwords keep their exact bytes.
func trimmable(key string) bool {
	switch key {
	case setdomain.KeyBillingCycleMode, setdomain.KeyBillingCycleDay,
		setdomain.KeyGraceDays, setdomain.KeyCompanyName,
		setdomain.KeyNotifProvider, setdomain.KeyNotifFrom,
		setdomain.KeySMTPHost, setdomain.KeySMTPPort, setdomain.KeySMTPUser,
		setdomain.KeyKriskrisBaseURL:
		return true
	default:
		return false
	}
}

// GetDecrypted returns one setting, decrypting secret values. It is for
// internal consumers (scheduler/notify); the HTTP API never exposes it.
func (s *Service) GetDecrypted(ctx context.Context, key string) (string, error) {
	row, err := s.repo.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if !setdomain.IsSecret(row.Key) {
		return row.Value, nil
	}
	if row.Value == "" {
		return "", nil
	}
	return s.crypt.Decrypt(row.Value)
}

// Typed returns the validated, defaulted view for scheduler and notify.
func (s *Service) Typed(ctx context.Context) (setdomain.Typed, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return setdomain.Typed{}, err
	}
	all := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Key == setdomain.KeySMTPPass {
			// SMTP needs the cleartext password; other secrets stay out
			// of the typed view.
			all[r.Key] = s.decryptLenient(r.Value)
			continue
		}
		if setdomain.IsSecret(r.Key) {
			continue
		}
		all[r.Key] = r.Value
	}
	return setdomain.TypedFromMap(all), nil
}

// decryptLenient decrypts a secret for internal consumers. Legacy plaintext
// values (stored before the key became secret) are returned as-is so old
// deployments keep working; re-saving the setting encrypts them.
func (s *Service) decryptLenient(stored string) string {
	if stored == "" {
		return ""
	}
	plain, err := s.crypt.Decrypt(stored)
	if err == nil {
		return plain
	}
	if errors.Is(err, setdomain.ErrNotEncrypted) {
		return stored
	}
	return ""
}
