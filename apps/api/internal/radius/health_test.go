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
