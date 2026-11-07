package radius

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	h := &Health{Cache: testCache(), Writer: NewWriter(nil, slog.New(slog.DiscardHandler)),
		Start: time.Now().Add(-61 * time.Second)}
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Status         string `json:"status"`
		CachedAccounts int    `json:"cached_accounts"`
		CachedNAS      int    `json:"cached_nas"`
		AcctQueue      int    `json:"acct_queue"`
		UptimeSeconds  int64  `json:"uptime_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.CachedAccounts != 3 || body.CachedNAS != 1 || body.AcctQueue != 0 {
		t.Fatalf("body = %+v", body)
	}
	if body.UptimeSeconds < 60 {
		t.Fatalf("uptime = %d", body.UptimeSeconds)
	}

	resp2, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status = %d", resp2.StatusCode)
	}
}

// TestHealthzG4Counters is the Fase 4 additive check (agen E1): the new
// auth_accept, auth_reject and acct_received fields carry the Metrics
// values while every pre-existing field keeps its shape.
func TestHealthzG4Counters(t *testing.T) {
	m := &Metrics{}
	m.AddAuthAccept()
	m.AddAuthAccept()
	m.AddAuthReject()
	m.AddAcctReceived()
	h := &Health{Cache: testCache(), Writer: NewWriter(nil, slog.New(slog.DiscardHandler)),
		Metrics: m, Start: time.Now().Add(-61 * time.Second)}
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Status         string `json:"status"`
		CachedAccounts int    `json:"cached_accounts"`
		CachedNAS      int    `json:"cached_nas"`
		AcctQueue      int    `json:"acct_queue"`
		UptimeSeconds  int64  `json:"uptime_seconds"`
		CoASent        int64  `json:"coa_sent"`
		Disconnects    int64  `json:"disconnects"`
		AuthAccept     int64  `json:"auth_accept"`
		AuthReject     int64  `json:"auth_reject"`
		AcctReceived   int64  `json:"acct_received"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.CachedAccounts != 3 || body.CachedNAS != 1 {
		t.Fatalf("legacy fields changed: %+v", body)
	}
	if body.AuthAccept != 2 || body.AuthReject != 1 || body.AcctReceived != 1 {
		t.Fatalf("g4 counters = %+v, want 2/1/1", body)
	}
	// A nil Metrics reports zeros instead of panicking.
	h2 := &Health{Cache: testCache(), Writer: NewWriter(nil, slog.New(slog.DiscardHandler)),
		Start: time.Now()}
	srv2 := httptest.NewServer(h2.Handler())
	defer srv2.Close()
	resp2, err := http.Get(srv2.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var zero struct {
		AuthAccept   int64 `json:"auth_accept"`
		AuthReject   int64 `json:"auth_reject"`
		AcctReceived int64 `json:"acct_received"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&zero); err != nil {
		t.Fatal(err)
	}
	if zero != (struct {
		AuthAccept   int64 `json:"auth_accept"`
		AuthReject   int64 `json:"auth_reject"`
		AcctReceived int64 `json:"acct_received"`
	}{}) {
		t.Fatalf("nil metrics = %+v, want zeros", zero)
	}
}
