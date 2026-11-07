package radius

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	lradius "layeh.com/radius"

	"boilerplate/api/internal/network/coa"
)

// fakeSender records CoA/Disconnect attempts without touching the network.
type fakeSender struct {
	mu          sync.Mutex
	disconnects []string // session ids
	coas        []coaCall
	coaErr      error
	discErr     error
}

type coaCall struct {
	sessionID string
	mutate    func(*lradius.Packet) error
}

func (f *fakeSender) Disconnect(_ context.Context, _ coa.NAS, _, sessionID string) (lradius.Code, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects = append(f.disconnects, sessionID)
	return lradius.CodeDisconnectACK, f.discErr
}

func (f *fakeSender) ChangeOfAuth(_ context.Context, _ coa.NAS, _, sessionID string, mutate func(*lradius.Packet) error) (lradius.Code, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.coas = append(f.coas, coaCall{sessionID: sessionID, mutate: mutate})
	return lradius.CodeCoAACK, f.coaErr
}

func (f *fakeSender) counts() (disc, coa int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.disconnects), len(f.coas)
}

// coaRate runs the recorded mutate for the i-th CoA and returns the
// Mikrotik-Rate-Limit it installs.
func (f *fakeSender) coaRate(t *testing.T, i int) string {
	t.Helper()
	f.mu.Lock()
	call := f.coas[i]
	f.mu.Unlock()
	pkt := lradius.New(lradius.CodeCoARequest, []byte("s"))
	if err := call.mutate(pkt); err != nil {
		t.Fatalf("coa mutate: %v", err)
	}
	return string(findMikrotik(t, pkt, MikrotikAttrRateLimit))
}

var enforceNAS = &NAS{ID: "nas-1", Name: "n1", IP: "192.0.2.1", Secret: "s", CoAPort: 3799}
var enforceAcct = &Account{ID: "a1", Username: "u1", Status: StatusActive}

func enforcePkg() *Package {
	return &Package{ID: "p1", RateLimit: "10M/10M",
		QuotaBytes: 10000, HasQuota: true,
		FUPThresholdBytes: 5000, HasFUPThreshold: true, FUPRateLimit: "1M/1M",
		OnExhausted: OnExhaustedReject}
}

func enforceInput(pkg *Package, session string, used uint64) InterimInput {
	return InterimInput{
		NAS: enforceNAS, Account: enforceAcct, Package: pkg,
		SessionID: session, SessionUsed: used, UsedBytes: used,
		IsolirRate: "128k/128k",
	}
}

func TestEnforceQuotaRejectDisconnectsOnce(t *testing.T) {
	fake := &fakeSender{}
	m := &Metrics{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), m)
	in := enforceInput(enforcePkg(), "sess-1", 4000) // below FUP and quota
	e.CheckInterim(in)
	if disc, coas := fake.counts(); disc != 0 || coas != 0 {
		t.Fatalf("below quota: disc=%d coa=%d, want 0,0", disc, coas)
	}
	in.UsedBytes, in.SessionUsed = 10000, 10000
	e.CheckInterim(in)
	e.CheckInterim(in) // repeat interim: still once
	if disc, coas := fake.counts(); disc != 1 || coas != 0 {
		t.Fatalf("exhausted: disc=%d coa=%d, want 1,0", disc, coas)
	}
	if m.Disconnects() != 1 || m.CoASent() != 0 {
		t.Fatalf("metrics = %d,%d, want 1,0", m.Disconnects(), m.CoASent())
	}
}

func TestEnforceQuotaIsolateDisconnects(t *testing.T) {
	fake := &fakeSender{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), nil)
	pkg := enforcePkg()
	pkg.OnExhausted = OnExhaustedIsolate
	e.CheckInterim(enforceInput(pkg, "sess-1", 20000))
	if disc, coas := fake.counts(); disc != 1 || coas != 0 {
		t.Fatalf("disc=%d coa=%d, want 1,0", disc, coas)
	}
}

func TestEnforceQuotaThrottleCoAOnce(t *testing.T) {
	fake := &fakeSender{}
	m := &Metrics{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), m)
	pkg := enforcePkg()
	pkg.OnExhausted = OnExhaustedThrottle
	in := enforceInput(pkg, "sess-1", 20000)
	e.CheckInterim(in)
	e.CheckInterim(in)
	if disc, coas := fake.counts(); disc != 0 || coas != 1 {
		t.Fatalf("disc=%d coa=%d, want 0,1", disc, coas)
	}
	if rate := fake.coaRate(t, 0); rate != "1M/1M" {
		t.Fatalf("coa rate = %q, want FUP rate", rate)
	}
	if m.CoASent() != 1 || m.Disconnects() != 0 {
		t.Fatalf("metrics = %d,%d, want 1,0", m.CoASent(), m.Disconnects())
	}
}

