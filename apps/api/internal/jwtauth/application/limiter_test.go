package application

import (
	"errors"
	"testing"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
)

func TestLoginLimiterLocksAfterFive(t *testing.T) {
	l := NewLoginLimiter(5, 15*time.Minute)
	for i := 0; i < 5; i++ {
		if err := l.Check("bob", "1.2.3.4"); err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
		l.Fail("bob", "1.2.3.4")
	}
	if err := l.Check("bob", "1.2.3.4"); !errors.Is(err, authdomain.ErrTooManyAttempts) {
		t.Fatalf("6th check = %v, want ErrTooManyAttempts", err)
	}
}

func TestLoginLimiterIsolatedPerKey(t *testing.T) {
	l := NewLoginLimiter(2, 15*time.Minute)
	l.Fail("bob", "1.2.3.4")
	l.Fail("bob", "1.2.3.4")
	if err := l.Check("bob", "1.2.3.4"); !errors.Is(err, authdomain.ErrTooManyAttempts) {
		t.Fatalf("locked pair not locked: %v", err)
	}
	if err := l.Check("bob", "5.6.7.8"); err != nil {
		t.Fatalf("other IP locked: %v", err)
	}
	if err := l.Check("alice", "1.2.3.4"); err != nil {
		t.Fatalf("other user locked: %v", err)
	}
}

func TestLoginLimiterResetAndWindow(t *testing.T) {
	now := time.Now()
	l := NewLoginLimiter(2, 15*time.Minute)
	l.now = func() time.Time { return now }

	l.Fail("bob", "ip")
	l.Fail("bob", "ip")
	if err := l.Check("bob", "ip"); !errors.Is(err, authdomain.ErrTooManyAttempts) {
		t.Fatalf("want locked, got %v", err)
	}
	l.Reset("bob", "ip")
	if err := l.Check("bob", "ip"); err != nil {
		t.Fatalf("want unlocked after reset, got %v", err)
	}

	l.Fail("bob", "ip")
	l.Fail("bob", "ip")
	now = now.Add(16 * time.Minute)
	if err := l.Check("bob", "ip"); err != nil {
		t.Fatalf("want unlocked after window, got %v", err)
	}
	l.Fail("bob", "ip")
	if err := l.Check("bob", "ip"); err != nil {
		t.Fatalf("fresh window should allow 1 fail, got %v", err)
	}
}
