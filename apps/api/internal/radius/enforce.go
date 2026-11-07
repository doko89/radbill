// Interim enforcement: quota and FUP actions while a session runs (PRD 7.3).
//
// Every non-duplicate Interim-Update passes through Enforcer.CheckInterim,
// which compares the account mirror (post-delta) and the session totals
// against the package quota and FUP threshold:
//
//   - quota crossed -> Disconnect-Request for on_exhausted reject/isolate,
//     one CoA to the FUP rate for throttle;
//   - FUP threshold crossed (quota still left) -> one CoA with the new
//     Mikrotik-Rate-Limit, session stays up.
//
// Each action fires at most once per session (tracked per SessionKey); the
// flags latch until the session ends (Stop), restarts (Start) or the NAS
// reboots (Accounting-On/Off). There is deliberately no revert CoA: usage
// is monotonic inside a bucket, so a throttled session keeps its rate
// until it re-authenticates. All sends are best-effort: failures are
// logged, the interim is still ACKed, and the attempt counters below keep
// counting attempts, not NAS ACKs.
package radius

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	lradius "layeh.com/radius"

	"boilerplate/api/internal/network/coa"
)

// Metrics counts enforcement attempts for /healthz. Zero value is usable;
// methods are nil-receiver safe so servers without enforcement report 0.
//
// G4 (agen E1, ADITIF) adds auth accept/reject and accounting-received
// counters, surfaced on /healthz as auth_accept, auth_reject and
// acct_received. Like the enforcement counters they count decisions,
// not NAS ACKs.
type Metrics struct {
	coaSent     atomic.Int64
	disconnects atomic.Int64
	// G4 additive counters.
	authAccept   atomic.Int64
	authReject   atomic.Int64
	acctReceived atomic.Int64
}

// AddCoA records one CoA-Request attempt.
func (m *Metrics) AddCoA() {
	if m == nil {
		return
	}
	m.coaSent.Add(1)
}

// AddDisconnect records one Disconnect-Request attempt.
func (m *Metrics) AddDisconnect() {
	if m == nil {
		return
	}
	m.disconnects.Add(1)
}

// CoASent returns the CoA-Request attempt count.
func (m *Metrics) CoASent() int64 {
	if m == nil {
		return 0
	}
	return m.coaSent.Load()
}

// Disconnects returns the Disconnect-Request attempt count.
func (m *Metrics) Disconnects() int64 {
	if m == nil {
		return 0
	}
	return m.disconnects.Load()
}

// AddAuthAccept records one Access-Accept decision (G4, ADITIF).
func (m *Metrics) AddAuthAccept() {
	if m == nil {
		return
	}
	m.authAccept.Add(1)
}

// AddAuthReject records one Access-Reject decision (G4, ADITIF).
func (m *Metrics) AddAuthReject() {
	if m == nil {
		return
	}
	m.authReject.Add(1)
}

// AddAcctReceived records one received Accounting-Request (G4, ADITIF).
func (m *Metrics) AddAcctReceived() {
	if m == nil {
		return
	}
	m.acctReceived.Add(1)
}

// AuthAccepts returns the Access-Accept decision count (G4, ADITIF).
func (m *Metrics) AuthAccepts() int64 {
	if m == nil {
		return 0
	}
	return m.authAccept.Load()
}

// AuthRejects returns the Access-Reject decision count (G4, ADITIF).
func (m *Metrics) AuthRejects() int64 {
	if m == nil {
		return 0
	}
	return m.authReject.Load()
}

// AcctReceived returns the received Accounting-Request count (G4, ADITIF).
func (m *Metrics) AcctReceived() int64 {
	if m == nil {
		return 0
	}
	return m.acctReceived.Load()
}

// CoASender exchanges CoA/Disconnect packets with a NAS. *coa.Sender
// implements it; tests substitute a fake.
type CoASender interface {
	Disconnect(ctx context.Context, nas coa.NAS, username, sessionID string) (lradius.Code, error)
	ChangeOfAuth(ctx context.Context, nas coa.NAS, username, sessionID string, mutate func(*lradius.Packet) error) (lradius.Code, error)
}

// Enforcer carries per-session enforcement flags plus the CoA sender.
// The zero value is not usable; build with NewEnforcer. Methods are
// nil-receiver safe (a nil *Enforcer enforces nothing) so the accounting
// path needs no guards.
type Enforcer struct {
	sender  CoASender
	log     *slog.Logger
	metrics *Metrics
	timeout time.Duration

	mu    sync.Mutex
	state map[SessionKey]*enforceState
}

type enforceState struct {
	capped bool // quota action already fired (disconnect or throttle CoA)
	fup    bool // FUP-rate CoA already fired
}

