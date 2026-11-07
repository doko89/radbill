package application

import (
	"context"
	"errors"
	"testing"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	jwtinfra "boilerplate/api/internal/jwtauth/infrastructure"
)

type fakeAccounts struct {
	byName map[string]*authdomain.Account
	byID   map[string]*authdomain.Account
	custID map[string]string
}

func (f *fakeAccounts) GetByUsername(_ context.Context, u string) (*authdomain.Account, error) {
	if a, ok := f.byName[u]; ok {
		return a, nil
	}
	return nil, authdomain.ErrAccountNotFound
}

func (f *fakeAccounts) GetByID(_ context.Context, id string) (*authdomain.Account, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, authdomain.ErrAccountNotFound
}

func (f *fakeAccounts) Create(_ context.Context, a *authdomain.Account) error {
	f.byName[a.Username] = a
	f.byID[a.ID] = a
	return nil
}

func (f *fakeAccounts) UpdatePassword(_ context.Context, id, hash string, _ time.Time) error {
	f.byID[id].PasswordHash = hash
	return nil
}

func (f *fakeAccounts) UpdateStatus(_ context.Context, id, status string, _ time.Time) error {
	f.byID[id].Status = status
	return nil
}

func (f *fakeAccounts) Count(_ context.Context) (int64, error) { return int64(len(f.byID)), nil }

func (f *fakeAccounts) CustomerIDForAccount(_ context.Context, id string) (string, error) {
	return f.custID[id], nil
}

type fakeRefresh struct {
	byHash map[string]*authdomain.RefreshToken
}

func (f *fakeRefresh) Create(_ context.Context, t *authdomain.RefreshToken) error {
	f.byHash[t.TokenHash] = t
	return nil
}

func (f *fakeRefresh) GetByHash(_ context.Context, h string) (*authdomain.RefreshToken, error) {
	if t, ok := f.byHash[h]; ok {
		return t, nil
	}
	return nil, authdomain.ErrTokenInvalid
}

func (f *fakeRefresh) Revoke(_ context.Context, h string, now time.Time) error {
	if t, ok := f.byHash[h]; ok {
		t.RevokedAt = &now
	}
	return nil
}

func (f *fakeRefresh) RevokeAllForAccount(_ context.Context, id string, now time.Time) error {
	for _, t := range f.byHash {
		if t.AccountID == id {
			rev := now
			t.RevokedAt = &rev
		}
	}
	return nil
}

type fakeResets struct {
	byHash map[string]*authdomain.PasswordReset
}

func (f *fakeResets) Create(_ context.Context, r *authdomain.PasswordReset) error {
	f.byHash[r.TokenHash] = r
	return nil
}

func (f *fakeResets) GetByHash(_ context.Context, h string) (*authdomain.PasswordReset, error) {
	if r, ok := f.byHash[h]; ok {
		return r, nil
	}
	return nil, authdomain.ErrTokenInvalid
}

func (f *fakeResets) MarkUsed(_ context.Context, id string, now time.Time) error {
	for _, r := range f.byHash {
		if r.ID == id {
			r.UsedAt = &now
		}
	}
	return nil
}

func testService(t *testing.T) (*Service, *fakeAccounts) {
	t.Helper()
	hasher := jwtinfra.NewTestArgon2Hasher()
	hash, err := hasher.Hash("admin1234")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := time.Now().UTC()
	admin := &authdomain.Account{ID: "a1", Username: "admin", PasswordHash: hash, Role: authdomain.RoleAdmin, Status: authdomain.StatusActive, CreatedAt: now, UpdatedAt: now}
	disabledHash, _ := hasher.Hash("disabled12")
	disabled := &authdomain.Account{ID: "a2", Username: "off", PasswordHash: disabledHash, Role: authdomain.RoleUser, Status: authdomain.StatusDisabled, CreatedAt: now, UpdatedAt: now}
	accounts := &fakeAccounts{
		byName: map[string]*authdomain.Account{"admin": admin, "off": disabled},
		byID:   map[string]*authdomain.Account{"a1": admin, "a2": disabled},
		custID: map[string]string{},
	}
	svc := NewService(accounts, &fakeRefresh{byHash: map[string]*authdomain.RefreshToken{}},
		&fakeResets{byHash: map[string]*authdomain.PasswordReset{}},
		hasher, "test-jwt-secret-min-32-chars-ok", 15*time.Minute, 30*24*time.Hour)
	return svc, accounts
}

