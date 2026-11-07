package application

import (
	"context"
	"strings"
	"testing"
	"time"

	setdomain "boilerplate/api/internal/settings/domain"
	setinfra "boilerplate/api/internal/settings/infrastructure"
	"boilerplate/api/internal/shared/apperr"
)

type fakeRepo struct {
	rows map[string]string
}

func (f *fakeRepo) Get(_ context.Context, key string) (setdomain.Setting, error) {
	v, ok := f.rows[key]
	if !ok {
		return setdomain.Setting{}, setdomain.ErrSettingNotFound
	}
	return setdomain.Setting{Key: key, Value: v}, nil
}

func (f *fakeRepo) List(_ context.Context) ([]setdomain.Setting, error) {
	out := make([]setdomain.Setting, 0, len(f.rows))
	for k, v := range f.rows {
		out = append(out, setdomain.Setting{Key: k, Value: v})
	}
	return out, nil
}

func (f *fakeRepo) Upsert(_ context.Context, key, value string, _ time.Time) (setdomain.Setting, error) {
	f.rows[key] = value
	return setdomain.Setting{Key: key, Value: value}, nil
}

func newService(rows map[string]string) (*Service, *fakeRepo) {
	repo := &fakeRepo{rows: rows}
	return NewService(repo, setinfra.NewCrypter("test-enc-key")), repo
}

func TestListMasksSecrets(t *testing.T) {
	svc, _ := newService(map[string]string{
		"grace_days":       "5",
		"kriskris_api_key": "enc:v1:AAAA",
	})
	out, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out["grace_days"] != "5" {
		t.Fatalf("plain value = %q", out["grace_days"])
	}
	if out["kriskris_api_key"] != "***" {
		t.Fatalf("secret value = %q, want masked", out["kriskris_api_key"])
	}
}

func TestUpdateEncryptsSecrets(t *testing.T) {
	svc, repo := newService(map[string]string{})
	if _, err := svc.Update(context.Background(), map[string]string{
		"kriskris_api_key": "super-secret",
		"grace_days":       "7",
	}); err != nil {
		t.Fatal(err)
	}
	stored := repo.rows["kriskris_api_key"]
	if !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("secret stored plain: %q", stored)
	}
	if repo.rows["grace_days"] != "7" {
		t.Fatalf("plain stored = %q", repo.rows["grace_days"])
	}
	dec, err := svc.GetDecrypted(context.Background(), "kriskris_api_key")
	if err != nil || dec != "super-secret" {
		t.Fatalf("decrypted = %q,%v", dec, err)
	}
	masked, err := svc.List(context.Background())
	if err != nil || masked["kriskris_api_key"] != "***" {
		t.Fatalf("listed secret = %q,%v", masked["kriskris_api_key"], err)
	}
}

func TestUpdateMaskedKeepsStored(t *testing.T) {
	svc, repo := newService(map[string]string{})
	if _, err := svc.Update(context.Background(), map[string]string{"kriskris_api_key": "first"}); err != nil {
		t.Fatal(err)
	}
	before := repo.rows["kriskris_api_key"]
	if _, err := svc.Update(context.Background(), map[string]string{"kriskris_api_key": "***"}); err != nil {
		t.Fatal(err)
	}
	if repo.rows["kriskris_api_key"] != before {
		t.Fatal("submitting *** overwrote the stored secret")
	}
}

func TestUpdateValidation(t *testing.T) {
	svc, _ := newService(map[string]string{})
	for key, val := range map[string]string{
		"billing_cycle_mode": "weekly",
		"billing_cycle_day":  "32",
		"grace_days":         "-1",
		"notif_provider":     "wa",
	} {
		if _, err := svc.Update(context.Background(), map[string]string{key: val}); err == nil {
			t.Fatalf("%s=%q accepted", key, val)
		} else if _, ok := err.(*apperr.FieldError); !ok {
			t.Fatalf("%s=%q err type = %T, want FieldError", key, val, err)
		}
	}
	if _, err := svc.Update(context.Background(), map[string]string{"my_future_key": "anything"}); err != nil {
		t.Fatalf("unknown key rejected: %v", err)
	}
}

func TestTypedDefaultsOnBadValues(t *testing.T) {
	svc, _ := newService(map[string]string{
		"billing_cycle_mode": "bogus",
		"grace_days":         "NaN",
		"billing_cycle_day":  "99",
		"notif_provider":     "bogus",
	})
	typed, err := svc.Typed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := setdomain.DefaultTyped()
	if typed.BillingCycleMode != want.BillingCycleMode || typed.GraceDays != want.GraceDays ||
		typed.BillingCycleDay != want.BillingCycleDay || typed.NotifProvider != want.NotifProvider {
		t.Fatalf("bad values did not fall back to defaults: %+v", typed)
	}
	if typed.TplIssued == "" || !strings.Contains(typed.TplIssued, "{{.Nama}}") {
		t.Fatalf("default template missing: %q", typed.TplIssued)
	}
}