// NewEnforcer builds an Enforcer around the default CoA sender. log and
// metrics may be nil (discard / count nowhere).
func NewEnforcer(log *slog.Logger, metrics *Metrics) *Enforcer {
	return NewEnforcerWithSender(coa.NewSender(coa.DefaultTimeout), log, metrics)
}

// NewEnforcerWithSender builds an Enforcer around a custom sender (tests).
func NewEnforcerWithSender(sender CoASender, log *slog.Logger, metrics *Metrics) *Enforcer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Enforcer{
		sender:  sender,
		log:     log,
		metrics: metrics,
		timeout: coa.DefaultTimeout,
		state:   make(map[SessionKey]*enforceState),
	}
}

// InterimInput is one enforcement check: the interim's session totals plus
// the account mirror reading after applying its delta.
type InterimInput struct {
	NAS         *NAS
	Account     *Account
	Package     *Package
	SessionID   string
	SessionUsed uint64 // session in+out totals (catches runaway sessions past a rollover)
	UsedBytes   uint64 // account mirror total in the running bucket
	TopupBytes  int64  // account top-ups in the running bucket
	IsolirRate  string // fallback throttle rate when the package sets no fup_rate_limit
}

// CheckInterim enforces quota and FUP for one Interim-Update. It blocks
// for at most one CoA/Disconnect exchange (5s timeout, normally
// milliseconds); the UDP handler serves packets concurrently so other
// sessions are unaffected. Nil packages, accounts or NAS skip silently.
func (e *Enforcer) CheckInterim(in InterimInput) {
	if e == nil || in.Package == nil || in.Account == nil || in.NAS == nil || in.SessionID == "" {
		return
	}
	p := in.Package
	total := quotaTotal(p, UsageSnapshot{UsedBytes: in.UsedBytes, TopupBytes: in.TopupBytes})
	exhausted := total > 0 && (in.UsedBytes >= uint64(total) || in.SessionUsed >= uint64(total))

	const (
		fireNone = iota
		fireDisconnect
		fireCoA
	)
	fire := fireNone
	rate := ""
	key := SessionKey{NASID: in.NAS.ID, SessionID: in.SessionID}

	e.mu.Lock()
	st, ok := e.state[key]
	var capped, fup bool
	if ok {
		capped, fup = st.capped, st.fup
	}
	switch {
	case exhausted && !capped:
		capped, fup = true, true // a throttle CoA already puts the session at FUP rate
		if p.OnExhausted == OnExhaustedThrottle {
			fire, rate = fireCoA, throttleRate(p, in.IsolirRate)
		} else {
			// reject, isolate, empty or unknown: fail closed by disconnecting.
			fire = fireDisconnect
		}
	case !exhausted && !fup && fupCrossed(p, UsageSnapshot{UsedBytes: in.UsedBytes}):
		fup = true
		fire, rate = fireCoA, throttleRate(p, in.IsolirRate)
	}
	if fire != fireNone {
		if !ok {
			st = &enforceState{}
			e.state[key] = st
		}
		st.capped, st.fup = capped, fup
	}
	e.mu.Unlock()

	target := coa.NAS{Address: in.NAS.IP, Port: in.NAS.CoAPort, Secret: in.NAS.Secret}
	switch fire {
	case fireDisconnect:
		e.metrics.AddDisconnect()
		ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
		defer cancel()
		code, err := e.sender.Disconnect(ctx, target, in.Account.Username, in.SessionID)
		if err != nil {
			e.log.Warn("enforce: disconnect failed", "user", in.Account.Username,
				"session", in.SessionID, "nas", in.NAS.Name, "err", err)
			return
		}
		e.log.Info("enforce: quota exhausted, disconnect sent", "user", in.Account.Username,
			"session", in.SessionID, "nas", in.NAS.Name, "code", code.String())
	case fireCoA:
		e.metrics.AddCoA()
		ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
		defer cancel()
		code, err := e.sender.ChangeOfAuth(ctx, target, in.Account.Username, in.SessionID,
			func(pkt *lradius.Packet) error {
				return AddMikrotikString(pkt, MikrotikAttrRateLimit, rate)
			})
		if err != nil {
			e.log.Warn("enforce: CoA failed", "user", in.Account.Username,
				"session", in.SessionID, "nas", in.NAS.Name, "err", err)
			return
		}
		e.log.Info("enforce: rate CoA sent", "user", in.Account.Username,
			"session", in.SessionID, "nas", in.NAS.Name, "code", code.String())
	}
}

// Remove forgets one session's enforcement flags (on Stop and on Start,
// so a reused session id starts clean). Nil-receiver safe.
func (e *Enforcer) Remove(key SessionKey) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.state, key)
}

// RemoveNAS forgets every session flag of one NAS (after Accounting-On/
// Off). Nil-receiver safe.
func (e *Enforcer) RemoveNAS(nasID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for k := range e.state {
		if k.NASID == nasID {
			delete(e.state, k)
		}
	}
}
