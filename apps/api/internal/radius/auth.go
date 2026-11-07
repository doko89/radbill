package radius

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"log/slog"
	"net"
	"time"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2869"
)

// Secrets resolves the shared secret for a packet source address. Unknown
// senders yield an empty secret, which makes the server drop the packet
// without a response (never log secrets).
type Secrets struct {
	Cache *Cache
	Log   *slog.Logger
}

// RADIUSSecret implements lradius.SecretSource.
func (s *Secrets) RADIUSSecret(_ context.Context, remoteAddr net.Addr) ([]byte, error) {
	ip := sourceIP(remoteAddr)
	nas, ok := s.Cache.NASByIP(ip)
	if !ok {
		return nil, nil // drop packets from unknown NAS
	}
	return []byte(nas.Secret), nil
}

func sourceIP(addr net.Addr) string {
	switch a := addr.(type) {
	case *net.UDPAddr:
		return a.IP.String()
	case *net.TCPAddr:
		return a.IP.String()
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return addr.String()
		}
		return host
	}
}

// AuthServer answers Access-Request packets from the cache only: the auth
// path performs no database queries.
type AuthServer struct {
	Cache *Cache
	Log   *slog.Logger
	Usage *UsageMirror     // nil = treat every account as fresh (full quota/time)
	Now   func() time.Time // overridable clock (tests); nil = time.Now
	// Metrics counts accept/reject decisions (G4, ADITIF); nil = no counting.
	Metrics *Metrics
}

