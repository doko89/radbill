package domain

import (
	"testing"
	"time"
)

func validDetails() Details {
	return Details{Name: "Basic 10M", Price: 100000, Period: "monthly"}
}

func TestPackageTypeValidation(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty legacy defaults to time", input: "", want: TypeTime},
		{name: "time kept", input: TypeTime, want: TypeTime},
		{name: "quota kept", input: TypeQuota, want: TypeQuota},
		{name: "fup kept", input: TypeFUP, want: TypeFUP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := validDetails()
			d.Type = tc.input
			p, err := NewPackage("pkg-1", d, now)
			if err != nil {
				t.Fatalf("NewPackage: %v", err)
			}
			if p.Type != tc.want {
				t.Fatalf("Type = %q, want %q", p.Type, tc.want)
			}
		})
	}
}

func TestPackageTypeInvalid(t *testing.T) {
	d := validDetails()
	d.Type = "unlimited"
	if _, err := NewPackage("pkg-1", d, time.Now().UTC()); err == nil {
		t.Fatal("expected error for unknown type")
	}
}
