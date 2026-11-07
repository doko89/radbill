package radius

import "time"

// AuthInput is the cached data needed for one authentication decision.
// It carries no framework types so the decision stays a pure function.
type AuthInput struct {
	Account *Account // nil = unknown username
	Package *Package // nil when the account's package row is missing

	// NASID is the id of the requesting NAS. Accounts with nas_id set only
	// authenticate from that NAS; accounts without bind accept any NAS.
	NASID string
	// Usage is the account mirror reading for the running bucket (zero
	// value = fresh account: full quota, full time, no top-ups).
	Usage UsageSnapshot

	InterimInterval   uint32
	IsolirRateLimit   string
	IsolirAddressList string

	Now time.Time
}

// AuthDecision is the result of decideAuth. The auth handler translates it
// to an Access-Accept or Access-Reject packet.
type AuthDecision struct {
	Accept       bool
	ReplyMessage string // set on reject (and never contains secrets)
	Isolated     bool
	Throttled    bool // exhausted accept at the (FUP or isolir) degraded rate

	RateLimit         string // Mikrotik-Rate-Limit ("" = omit)
	AddressList       string // Mikrotik-Address-List ("" = omit)
	FramedPool        string // Framed-Pool ("" = omit)
	FramedIP          string // Framed-IP-Address ("" = omit)
	SessionTimeout    uint32
	HasSessionTimeout bool
	InterimInterval   uint32
	TotalLimit        uint64 // Mikrotik-Total-Limit in bytes
	HasTotalLimit     bool
}

func reject(msg string) AuthDecision {
	return AuthDecision{Accept: false, ReplyMessage: msg}
}

// decideAuth maps account + package + settings + usage to accept/reject
// plus reply attributes. Pure: no I/O, no logging, no framework imports.
//
// Rules (PRD 7.1, 7.3):
//   - unknown username, disabled status, or NAS binding mismatch: Reject.
//   - isolated: Accept with isolir attributes from settings.
//   - expired (expires_at passed), quota exhausted, or online time
//     exhausted: follow the package on_exhausted action (reject rejects,
//     isolate accepts isolated, throttle accepts at the degraded rate).
//   - active with remaining quota/time: Accept with package attributes;
//     Session-Timeout is the minimum of the remaining expires_at and
//     online time when either is set; Mikrotik-Total-Limit carries the
//     REMAINING quota (quota + top-ups - used); past the FUP threshold
//     the session starts at the FUP rate.
func decideAuth(in AuthInput) AuthDecision {
	if in.Account == nil {
		return reject("Access denied")
	}
	acct := in.Account
	if acct.NASID != "" && in.NASID != acct.NASID {
		return reject("Access denied")
	}
	switch acct.Status {
	case StatusDisabled:
		return reject("Access denied")
	case StatusIsolated:
		return isolatedAccept(in)
	case StatusActive:
		// Continue below.
	default:
		return reject("Access denied")
	}

	if expired, _ := expiry(acct.ExpiresAt, in.Now); expired {
		return exhaustedDecision(in, "Account expired")
	}
	if reason, exhausted := exhaustedReason(in.Package, in.Usage); exhausted {
		return exhaustedDecision(in, reason)
	}

	d := AuthDecision{
		Accept:          true,
		FramedIP:        acct.StaticIP,
		InterimInterval: in.InterimInterval,
	}
	if in.Package != nil {
		p := in.Package
		d.RateLimit = p.RateLimit
		d.FramedPool = p.AddressPool
		if fupCrossed(p, in.Usage) {
			d.RateLimit = throttleRate(p, in.IsolirRateLimit)
		}
		if total := quotaTotal(p, in.Usage); total > 0 && in.Usage.UsedBytes < uint64(total) {
			d.TotalLimit = uint64(total) - in.Usage.UsedBytes
			d.HasTotalLimit = true
		}
	}
	if to := sessionTimeout(acct.ExpiresAt, in.Package, in.Usage, in.Now); to > 0 {
		d.SessionTimeout = to
		d.HasSessionTimeout = true
	}
	return d
}

func isolatedAccept(in AuthInput) AuthDecision {
	d := AuthDecision{
		Accept:          true,
		Isolated:        true,
		RateLimit:       in.IsolirRateLimit,
		AddressList:     in.IsolirAddressList,
		InterimInterval: in.InterimInterval,
	}
	if _, remaining := expiry(in.Account.ExpiresAt, in.Now); remaining > 0 {
		d.SessionTimeout = remaining
		d.HasSessionTimeout = true
	}
	return d
}

