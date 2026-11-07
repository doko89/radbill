package radius

import (
	"log/slog"
	"testing"
	"time"

	lradius "layeh.com/radius"
)

// TestDecideAuthG3 covers the Fase 3 decision matrix: remaining quota and
// time, every on_exhausted action, throttle (with and without the isolir
// fallback), auth-time FUP, and NAS binding.
func TestDecideAuthG3(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	future := now.Add(90 * time.Minute).Format(time.RFC3339) // 5400s

	quotaPkg := func(action string) *Package {
		return &Package{ID: "p1", RateLimit: "10M/10M", AddressPool: "pppoe-pool",
			QuotaBytes: 10 << 30, HasQuota: true, QuotaReset: "monthly",
			OnlineSecondsLimit: 3600, HasOnlineLimit: true,
			FUPThresholdBytes: 8 << 30, HasFUPThreshold: true, FUPRateLimit: "1M/1M",
			OnExhausted: action}
	}
	active := func() *Account {
		return &Account{ID: "a", Username: "u", Status: StatusActive, ExpiresAt: future}
	}

	tests := []struct {
		name  string
		acct  *Account
		pkg   *Package
		nasID string
		usage UsageSnapshot
		check func(t *testing.T, d AuthDecision)
	}{
		{
			name: "total limit carries remaining quota",
			acct: active(), pkg: quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedBytes: 3 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.HasTotalLimit || d.TotalLimit != 7<<30 {
					t.Fatalf("got %+v, want total limit 7GiB", d)
				}
				if d.RateLimit != "10M/10M" {
					t.Fatalf("got %+v, want normal rate below FUP", d)
				}
			},
		},
		{
			name: "topups extend remaining quota",
			acct: active(), pkg: quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedBytes: 12 << 30, TopupBytes: 5 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.HasTotalLimit || d.TotalLimit != 3<<30 {
					t.Fatalf("got %+v, want total limit 3GiB", d)
				}
			},
		},
		{
			name: "quota exhausted rejects",
			acct: active(), pkg: quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedBytes: 10 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept || d.ReplyMessage != "Quota exhausted" {
					t.Fatalf("got %+v, want quota reject", d)
				}
			},
		},
		{
			name: "quota exhausted isolates",
			acct: active(), pkg: quotaPkg(OnExhaustedIsolate),
			usage: UsageSnapshot{UsedBytes: 11 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.Isolated || d.AddressList != "isolir" {
					t.Fatalf("got %+v, want isolated accept", d)
				}
			},
		},
		{
			name: "quota exhausted throttles at FUP rate",
			acct: active(), pkg: quotaPkg(OnExhaustedThrottle),
			usage: UsageSnapshot{UsedBytes: 11 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || d.Isolated || !d.Throttled {
					t.Fatalf("got %+v, want throttled accept", d)
				}
				if d.RateLimit != "1M/1M" || d.AddressList != "" {
					t.Fatalf("got %+v, want FUP rate without address-list", d)
				}
				if d.HasTotalLimit || d.HasSessionTimeout {
					t.Fatalf("got %+v, want no caps on throttled accept", d)
				}
				if d.FramedPool != "pppoe-pool" || d.InterimInterval != 300 {
					t.Fatalf("got %+v, want pool+interim kept", d)
				}
			},
		},
		{
			name: "throttle without fup rate falls back to isolir rate",
			acct: active(),
			pkg: func() *Package {
				p := quotaPkg(OnExhaustedThrottle)
				p.FUPRateLimit = ""
				return p
			}(),
			usage: UsageSnapshot{UsedBytes: 11 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.Throttled || d.RateLimit != "128k/128k" {
					t.Fatalf("got %+v, want isolir-rate fallback", d)
				}
			},
		},
		{
			name: "session timeout is min of expires and online",
			acct: active(), pkg: quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedSeconds: 600}, // 3000s left < 5400s expires
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.HasSessionTimeout || d.SessionTimeout != 3000 {
					t.Fatalf("got %+v, want session-timeout 3000", d)
				}
			},
		},
		{
			name: "session timeout picks expires when smaller",
			acct: func() *Account {
				a := active()
				a.ExpiresAt = now.Add(30 * time.Minute).Format(time.RFC3339) // 1800s
				return a
			}(),
			pkg:   quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedSeconds: 600}, // 3000s left
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.HasSessionTimeout || d.SessionTimeout != 1800 {
					t.Fatalf("got %+v, want session-timeout 1800", d)
				}
			},
		},
		{
			name: "session timeout from online only",
			acct: func() *Account {
				a := active()
				a.ExpiresAt = ""
				return a
			}(),
			pkg:   quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedSeconds: 600},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.HasSessionTimeout || d.SessionTimeout != 3000 {
					t.Fatalf("got %+v, want session-timeout 3000", d)
				}
			},
		},
		{
			name: "online time exhausted rejects",
			acct: active(), pkg: quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedSeconds: 3600},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept || d.ReplyMessage != "Online time exhausted" {
					t.Fatalf("got %+v, want time reject", d)
				}
			},
		},
		{
			name: "online time exhausted isolates",
			acct: active(), pkg: quotaPkg(OnExhaustedIsolate),
			usage: UsageSnapshot{UsedSeconds: 9999},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.Isolated {
					t.Fatalf("got %+v, want isolated accept", d)
				}
			},
		},
		{
			name: "online time exhausted throttles",
			acct: active(), pkg: quotaPkg(OnExhaustedThrottle),
			usage: UsageSnapshot{UsedSeconds: 3600},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || !d.Throttled || d.RateLimit != "1M/1M" {
					t.Fatalf("got %+v, want throttled accept", d)
				}
			},
		},
		{
			name: "past FUP threshold starts at FUP rate with quota left",
			acct: active(), pkg: quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedBytes: 9 << 30}, // threshold 8GiB, quota 10GiB
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept || d.Isolated || d.Throttled {
					t.Fatalf("got %+v, want plain accept", d)
				}
				if d.RateLimit != "1M/1M" {
					t.Fatalf("got %+v, want FUP rate", d)
				}
				if !d.HasTotalLimit || d.TotalLimit != 1<<30 {
					t.Fatalf("got %+v, want remaining 1GiB", d)
				}
			},
		},
		{
			name: "expiry wins over quota exhaustion",
			acct: func() *Account {
				a := active()
				a.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339)
				return a
			}(),
			pkg:   quotaPkg(OnExhaustedReject),
			usage: UsageSnapshot{UsedBytes: 99 << 30},
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept || d.ReplyMessage != "Account expired" {
					t.Fatalf("got %+v, want expired reject", d)
				}
			},
		},
		{
			name: "nas binding match accepts",
			acct: func() *Account {
				a := active()
				a.NASID = "nas-1"
				return a
			}(),
			pkg:   quotaPkg(OnExhaustedReject),
			nasID: "nas-1",
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept {
					t.Fatalf("got %+v, want accept", d)
				}
			},
		},
		{
			name: "nas binding mismatch rejects",
			acct: func() *Account {
				a := active()
				a.NASID = "nas-1"
				return a
			}(),
			pkg:   quotaPkg(OnExhaustedReject),
			nasID: "nas-2",
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want reject", d)
				}
			},
		},
		{
			name:  "unbound account accepts any NAS",
			acct:  active(),
			pkg:   quotaPkg(OnExhaustedReject),
			nasID: "nas-9",
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if !d.Accept {
					t.Fatalf("got %+v, want accept", d)
				}
			},
		},
		{
			name: "bound account with unknown requesting NAS rejects",
			acct: func() *Account {
				a := active()
				a.NASID = "nas-1"
				return a
			}(),
			pkg:   quotaPkg(OnExhaustedReject),
			nasID: "",
			check: func(t *testing.T, d AuthDecision) {
				t.Helper()
				if d.Accept {
					t.Fatalf("got %+v, want fail-closed reject", d)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := decideTestInput()
			in.Account = tt.acct
			in.Package = tt.pkg
			in.NASID = tt.nasID
			in.Usage = tt.usage
			tt.check(t, decideAuth(in))
		})
	}
}