func TestEnforceThrottleFallsBackToIsolirRate(t *testing.T) {
	fake := &fakeSender{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), nil)
	pkg := enforcePkg()
	pkg.OnExhausted = OnExhaustedThrottle
	pkg.FUPRateLimit = ""
	e.CheckInterim(enforceInput(pkg, "sess-1", 20000))
	if disc, coas := fake.counts(); disc != 0 || coas != 1 {
		t.Fatalf("disc=%d coa=%d, want 0,1", disc, coas)
	}
	if rate := fake.coaRate(t, 0); rate != "128k/128k" {
		t.Fatalf("coa rate = %q, want isolir fallback", rate)
	}
}

func TestEnforceFUPCoAOncePerSession(t *testing.T) {
	fake := &fakeSender{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), nil)
	pkg := enforcePkg()
	in := enforceInput(pkg, "sess-fup", 4999)
	e.CheckInterim(in) // below threshold: normal, no action
	if disc, coas := fake.counts(); disc != 0 || coas != 0 {
		t.Fatalf("below FUP: disc=%d coa=%d, want 0,0", disc, coas)
	}
	in.UsedBytes, in.SessionUsed = 5000, 5000
	e.CheckInterim(in)
	in.UsedBytes, in.SessionUsed = 7000, 7000
	e.CheckInterim(in) // still over: no second CoA
	if disc, coas := fake.counts(); disc != 0 || coas != 1 {
		t.Fatalf("over FUP: disc=%d coa=%d, want 0,1", disc, coas)
	}
	if rate := fake.coaRate(t, 0); rate != "1M/1M" {
		t.Fatalf("coa rate = %q, want FUP rate", rate)
	}
	// A different session of the same account fires its own CoA.
	e.CheckInterim(enforceInput(pkg, "sess-other", 6000))
	if _, coas := fake.counts(); coas != 2 {
		t.Fatalf("second session: coa=%d, want 2", coas)
	}
}

func TestEnforceSessionBytesTriggerPastRollover(t *testing.T) {
	fake := &fakeSender{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), nil)
	// Account mirror just rolled (low), but the continuing session alone
	// crossed the quota: still enforced.
	in := enforceInput(enforcePkg(), "sess-1", 0)
	in.UsedBytes, in.SessionUsed = 100, 10000
	e.CheckInterim(in)
	if disc, _ := fake.counts(); disc != 1 {
		t.Fatalf("disc=%d, want 1", disc)
	}
}

func TestEnforceRemoveRearms(t *testing.T) {
	fake := &fakeSender{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), nil)
	pkg := enforcePkg()
	in := enforceInput(pkg, "sess-1", 20000)
	e.CheckInterim(in)
	e.Remove(SessionKey{NASID: "nas-1", SessionID: "sess-1"}) // Stop/Start
	e.CheckInterim(in)
	if disc, _ := fake.counts(); disc != 2 {
		t.Fatalf("after rearm: disc=%d, want 2", disc)
	}
	e.CheckInterim(enforceInput(pkg, "sess-2", 20000))
	e.RemoveNAS("nas-1") // Accounting-On/Off
	e.CheckInterim(enforceInput(pkg, "sess-2", 20000))
	if disc, _ := fake.counts(); disc != 4 {
		t.Fatalf("after NAS reset: disc=%d, want 4", disc)
	}
}

func TestEnforceNilSafe(t *testing.T) {
	var e *Enforcer
	e.CheckInterim(enforceInput(enforcePkg(), "s", 1)) // must not panic
	e.Remove(SessionKey{})
	e.RemoveNAS("x")
	var m *Metrics
	m.AddCoA()
	m.AddDisconnect()
	if m.CoASent() != 0 || m.Disconnects() != 0 {
		t.Fatal("nil metrics must read zero")
	}
}

func TestEnforceBestEffortOnSenderError(t *testing.T) {
	fake := &fakeSender{discErr: context.DeadlineExceeded}
	m := &Metrics{}
	e := NewEnforcerWithSender(fake, slog.New(slog.DiscardHandler), m)
	e.CheckInterim(enforceInput(enforcePkg(), "sess-1", 20000)) // must not panic
	if m.Disconnects() != 1 {
		t.Fatalf("attempts = %d, want 1 (counter counts attempts)", m.Disconnects())
	}
}

func TestHealthzEnforcementCounters(t *testing.T) {
	m := &Metrics{}
	m.AddCoA()
	m.AddCoA()
	m.AddDisconnect()
	h := &Health{Cache: testCache(), Writer: NewWriter(nil, slog.New(slog.DiscardHandler)),
		Metrics: m, Start: time.Now()}
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["cached_accounts"] != 3.0 {
		t.Fatalf("existing fields broken: %v", body)
	}
	if body["coa_sent"] != 2.0 || body["disconnects"] != 1.0 {
		t.Fatalf("counters = %v, want coa_sent=2 disconnects=1", body)
	}
}
