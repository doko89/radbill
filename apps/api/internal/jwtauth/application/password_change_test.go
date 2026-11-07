package application

import (
	"context"
	"testing"

	authdomain "boilerplate/api/internal/jwtauth/domain"
)

func TestChangePassword(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	_, raw, err := svc.Login(ctx, "admin", "admin1234", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.ChangePassword(ctx, "a1", "admin1234", "brand-new-password"); err != nil {
		t.Fatalf("change: %v", err)
	}
	// Old password stops working, new one works.
	if _, _, err := svc.Login(ctx, "admin", "admin1234", "127.0.0.1"); err != authdomain.ErrInvalidCredentials {
		t.Fatalf("old password login = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "brand-new-password", "127.0.0.1"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
	// Pre-change refresh token was revoked.
	if _, _, err := svc.Refresh(ctx, raw); err != authdomain.ErrTokenInvalid {
		t.Fatalf("old refresh = %v, want ErrTokenInvalid", err)
	}
}

func TestChangePasswordFailures(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	if err := svc.ChangePassword(ctx, "a1", "wrong-current", "brand-new-password"); err != authdomain.ErrInvalidCredentials {
		t.Fatalf("wrong current = %v, want ErrInvalidCredentials", err)
	}
	if err := svc.ChangePassword(ctx, "a1", "admin1234", "short"); err == nil {
		t.Fatal("short new password accepted")
	}
	if err := svc.ChangePassword(ctx, "a2", "disabled12", "brand-new-password"); err != authdomain.ErrAccountDisabled {
		t.Fatalf("disabled = %v, want ErrAccountDisabled", err)
	}
	if err := svc.ChangePassword(ctx, "ghost", "whatever", "brand-new-password"); err != authdomain.ErrAccountNotFound {
		t.Fatalf("unknown = %v, want ErrAccountNotFound", err)
	}
}
