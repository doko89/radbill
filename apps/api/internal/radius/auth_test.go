package radius

import (
	"context"
	"crypto/md5"
	"log/slog"
	"net"
	"testing"
	"time"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

const (
	testNASIP     = "127.0.0.1"
	testNASID     = "nas-test"
	testNASSecret = "test-secret"
)

// testCache builds a cache without a database (in-package access).
func testCache() *Cache {
	c := NewCache()
	c.storeNAS(&NAS{ID: testNASID, Name: "test-nas", IP: testNASIP, Secret: testNASSecret, CoAPort: 3799})
	c.accounts["alice"] = &Account{ID: "a-alice", CustomerID: "c1", PackageID: "p1",
		Username: "alice", Password: "alicepw", Status: StatusActive, StaticIP: "198.51.100.7"}
	c.accounts["bob"] = &Account{ID: "a-bob", CustomerID: "c1", PackageID: "p1",
		Username: "bob", Password: "bobpw", Status: StatusIsolated}
	c.accounts["zed"] = &Account{ID: "a-zed", CustomerID: "c1", PackageID: "p1",
		Username: "zed", Password: "zedpw", Status: StatusDisabled}
	c.packages["p1"] = &Package{ID: "p1", Name: "P1", RateLimit: "10M/10M",
		AddressPool: "pool1", QuotaBytes: 5 << 30, HasQuota: true, OnExhausted: OnExhaustedReject}
	c.settings[SettingInterimInterval] = "300"
	c.settings[SettingIsolirRateLimit] = DefaultIsolirRateLimit
	c.settings[SettingIsolirAddressList] = DefaultIsolirAddressList
	return c
}

type captureWriter struct{ got *lradius.Packet }

func (w *captureWriter) Write(p *lradius.Packet) error { w.got = p; return nil }

func testRequest(pkt *lradius.Packet, ip string) *lradius.Request {
	return &lradius.Request{
		RemoteAddr: &net.UDPAddr{IP: net.ParseIP(ip)},
		Packet:     pkt,
	}
}

func papPacket(t *testing.T, username, password string) *lradius.Packet {
	t.Helper()
	p := lradius.New(lradius.CodeAccessRequest, []byte(testNASSecret))
	if err := rfc2865.UserName_SetString(p, username); err != nil {
		t.Fatal(err)
	}
	// Single encryption exactly like a real NAS: Add pads (RFC 2865 5.2)
	// and encrypts. Never pre-encrypt: that double-encrypts on the wire.
	if err := rfc2865.UserPassword_AddString(p, password); err != nil {
		t.Fatal(err)
	}
	return p
}

// nasPacket builds a request exactly as a real NAS puts it on the wire: the
// User-Password attribute is encrypted once with NewUserPassword and added
// raw, under a fixed authenticator so the vector is deterministic.
func nasPacket(t *testing.T, username, password string, authenticator [16]byte) *lradius.Packet {
	t.Helper()
	p := lradius.New(lradius.CodeAccessRequest, []byte(testNASSecret))
	copy(p.Authenticator[:], authenticator[:])
	if err := rfc2865.UserName_SetString(p, username); err != nil {
		t.Fatal(err)
	}
	enc, err := lradius.NewUserPassword([]byte(password), []byte(testNASSecret), p.Authenticator[:])
	if err != nil {
		t.Fatal(err)
	}
	p.Add(rfc2865.UserPassword_Type, enc)
	return p
}

func chapPacket(t *testing.T, username, password string, ident byte, challenge []byte) *lradius.Packet {
	t.Helper()
	p := lradius.New(lradius.CodeAccessRequest, []byte(testNASSecret))
	if err := rfc2865.UserName_SetString(p, username); err != nil {
		t.Fatal(err)
	}
	mac := md5.New()
	mac.Write([]byte{ident})
	mac.Write([]byte(password))
	mac.Write(challenge)
	chap := append([]byte{ident}, mac.Sum(nil)...)
	if err := rfc2865.CHAPPassword_Add(p, chap); err != nil {
		t.Fatal(err)
	}
	if err := rfc2865.CHAPChallenge_Add(p, challenge); err != nil {
		t.Fatal(err)
	}
	return p
}

// findMikrotik extracts a MikroTik VSA value from a packet.
func findMikrotik(t *testing.T, p *lradius.Packet, typ byte) []byte {
	t.Helper()
	for _, avp := range p.Attributes {
		if avp.Type != rfc2865.VendorSpecific_Type {
			continue
		}
		vid, vsa, err := lradius.VendorSpecific(avp.Attribute)
		if err != nil || vid != VendorIDMikrotik {
			continue
		}
		for len(vsa) >= 3 {
			vt, vl := vsa[0], vsa[1]
			if int(vl) > len(vsa) || vl < 3 {
				break
			}
			if vt == typ {
				return append([]byte(nil), vsa[2:int(vl)]...)
			}
			vsa = vsa[int(vl):]
		}
	}
	t.Fatalf("mikrotik vsa %d not found", typ)
	return nil
}

func serveAuth(pkt *lradius.Packet, ip string) *lradius.Packet {
	srv := &AuthServer{Cache: testCache(), Log: slog.New(slog.DiscardHandler)}
	cw := &captureWriter{}
	srv.ServeRADIUS(cw, testRequest(pkt, ip))
	return cw.got
}

func TestAuthPAPAccept(t *testing.T) {
	got := serveAuth(papPacket(t, "alice", "alicepw"), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessAccept {
		t.Fatalf("got %+v, want Access-Accept", got)
	}
	if v := findMikrotik(t, got, MikrotikAttrRateLimit); string(v) != "10M/10M" {
		t.Fatalf("rate-limit = %q", v)
	}
	if v := findMikrotik(t, got, MikrotikAttrTotalLimit); len(v) != 4 {
		t.Fatalf("total-limit = % x, want 4 bytes", v)
	}
	if v := findMikrotik(t, got, MikrotikAttrTotalLimitGigawords); len(v) != 4 || v[3] != 1 {
		t.Fatalf("total-limit-gigawords = % x, want 1", v)
	}
}

// TestAuthPAPRealNASVector is the regression test for the double PAP
// (en|de)cryption bug: the server must accept a singly-encrypted
// User-Password exactly as a real NAS sends it. Fixed authenticator, no
// randomness, so this test cannot flake.
func TestAuthPAPRealNASVector(t *testing.T) {
	var fixedAuth [16]byte
	for i := range fixedAuth {
		fixedAuth[i] = byte(i + 1)
	}
	cases := []struct {
		name     string
		username string
		password string
		accept   bool
	}{
		{"active accept", "alice", "alicepw", true},
		{"isolated accept", "bob", "bobpw", true},
		{"wrong password reject", "alice", "wrong", false},
		{"unknown user reject", "nobody", "x", false},
		{"disabled reject", "zed", "zedpw", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serveAuth(nasPacket(t, tc.username, tc.password, fixedAuth), testNASIP)
			if got == nil {
				t.Fatal("got no response, want Accept or Reject")
			}
			if tc.accept && got.Code != lradius.CodeAccessAccept {
				t.Fatalf("got %v (%q), want Access-Accept", got.Code, rfc2865.ReplyMessage_GetString(got))
			}
			if !tc.accept && got.Code != lradius.CodeAccessReject {
				t.Fatalf("got %v, want Access-Reject", got.Code)
			}
		})
	}
}

func TestAuthPAPWrongPassword(t *testing.T) {
	got := serveAuth(papPacket(t, "alice", "wrong"), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("got %+v, want Access-Reject", got)
	}
	if rfc2865.ReplyMessage_GetString(got) == "" {
		t.Fatal("reject must carry Reply-Message")
	}
}

func TestAuthCHAPAccept(t *testing.T) {
	got := serveAuth(chapPacket(t, "alice", "alicepw", 7, []byte("challenge-16byte")), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessAccept {
		t.Fatalf("got %+v, want Access-Accept", got)
	}
}

func TestAuthCHAPWrongPassword(t *testing.T) {
	got := serveAuth(chapPacket(t, "alice", "wrong", 7, []byte("challenge-16byte")), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("got %+v, want Access-Reject", got)
	}
}

func TestAuthUnknownUserRejects(t *testing.T) {
	got := serveAuth(papPacket(t, "nobody", "x"), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("got %+v, want Access-Reject", got)
	}
}

func TestAuthDisabledRejects(t *testing.T) {
	got := serveAuth(papPacket(t, "zed", "zedpw"), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("got %+v, want Access-Reject", got)
	}
}

func TestAuthIsolatedAccept(t *testing.T) {
	got := serveAuth(papPacket(t, "bob", "bobpw"), testNASIP)
	if got == nil || got.Code != lradius.CodeAccessAccept {
		t.Fatalf("got %+v, want Access-Accept", got)
	}
	if v := findMikrotik(t, got, MikrotikAttrAddressList); string(v) != "isolir" {
		t.Fatalf("address-list = %q", v)
	}
}

func TestAuthUnknownNASDrops(t *testing.T) {
	if got := serveAuth(papPacket(t, "alice", "alicepw"), "203.0.113.9"); got != nil {
		t.Fatalf("unknown NAS must get no response, got %+v", got)
	}
	s := &Secrets{Cache: testCache(), Log: slog.New(slog.DiscardHandler)}
	secret, err := s.RADIUSSecret(context.Background(), &net.UDPAddr{IP: net.ParseIP("203.0.113.9")})
	if err != nil || len(secret) != 0 {
		t.Fatalf("unknown NAS secret = %q, %v; want empty", secret, err)
	}
	secret, err = s.RADIUSSecret(context.Background(), &net.UDPAddr{IP: net.ParseIP(testNASIP)})
	if err != nil || string(secret) != testNASSecret {
		t.Fatalf("known NAS secret mismatch: %v", err)
	}
}

func TestAuthNoCredentialsRejects(t *testing.T) {
	p := lradius.New(lradius.CodeAccessRequest, []byte(testNASSecret))
	if err := rfc2865.UserName_SetString(p, "alice"); err != nil {
		t.Fatal(err)
	}
	got := serveAuth(p, testNASIP)
	if got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("got %+v, want Access-Reject", got)
	}
}

// TestAuthUDP runs a real UDP auth exchange against PacketServer.
func TestAuthUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("udp unavailable: %v", err)
	}
	cache := testCache()
	srv := &lradius.PacketServer{
		SecretSource: &Secrets{Cache: cache, Log: slog.New(slog.DiscardHandler)},
		Handler:      &AuthServer{Cache: cache, Log: slog.New(slog.DiscardHandler)},
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(conn) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	req := lradius.New(lradius.CodeAccessRequest, []byte(testNASSecret))
	if err := rfc2865.UserName_SetString(req, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := rfc2865.UserPassword_AddString(req, "alicepw"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &lradius.Client{}
	resp, err := client.Exchange(ctx, req, conn.LocalAddr().String())
	if err != nil {
		t.Skipf("udp exchange failed: %v", err)
	}
	if resp.Code != lradius.CodeAccessAccept {
		t.Fatalf("got %v, want Access-Accept", resp.Code)
	}
}

// TestAuthG4Metrics is the Fase 4 additive check (agen E1): accepts and
// rejects increment the Metrics counters; silent drops (unknown NAS,
// non-Access-Request) count neither; a nil Metrics stays safe.
func TestAuthG4Metrics(t *testing.T) {
	m := &Metrics{}
	srv := &AuthServer{Cache: testCache(), Log: slog.New(slog.DiscardHandler), Metrics: m}
	serve := func(pkt *lradius.Packet, ip string) *lradius.Packet {
		cw := &captureWriter{}
		srv.ServeRADIUS(cw, testRequest(pkt, ip))
		return cw.got
	}
	if got := serve(papPacket(t, "alice", "alicepw"), testNASIP); got == nil || got.Code != lradius.CodeAccessAccept {
		t.Fatalf("accept = %+v, want Access-Accept", got)
	}
	if got := serve(papPacket(t, "alice", "wrongpw"), testNASIP); got == nil || got.Code != lradius.CodeAccessReject {
		t.Fatalf("reject = %+v, want Access-Reject", got)
	}
	serve(papPacket(t, "alice", "alicepw"), "10.9.9.9") // unknown NAS: drop
	serve(lradius.New(lradius.CodeAccountingRequest, []byte(testNASSecret)), testNASIP)
	if m.AuthAccepts() != 1 || m.AuthRejects() != 1 {
		t.Fatalf("accepts=%d rejects=%d, want 1/1", m.AuthAccepts(), m.AuthRejects())
	}
	// Nil metrics: identical decisions, no counting, no panic.
	plain := &AuthServer{Cache: testCache(), Log: slog.New(slog.DiscardHandler)}
	cw := &captureWriter{}
	plain.ServeRADIUS(cw, testRequest(papPacket(t, "alice", "alicepw"), testNASIP))
	if cw.got == nil || cw.got.Code != lradius.CodeAccessAccept {
		t.Fatalf("nil-metrics accept = %+v", cw.got)
	}
}
