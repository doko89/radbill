package radius

import (
	"testing"
	"time"
)

func decideTestInput() AuthInput {
	return AuthInput{
		InterimInterval:   300,
		IsolirRateLimit:   "128k/128k",
		IsolirAddressList: "isolir",
		Now:               time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC),
	}
}

func TestDecideAuth(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	future := now.Add(90 * time.Minute).Format(time.RFC3339)
	past := now.Add(-time.Hour).Format(time.RFC3339)

	fullPkg := &Package{
		ID: "p1", RateLimit: "10M/10M", AddressPool: "pppoe-pool",
		QuotaBytes: 10 << 30, HasQuota: true, OnExhausted: OnExhaustedReject,
	}

	tests := []struct {
		name  string
		acct  *Account
		pkg   *Package
		check func(t *testing.T, d AuthDecision)
	}{
		{
			name: "unknown user rejects",
			acct: nil, pkg: fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept || d.ReplyMessage == "" {
					t.Fatalf("got %+v, want reject with message", d)
				}
			},
		},
		{
			name: "disabled rejects",
			acct: &Account{ID: "a", Username: "u", Status: StatusDisabled}, pkg: fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want reject", d)
				}
			},
		},
		{
			name: "unknown status rejects",
			acct: &Account{ID: "a", Username: "u", Status: "weird"}, pkg: fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want reject", d)
				}
			},
		},
		{
			name: "isolated accepts with isolir attributes",
			acct: &Account{ID: "a", Username: "u", Status: StatusIsolated}, pkg: fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.Isolated {
					t.Fatalf("got %+v, want isolated accept", d)
				}
				if d.RateLimit != "128k/128k" || d.AddressList != "isolir" {
					t.Fatalf("got %+v, want isolir rate+list", d)
				}
				if d.InterimInterval != 300 || d.HasSessionTimeout || d.HasTotalLimit {
					t.Fatalf("got %+v, want interim only", d)
				}
			},
		},
		{
			name: "active minimal accepts",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive}, pkg: nil,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || d.Isolated {
					t.Fatalf("got %+v, want plain accept", d)
				}
				if d.RateLimit != "" || d.FramedPool != "" || d.FramedIP != "" {
					t.Fatalf("got %+v, want no package attrs", d)
				}
				if d.InterimInterval != 300 {
					t.Fatalf("got %+v, want interim 300", d)
				}
			},
		},
		{
			name: "active full carries package attributes",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, StaticIP: "10.0.0.5", ExpiresAt: future},
			pkg:  fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || d.Isolated {
					t.Fatalf("got %+v, want accept", d)
				}
				if d.RateLimit != "10M/10M" || d.FramedPool != "pppoe-pool" || d.FramedIP != "10.0.0.5" {
					t.Fatalf("got %+v, want rate+pool+ip", d)
				}
				if !d.HasSessionTimeout || d.SessionTimeout != 5400 {
					t.Fatalf("got %+v, want session-timeout 5400", d)
				}
				if !d.HasTotalLimit || d.TotalLimit != 10<<30 {
					t.Fatalf("got %+v, want total limit 10GiB", d)
				}
			},
		},
		{
			name: "expired with reject policy rejects",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: past},
			pkg:  fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept || d.ReplyMessage != "Account expired" {
					t.Fatalf("got %+v, want expired reject", d)
				}
			},
		},
		{
			name: "expired with isolate policy accepts isolated",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: past},
			pkg:  &Package{ID: "p1", OnExhausted: OnExhaustedIsolate},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.Isolated || d.AddressList != "isolir" {
					t.Fatalf("got %+v, want isolated accept", d)
				}
				if d.HasSessionTimeout {
					t.Fatalf("got %+v, want no session-timeout when expired", d)
				}
			},
		},
		{
			// G3 change (was: throttle fell back to isolate in Fase 1):
			// throttle now accepts at the degraded rate with no caps and
			// no address-list.
			name: "expired with throttle policy accepts throttled",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: past},
			pkg:  &Package{ID: "p1", OnExhausted: OnExhaustedThrottle, FUPRateLimit: "1M/1M"},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || d.Isolated || !d.Throttled {
					t.Fatalf("got %+v, want throttled accept", d)
				}
				if d.RateLimit != "1M/1M" || d.AddressList != "" {
					t.Fatalf("got %+v, want FUP rate without address-list", d)
				}
				if d.HasSessionTimeout || d.HasTotalLimit {
					t.Fatalf("got %+v, want no caps on throttled accept", d)
				}
			},
		},
		{
			name: "expired without package rejects",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: past},
			pkg:  nil,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want reject", d)
				}
			},
		},
		{
			name: "unparseable expires_at rejects",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: "not-a-time"},
			pkg:  fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want reject", d)
				}
			},
		},
		{
			name: "expiry exactly now rejects",
			acct: &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: now.Format(time.RFC3339)},
			pkg:  fullPkg,
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want reject", d)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := decideTestInput()
			in.Account = tt.acct
			in.Package = tt.pkg
			tt.check(t, decideAuth(in))
		})
	}
}

func TestSplitLimit(t *testing.T) {
	tests := []struct {
		limit   uint64
		low, gw uint32
	}{
		{0, 0, 0},
		{1000, 1000, 0},
		{0xffffffff, 0xffffffff, 0},
		{0x100000000, 0, 1},
		{10 << 30, 0x80000000, 2},
		{0x1_00000002, 2, 1},
	}
	for _, tt := range tests {
		low, gw := splitLimit(tt.limit)
		if low != tt.low || gw != tt.gw {
			t.Fatalf("splitLimit(%d) = (%d,%d), want (%d,%d)", tt.limit, low, gw, tt.low, tt.gw)
		}
	}
}