func TestLoginSuccess(t *testing.T) {
	svc, _ := testService(t)
	sess, raw, err := svc.Login(context.Background(), "admin", "admin1234", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if sess.AccessToken == "" || raw == "" || sess.User.Username != "admin" {
		t.Fatalf("bad session: %+v", sess)
	}
	if len(sess.User.Permissions) != 14 {
		t.Fatalf("admin perms = %d", len(sess.User.Permissions))
	}
	claims, err := svc.VerifyAccess(sess.AccessToken)
	if err != nil || claims.Subject != "a1" || claims.Role != authdomain.RoleAdmin {
		t.Fatalf("verify: %v %+v", err, claims)
	}
}

func TestLoginFailures(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  error
	}{
		{"wrong password", "admin", "nope", authdomain.ErrInvalidCredentials},
		{"unknown user", "ghost", "whatever", authdomain.ErrInvalidCredentials},
		{"disabled", "off", "disabled12", authdomain.ErrAccountDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := testService(t)
			if _, _, err := svc.Login(context.Background(), tt.username, tt.password, "10.0.0.1"); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoginLocksOut(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, _ = svc.Login(ctx, "admin", "wrong", "9.9.9.9")
	}
	if _, _, err := svc.Login(ctx, "admin", "admin1234", "9.9.9.9"); !errors.Is(err, authdomain.ErrTooManyAttempts) {
		t.Fatalf("err = %v, want ErrTooManyAttempts", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "admin1234", "9.9.9.10"); err != nil {
		t.Fatalf("other IP blocked: %v", err)
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	_, raw1, err := svc.Login(ctx, "admin", "admin1234", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_, raw2, err := svc.Refresh(ctx, raw1)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if raw1 == raw2 {
		t.Fatalf("refresh token not rotated")
	}
	if _, _, err := svc.Refresh(ctx, raw1); !errors.Is(err, authdomain.ErrTokenInvalid) {
		t.Fatalf("reuse = %v, want ErrTokenInvalid", err)
	}
	if _, _, err := svc.Refresh(ctx, raw2); !errors.Is(err, authdomain.ErrTokenInvalid) {
		t.Fatalf("family not revoked after reuse: %v", err)
	}
}

func TestLogoutIdempotent(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	_, raw, err := svc.Login(ctx, "admin", "admin1234", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.Logout(ctx, raw); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, _, err := svc.Refresh(ctx, raw); !errors.Is(err, authdomain.ErrTokenInvalid) {
		t.Fatalf("revoked token still refreshes: %v", err)
	}
	if err := svc.Logout(ctx, "unknown"); err != nil {
		t.Fatalf("logout unknown: %v", err)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	empty, err := svc.RequestPasswordReset(ctx, "ghost")
	if err != nil || empty != "" {
		t.Fatalf("unknown user leak: %q %v", empty, err)
	}
	raw, err := svc.RequestPasswordReset(ctx, "admin")
	if err != nil || raw == "" {
		t.Fatalf("request: %v", err)
	}
	if err := svc.ResetPassword(ctx, raw, "short"); err == nil {
		t.Fatalf("short password accepted")
	}
	if err := svc.ResetPassword(ctx, raw, "brand-new-password"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := svc.ResetPassword(ctx, raw, "another-password"); !errors.Is(err, authdomain.ErrTokenInvalid) {
		t.Fatalf("reuse = %v, want ErrTokenInvalid", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "brand-new-password", "127.0.0.1"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
}
