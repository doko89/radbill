package domain

import (
	"errors"
	"testing"
	"time"
)

func testPPPoE(status string) *PPPoE {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	p, err := NewPPPoE("p1", Details{
		CustomerID: "c1", PackageID: "pkg1", Username: "user01", Password: "secret1",
	}, now)
	if err != nil {
		panic(err)
	}
	p.Status = status
	return p
}

func TestStatusTransitions(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		from   string
		action string
		want   string
		wantOK bool
	}{
		{"isolate active", StatusActive, "isolate", StatusIsolated, true},
		{"isolate isolated idempotent", StatusIsolated, "isolate", StatusIsolated, true},
		{"isolate disabled rejected", StatusDisabled, "isolate", StatusDisabled, false},
		{"activate isolated", StatusIsolated, "activate", StatusActive, true},
		{"activate disabled", StatusDisabled, "activate", StatusActive, true},
		{"activate active noop", StatusActive, "activate", StatusActive, true},
		{"disable active", StatusActive, "disable", StatusDisabled, true},
		{"disable isolated", StatusIsolated, "disable", StatusDisabled, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPPPoE(tt.from)
			var err error
			switch tt.action {
			case "isolate":
				err = p.Isolate(now)
			case "activate":
				err = p.Activate(now)
			case "disable":
				p.Disable(now)
			}
			if tt.wantOK && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tt.wantOK && !errors.Is(err, ErrBadTransition) {
				t.Fatalf("err = %v, want ErrBadTransition", err)
			}
			if p.Status != tt.want {
				t.Fatalf("status = %q, want %q", p.Status, tt.want)
			}
		})
	}
}

func TestExtendAndChangePackage(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	p := testPPPoE(StatusActive)
	if err := p.Extend(now.Add(-time.Hour), now); err == nil {
		t.Fatalf("past expiry accepted")
	}
	future := now.Add(30 * 24 * time.Hour)
	if err := p.Extend(future, now); err != nil {
		t.Fatalf("extend: %v", err)
	}
	if !p.ExpiresAt.Equal(future) {
		t.Fatalf("expiry = %v", p.ExpiresAt)
	}
	if err := p.ChangePackage("", now); err == nil {
		t.Fatalf("empty package accepted")
	}
	if err := p.ChangePackage("pkg2", now); err != nil || p.PackageID != "pkg2" {
		t.Fatalf("change package: %v %q", err, p.PackageID)
	}
}

func TestNewPPPoEValidation(t *testing.T) {
	now := time.Now().UTC()
	base := Details{CustomerID: "c1", PackageID: "pkg1", Username: "user01", Password: "secret1"}
	cases := []struct {
		name   string
		mutate func(*Details)
	}{
		{"short username", func(d *Details) { d.Username = "ab" }},
		{"short password", func(d *Details) { d.Password = "abc" }},
		{"missing customer", func(d *Details) { d.CustomerID = "" }},
		{"bad static ip", func(d *Details) { d.StaticIP = "999.1.1.1" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			d := base
			tt.mutate(&d)
			if _, err := NewPPPoE("p1", d, now); err == nil {
				t.Fatalf("invalid input accepted")
			}
		})
	}
}