func (s *AuthServer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ServeRADIUS implements lradius.Handler.
func (s *AuthServer) ServeRADIUS(w lradius.ResponseWriter, r *lradius.Request) {
	if r.Code != lradius.CodeAccessRequest {
		return
	}
	nas, ok := s.Cache.NASByIP(sourceIP(r.RemoteAddr))
	if !ok {
		return // unknown NAS: drop without response
	}
	secret := []byte(nas.Secret)
	username := rfc2865.UserName_GetString(r.Packet)
	if username == "" {
		s.writeReject(w, r, nas, "", "missing username")
		return
	}

	var (
		okCreds bool
		method  string
	)
	if chapAttr, err := rfc2865.CHAPPassword_Lookup(r.Packet); err == nil {
		method = "chap"
		acct, found := s.Cache.Account(username)
		okCreds = found && verifyCHAP(chapAttr, chapChallenge(r.Packet, r.Authenticator[:]), acct.Password)
	} else if papAttr, ok := r.Packet.Lookup(rfc2865.UserPassword_Type); ok {
		// Raw wire bytes: rfc2865.UserPassword_Lookup already decrypts, and
		// decrypting its output again in verifyPAP would break PAP against
		// real NAS hardware (and flake in tests).
		method = "pap"
		acct, found := s.Cache.Account(username)
		okCreds = found && verifyPAP(papAttr, secret, r.Authenticator[:], acct.Password)
	} else {
		s.writeReject(w, r, nas, username, "no PAP or CHAP credentials")
		return
	}
	if !okCreds {
		s.writeReject(w, r, nas, username, method+" failed")
		return
	}

	acct, _ := s.Cache.Account(username)
	pkg, _ := s.Cache.Package(acct.PackageID)
	var snap UsageSnapshot
	if pkg != nil && s.Usage != nil {
		snap = s.Usage.Snapshot(acct.ID, pkg.QuotaReset, acct.CreatedAt, s.now()).Snapshot()
	}
	decision := decideAuth(AuthInput{
		Account:           acct,
		Package:           pkg,
		NASID:             nas.ID,
		Usage:             snap,
		InterimInterval:   s.Cache.InterimInterval(),
		IsolirRateLimit:   s.Cache.Setting(SettingIsolirRateLimit, DefaultIsolirRateLimit),
		IsolirAddressList: s.Cache.Setting(SettingIsolirAddressList, DefaultIsolirAddressList),
		Now:               s.now(),
	})
	if !decision.Accept {
		s.writeReject(w, r, nas, username, decision.ReplyMessage)
		return
	}
	resp := r.Response(lradius.CodeAccessAccept)
	if err := applyDecision(resp, &decision); err != nil {
		s.Log.Warn("auth accept: attribute error", "user", username, "nas", nas.Name, "err", err)
		s.writeReject(w, r, nas, username, "server error")
		return
	}
	if err := w.Write(resp); err != nil {
		s.Log.Warn("auth accept: write failed", "user", username, "nas", nas.Name, "err", err)
		return
	}
	s.Metrics.AddAuthAccept() // G4: nil-safe, servers without metrics skip
	s.Log.Debug("auth accept", "user", username, "nas", nas.Name, "method", method,
		"isolated", decision.Isolated, "throttled", decision.Throttled)
}

func (s *AuthServer) writeReject(w lradius.ResponseWriter, r *lradius.Request, nas *NAS, username, reason string) {
	s.Metrics.AddAuthReject() // G4: count the decision, like coa_sent/disconnects
	resp := r.Response(lradius.CodeAccessReject)
	_ = rfc2865.ReplyMessage_AddString(resp, reason)
	if err := w.Write(resp); err != nil {
		s.Log.Warn("auth reject: write failed", "nas", nas.Name, "err", err)
		return
	}
	s.Log.Debug("auth reject", "user", username, "nas", nas.Name, "reason", reason)
}

// applyDecision renders an accept decision into reply attributes.
func applyDecision(resp *lradius.Packet, d *AuthDecision) error {
	if d.RateLimit != "" {
		if err := AddMikrotikString(resp, MikrotikAttrRateLimit, d.RateLimit); err != nil {
			return err
		}
	}
	if d.AddressList != "" {
		if err := AddMikrotikString(resp, MikrotikAttrAddressList, d.AddressList); err != nil {
			return err
		}
	}
	if d.FramedPool != "" {
		if err := rfc2869.FramedPool_AddString(resp, d.FramedPool); err != nil {
			return err
		}
	}
	if d.FramedIP != "" {
		ip := net.ParseIP(d.FramedIP).To4()
		if ip == nil {
			return &net.ParseError{Type: "IPv4", Text: d.FramedIP}
		}
		if err := rfc2865.FramedIPAddress_Add(resp, ip); err != nil {
			return err
		}
	}
	if d.HasSessionTimeout {
		if err := rfc2865.SessionTimeout_Add(resp, rfc2865.SessionTimeout(d.SessionTimeout)); err != nil {
			return err
		}
	}
	if d.InterimInterval > 0 {
		if err := rfc2869.AcctInterimInterval_Add(resp, rfc2869.AcctInterimInterval(d.InterimInterval)); err != nil {
			return err
		}
	}
	if d.HasTotalLimit {
		low, gw := splitLimit(d.TotalLimit)
		if err := AddMikrotikUint32(resp, MikrotikAttrTotalLimit, low); err != nil {
			return err
		}
		if gw > 0 {
			if err := AddMikrotikUint32(resp, MikrotikAttrTotalLimitGigawords, gw); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyPAP decrypts a raw wire User-Password attribute (exactly as sent by
// the NAS: singly encrypted) and compares it with the cached plaintext
// password in constant time.
func verifyPAP(attr lradius.Attribute, secret, requestAuth []byte, password string) bool {
	plain, err := lradius.UserPassword(attr, secret, requestAuth)
	if err != nil {
		return false
	}
	if len(plain) != len(password) {
		return false
	}
	return subtle.ConstantTimeCompare(plain, []byte(password)) == 1
}

// chapChallenge returns the CHAP challenge: the CHAP-Challenge attribute
// when present, otherwise the request authenticator (RFC 2865 4.2).
func chapChallenge(p *lradius.Packet, requestAuth []byte) []byte {
	if ch, err := rfc2865.CHAPChallenge_Lookup(p); err == nil {
		return []byte(ch)
	}
	return requestAuth
}

// verifyCHAP checks a CHAP-Password attribute (ident byte + 16-byte MD5
// response) against the cached plaintext password.
func verifyCHAP(chapAttr, challenge []byte, password string) bool {
	if len(chapAttr) != 17 || len(challenge) == 0 {
		return false
	}
	mac := md5.New()
	mac.Write(chapAttr[:1])
	mac.Write([]byte(password))
	mac.Write(challenge)
	return subtle.ConstantTimeCompare(mac.Sum(nil), chapAttr[1:]) == 1
}