// TestAuthNASBinding exercises the binding end to end: a bound account
// accepts from its NAS and rejects from any other, while unbound accounts
// accept from every known NAS.
func TestAuthNASBinding(t *testing.T) {
	const otherIP = "127.0.0.2"
	cache := testCache()
	cache.storeNAS(&NAS{ID: "nas-other", Name: "other", IP: otherIP, Secret: testNASSecret, CoAPort: 3799})
	srv := &AuthServer{Cache: cache, Log: slog.New(slog.DiscardHandler)}
	serve := func(username, password, ip string) *lradius.Packet {
		cw := &captureWriter{}
		srv.ServeRADIUS(cw, testRequest(papPacket(t, username, password), ip))
		return cw.got
	}

	// Unbound alice accepts from both NAS.
	for _, ip := range []string{testNASIP, otherIP} {
		if got := serve("alice", "alicepw", ip); got == nil || got.Code != lradius.CodeAccessAccept {
			t.Fatalf("unbound alice from %s: got %+v, want Access-Accept", ip, got)
		}
	}
	// Bound to nas-test: home NAS accepts, the other rejects.
	cache.accounts["alice"].NASID = testNASID
	if got := serve("alice", "alicepw", testNASIP); got == nil || got.Code != lradius.CodeAccessAccept {
		t.Fatalf("bound alice from home NAS: got %+v, want Access-Accept", got)
	}
	if got := serve("alice", "alicepw", otherIP); got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("bound alice from other NAS: got %+v, want Access-Reject", got)
	}
}
