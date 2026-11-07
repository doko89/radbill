package radius

import "time"

// AuthInput is the cached data needed for one authentication decision.
// It carries no framework types so the decision stays a pure function.
type AuthInput struct {
	Account *Account // nil = unknown username
	Package *Package // nil when the account's package row is missing

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

// decideAuth maps account + package + settings to accept/reject plus reply
// attributes. Pure: no I/O, no logging, no framework imports.
//
// Rules (PRD 7.1):
//   - unknown username or disabled status: Reject.
//   - expired (expires_at passed): follow the package on_exhausted action.
//     throttle is treated as isolate in Fase 1 (deviation: no separate
//     throttle profile exists yet; Fase 3 enforcement).
//   - isolated: Accept with isolir attributes from settings.
//   - active: Accept with package attributes; Session-Timeout is the
//     remaining time until expires_at when set; Mikrotik-Total-Limit
//     carries the full package quota when set (deviation: remaining quota
//     needs usage lookup, Fase 3).
func decideAuth(in AuthInput) AuthDecision {
	if in.Account == nil {
		return reject("Access denied")
	}
	acct := in.Account
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

	if expired, remaining := expiry(acct.ExpiresAt, in.Now); expired {
		return expiredDecision(in, remaining)
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
		if p.HasQuota && p.QuotaBytes > 0 {
			d.TotalLimit = uint64(p.QuotaBytes)
			d.HasTotalLimit = true
		}
	}
	if _, remaining := expiry(acct.ExpiresAt, in.Now); remaining > 0 {
		d.SessionTimeout = remaining
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

func expiredDecision(in AuthInput, _ uint32) AuthDecision {
	action := OnExhaustedReject
	if in.Package != nil && in.Package.OnExhausted != "" {
		action = in.Package.OnExhausted
	}
	switch action {
	case OnExhaustedIsolate, OnExhaustedThrottle:
		// throttle falls back to isolate in Fase 1 (see decideAuth).
		return isolatedAccept(in)
	default:
		return reject("Account expired")
	}
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
