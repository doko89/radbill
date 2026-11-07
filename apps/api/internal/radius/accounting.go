package radius

import (
	"log/slog"
	"time"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
)

// AcctServer answers Accounting-Request packets. It tracks per-session
// counters in memory and enqueues batched DB writes; it never blocks the
// UDP path on the database.
type AcctServer struct {
	Cache    *Cache
	Log      *slog.Logger
	Sessions *SessionTracker
	Writer   *Writer
	Usage    *UsageMirror     // nil = no usage mirror (usage still persists via Writer)
	Enforce  *Enforcer        // nil = no interim enforcement
	Now      func() time.Time // overridable clock (tests); nil = time.Now
	// Metrics counts received packets (G4, ADITIF); nil = no counting.
	Metrics *Metrics
}

func (s *AcctServer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ServeRADIUS implements lradius.Handler.
func (s *AcctServer) ServeRADIUS(w lradius.ResponseWriter, r *lradius.Request) {
	if r.Code != lradius.CodeAccountingRequest {
		return
	}
	// G4: count every received Accounting-Request (nil-safe), including
	// drops from unknown NAS below — acct_received means received.
	s.Metrics.AddAcctReceived()
	nas, ok := s.Cache.NASByIP(sourceIP(r.RemoteAddr))
	if !ok {
		return // unknown NAS: drop without response
	}
	status, err := rfc2866.AcctStatusType_Lookup(r.Packet)
	if err != nil {
		s.Log.Warn("accounting without status type", "nas", nas.Name)
		s.ack(w, r, nas)
		return
	}
	switch status {
	case rfc2866.AcctStatusType_Value_AccountingOn, rfc2866.AcctStatusType_Value_AccountingOff:
		s.handleOnOff(w, r, nas, status.String())
	case rfc2866.AcctStatusType_Value_Start,
		rfc2866.AcctStatusType_Value_InterimUpdate,
		rfc2866.AcctStatusType_Value_Stop:
		s.handleSession(w, r, nas, status)
	default:
		s.Log.Debug("ignoring accounting status", "nas", nas.Name, "status", status.String())
		s.ack(w, r, nas)
	}
}

func (s *AcctServer) ack(w lradius.ResponseWriter, r *lradius.Request, nas *NAS) {
	if err := w.Write(r.Response(lradius.CodeAccountingResponse)); err != nil {
		s.Log.Warn("accounting ack failed", "nas", nas.Name, "err", err)
	}
}

// handleOnOff closes orphan sessions after a NAS reboot/reload.
func (s *AcctServer) handleOnOff(w lradius.ResponseWriter, r *lradius.Request, nas *NAS, what string) {
	now := s.now().UTC().Format(time.RFC3339)
	dropped := s.Sessions.RemoveNAS(nas.ID)
	s.Enforce.RemoveNAS(nas.ID)
	s.Writer.Enqueue(Op{Kind: OpCloseOrphans, NASID: nas.ID, StoppedAt: now})
	s.ack(w, r, nas)
	s.Log.Info("accounting "+what, "nas", nas.Name, "orphans_closed", dropped)
}

// handleSession processes Start/Interim-Update/Stop for one session.
func (s *AcctServer) handleSession(w lradius.ResponseWriter, r *lradius.Request, nas *NAS, status rfc2866.AcctStatusType) {
	username := rfc2865.UserName_GetString(r.Packet)
	sessionID := rfc2866.AcctSessionID_GetString(r.Packet)
	if username == "" || sessionID == "" {
		s.Log.Warn("accounting without user or session id", "nas", nas.Name, "status", status.String())
		s.ack(w, r, nas)
		return
	}
	acct, ok := s.Cache.Account(username)
	if !ok {
		s.Log.Warn("accounting for unknown user", "nas", nas.Name, "user", username)
		s.ack(w, r, nas)
		return
	}

	now := s.now()
	cur := Totals{
		In:          acctTotals(r.Packet, true),
		Out:         acctTotals(r.Packet, false),
		SessionTime: uint64(rfc2866.AcctSessionTime_Get(r.Packet)),
	}
	key := SessionKey{NASID: nas.ID, SessionID: sessionID}
	isStart := status == rfc2866.AcctStatusType_Value_Start
	if isStart {
		s.Sessions.Reopen(key)
		s.Enforce.Remove(key)
	}
	delta, _ := s.Sessions.Update(key, cur)
	if delta.Duplicate && !isStart {
		// Duplicate Interim or retransmitted Stop: ignore, still ACK.
		s.Log.Debug("duplicate accounting update ignored", "nas", nas.Name, "user", username, "session", sessionID)
		s.ack(w, r, nas)
		return
	}
	// A Start always (re)creates the session row, even with zero counters;
	// the upsert makes retransmits idempotent.

	framedIP := ""
	if ip, err := rfc2865.FramedIPAddress_Lookup(r.Packet); err == nil {
		framedIP = ip.String()
	}
	stamp := now.UTC().Format(time.RFC3339)

	// The usage bucket follows the account package's quota_reset mode;
	// without a mirror or package it stays the G1 monthly bucket.
	pkg, _ := s.Cache.Package(acct.PackageID)
	bucket := PeriodStart(now)
	var mirror UsageEntry
	if s.Usage != nil && pkg != nil {
		var rolled bool
		mirror, rolled = s.Usage.Add(acct.ID, pkg.QuotaReset, acct.CreatedAt, now,
			delta.In, delta.Out, delta.SessionTime)
		bucket = mirror.PeriodStart
		if rolled && delta.In == 0 && delta.Out == 0 && delta.SessionTime == 0 {
			// Materialise the new bucket row even with no usage yet.
			s.Writer.Enqueue(Op{Kind: OpUsage, AccountID: acct.ID, PeriodStart: bucket})
		}
	}

	if delta.In > 0 || delta.Out > 0 || delta.SessionTime > 0 {
		s.Writer.Enqueue(Op{
			Kind: OpUsage, AccountID: acct.ID, PeriodStart: bucket,
			DeltaIn: int64(delta.In), DeltaOut: int64(delta.Out), DeltaSecs: int64(delta.SessionTime),
		})
	}
	switch status {
	case rfc2866.AcctStatusType_Value_Stop:
		cause := ""
		if c, err := rfc2866.AcctTerminateCause_Lookup(r.Packet); err == nil {
			cause = c.String()
		}
		s.Writer.Enqueue(Op{
			Kind: OpSessionStop, SessionID: sessionID, AccountID: acct.ID,
			NASID: nas.ID, FramedIP: framedIP,
			TotalIn: int64(cur.In), TotalOut: int64(cur.Out),
			StartedAt: stamp, StoppedAt: stamp,
			TerminateCause: cause, UpdatedAt: stamp,
		})
		s.Sessions.Stop(key, cur)
		s.Enforce.Remove(key)
	default: // Start, Interim-Update
		s.Writer.Enqueue(Op{
			Kind: OpSessionUpsert, SessionID: sessionID, AccountID: acct.ID,
			NASID: nas.ID, FramedIP: framedIP,
			TotalIn: int64(cur.In), TotalOut: int64(cur.Out),
			StartedAt: stamp, UpdatedAt: stamp,
		})
	}
	if status == rfc2866.AcctStatusType_Value_InterimUpdate && pkg != nil {
		s.Enforce.CheckInterim(InterimInput{
			NAS: nas, Account: acct, Package: pkg,
			SessionID:   sessionID,
			SessionUsed: cur.In + cur.Out,
			UsedBytes:   mirror.UsedBytes(),
			TopupBytes:  mirror.Topups,
			IsolirRate:  s.Cache.Setting(SettingIsolirRateLimit, DefaultIsolirRateLimit),
		})
	}
	s.ack(w, r, nas)
	s.Log.Debug("accounting update", "nas", nas.Name, "status", status.String(),
		"user", username, "session", sessionID,
		"din", delta.In, "dout", delta.Out, "dsecs", delta.SessionTime)
}

// acctTotals combines Acct-{Input,Output}-Octets with the Gigawords
// extensions (RFC 2869) into a 64-bit total. Missing attributes count 0.
func acctTotals(p *lradius.Packet, input bool) uint64 {
	const giga = uint64(1) << 32
	if input {
		octets := uint64(rfc2866.AcctInputOctets_Get(p))
		gw := uint64(rfc2869.AcctInputGigawords_Get(p))
		return gw*giga + octets
	}
	octets := uint64(rfc2866.AcctOutputOctets_Get(p))
	gw := uint64(rfc2869.AcctOutputGigawords_Get(p))
	return gw*giga + octets
}
