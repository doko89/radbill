package radius

import (
	"encoding/json"
	"net/http"
	"time"
)

// Health serves the /healthz endpoint over stdlib net/http.
type Health struct {
	Cache  *Cache
	Writer *Writer
	Start  time.Time
}

// healthBody is the exact GET /healthz JSON shape.
type healthBody struct {
	Status         string `json:"status"`
	CachedAccounts int    `json:"cached_accounts"`
	CachedNAS      int    `json:"cached_nas"`
	AcctQueue      int    `json:"acct_queue"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
}

// Handler returns an http.Handler serving GET /healthz (404 otherwise).
func (h *Health) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		accts, nas := h.Cache.Counts()
		body := healthBody{
			Status:         "ok",
			CachedAccounts: accts,
			CachedNAS:      nas,
			AcctQueue:      h.Writer.QueueLen(),
			UptimeSeconds:  int64(time.Since(h.Start).Seconds()),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	return mux
}