// exhaustedDecision applies the package on_exhausted action to an expired
// or depleted account. Unknown or empty actions fail closed to reject.
func exhaustedDecision(in AuthInput, reason string) AuthDecision {
	action := OnExhaustedReject
	if in.Package != nil && in.Package.OnExhausted != "" {
		action = in.Package.OnExhausted
	}
	switch action {
	case OnExhaustedIsolate:
		return isolatedAccept(in)
	case OnExhaustedThrottle:
		return throttledAccept(in)
	default:
		return reject(reason)
	}
}

// throttledAccept answers an exhausted account whose package says
// throttle: accept at the degraded rate WITHOUT Session-Timeout or
// Total-Limit (either cap would defeat the throttle by ending the
// degraded session immediately). No address-list is attached, so unlike
// isolate there is no walled garden.
func throttledAccept(in AuthInput) AuthDecision {
	d := AuthDecision{
		Accept:          true,
		Throttled:       true,
		RateLimit:       throttleRate(in.Package, in.IsolirRateLimit),
		FramedIP:        in.Account.StaticIP,
		InterimInterval: in.InterimInterval,
	}
	if in.Package != nil {
		d.FramedPool = in.Package.AddressPool
	}
	return d
}

// throttleRate is the degraded session rate: the package fup_rate_limit,
// or the isolir rate when the package sets none (documented fallback:
// throttle must still signal a degraded state).
func throttleRate(p *Package, isolir string) string {
	if p != nil && p.FUPRateLimit != "" {
		return p.FUPRateLimit
	}
	return isolir
}

// quotaTotal is the effective quota: quota_bytes + running-period top-ups,
// floored at 0. Zero means no quota is configured (no HasQuota, or a
// non-positive total).
func quotaTotal(p *Package, u UsageSnapshot) int64 {
	if p == nil || !p.HasQuota {
		return 0
	}
	if total := p.QuotaBytes + u.TopupBytes; total > 0 {
		return total
	}
	return 0
}

// exhaustedReason reports whether the quota or the online time is spent.
// Pure: no I/O.
func exhaustedReason(p *Package, u UsageSnapshot) (string, bool) {
	if p == nil {
		return "", false
	}
	if total := quotaTotal(p, u); total > 0 && u.UsedBytes >= uint64(total) {
		return "Quota exhausted", true
	}
	if p.HasOnlineLimit && p.OnlineSecondsLimit > 0 &&
		u.UsedSeconds >= uint64(p.OnlineSecondsLimit) {
		return "Online time exhausted", true
	}
	return "", false
}

// fupCrossed reports whether usage reached the FUP threshold (the session
// should run at the degraded rate while quota remains).
func fupCrossed(p *Package, u UsageSnapshot) bool {
	return p.HasFUPThreshold && p.FUPThresholdBytes > 0 &&
		u.UsedBytes >= uint64(p.FUPThresholdBytes)
}

// sessionTimeout is the minimum of the remaining expires_at and online
// time (0 = omit). Pure: no I/O.
func sessionTimeout(expiresAt string, p *Package, u UsageSnapshot, now time.Time) uint32 {
	var best uint32
	have := false
	if _, rem := expiry(expiresAt, now); rem > 0 {
		best, have = rem, true
	}
	if p != nil && p.HasOnlineLimit && p.OnlineSecondsLimit > 0 &&
		u.UsedSeconds < uint64(p.OnlineSecondsLimit) {
		rem := uint64(p.OnlineSecondsLimit) - u.UsedSeconds
		if rem > 4294967295 {
			rem = 4294967295
		}
		if !have || uint32(rem) < best {
			best, have = uint32(rem), true
		}
	}
	if !have {
		return 0
	}
	return best
}

// expiry parses an RFC3339 expires_at value. It reports expired=true when
// the timestamp is set and now is at or past it; remaining is the whole
// seconds until expiry (0 when unset, unparseable, or already expired).
// An unparseable non-empty value is treated as expired (fail closed).
func expiry(expiresAt string, now time.Time) (expired bool, remaining uint32) {
	if expiresAt == "" {
		return false, 0
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true, 0
	}
	if !now.Before(t) {
		return true, 0
	}
	secs := t.Sub(now).Truncate(time.Second).Seconds()
	if secs < 1 {
		return true, 0
	}
	if secs > 4294967295 {
		secs = 4294967295
	}
	return false, uint32(secs)
}

// splitLimit splits a byte limit into the 32-bit Mikrotik-Total-Limit value
// and its Gigawords companion (0 when the limit fits in 32 bits).
func splitLimit(limit uint64) (low, gigawords uint32) {
	return uint32(limit & 0xffffffff), uint32(limit >> 32)
}
