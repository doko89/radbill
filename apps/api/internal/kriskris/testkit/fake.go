package kriskristest

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
)

// FakeInvoice is one remote invoice held by the fake.
type FakeInvoice struct {
	ID        string
	Label     string
	Amount    int64
	Status    string
	QRPayload string
	ExpiresAt string
}

// LoggedRequest records one call hitting the fake.
type LoggedRequest struct {
	Method      string
	Path        string
	RawBody     []byte
	Timestamp   string
	SignatureOK bool
	APIKey      string
}

// FakeKriskris is an httptest kriskris server: create/cancel/get/batch with
// server-side HMAC verification (wrong signatures get 401). The HMAC check
// recomputes the signature from the received values, so it verifies the
// client's signing path end to end (the canonical format itself is pinned by
// the fixed-vector domain test).
type FakeKriskris struct {
	T *testing.T

	APISecret string
	APIKey    string

	// OnCreate overrides invoice creation; return status code + JSON body.
	// When nil, invoices are created with status pending.
	OnCreate func(amount int64, label string) (int, any)

	mu       sync.Mutex
	seq      int
	invoices map[string]*FakeInvoice
	Requests []LoggedRequest

	Server *httptest.Server
}

// NewFake starts a fake kriskris server.
func NewFake(t *testing.T, apiKey, apiSecret string) *FakeKriskris {
	t.Helper()
	f := &FakeKriskris{
		T:         t,
		APISecret: apiSecret,
		APIKey:    apiKey,
		invoices:  map[string]*FakeInvoice{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/external/invoices", f.routeCollection)
	mux.HandleFunc("/api/v1/external/invoices/batch", f.routeBatch)
	mux.HandleFunc("/api/v1/external/invoices/", f.routeItem)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	return f
}

func (f *FakeKriskris) checkAuth(w http.ResponseWriter, r *http.Request, body []byte) bool {
	f.T.Helper()
	ts := r.Header.Get("X-Kriskris-Timestamp")
	sig := r.Header.Get("X-Kriskris-Signature")
	key := r.Header.Get("X-Kriskris-Api-Key")
	canonical := kriskrisdomain.CanonicalRequest(ts, r.Method, r.URL.Path, r.URL.RawQuery, body)
	want := kriskrisdomain.SignRequest(f.APISecret, canonical)
	okSig := subtle.ConstantTimeCompare([]byte(sig), []byte(want)) == 1 && sig != ""
	f.mu.Lock()
	f.Requests = append(f.Requests, LoggedRequest{
		Method: r.Method, Path: r.URL.Path, RawBody: append([]byte(nil), body...),
		Timestamp: ts, SignatureOK: okSig, APIKey: key,
	})
	f.mu.Unlock()
	if key != f.APIKey {
		writeErr(w, http.StatusUnauthorized, "invalid_key", "bad api key")
		return false
	}
	if !okSig {
		writeErr(w, http.StatusUnauthorized, "invalid_signature", "bad signature")
		return false
	}
	return true
}

func (f *FakeKriskris) routeCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	body := readAll(r)
	if !f.checkAuth(w, r, body) {
		return
	}
	var v struct {
		Amount        int64  `json:"amount"`
		Label         string `json:"label"`
		ExpiryMinutes int    `json:"expiry_minutes"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_amount", "bad json")
		return
	}
	if f.OnCreate != nil {
		code, payload := f.OnCreate(v.Amount, v.Label)
		writeJSON(w, code, payload)
		return
	}
	f.mu.Lock()
	f.seq++
	id := f.seq
	f.mu.Unlock()
	kid := "kk-test-" + itoaPad(id)
	exp := time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339)
	f.mu.Lock()
	f.invoices[kid] = &FakeInvoice{
		ID: kid, Label: v.Label, Amount: v.Amount,
		Status: "pending", QRPayload: "QR:" + kid + ":PAY", ExpiresAt: exp,
	}
	inv := *f.invoices[kid]
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"id": inv.ID, "qr_payload": inv.QRPayload, "amount": inv.Amount,
		"status": inv.Status, "expires_at": inv.ExpiresAt,
	}})
}

func (f *FakeKriskris) routeItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/external/invoices/")
	if strings.HasSuffix(rest, "/cancel") && r.Method == http.MethodPost {
		kid := strings.TrimSuffix(rest, "/cancel")
		body := readAll(r)
		if !f.checkAuth(w, r, body) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		inv, ok := f.invoices[kid]
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", "unknown invoice")
			return
		}
		if inv.Status == "pending" {
			inv.Status = "cancelled"
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": kid, "status": inv.Status}})
		return
	}
	if r.Method == http.MethodGet && !strings.Contains(rest, "/") {
		body := readAll(r)
		if !f.checkAuth(w, r, body) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		inv, ok := f.invoices[rest]
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", "unknown invoice")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"id": inv.ID, "qr_payload": inv.QRPayload, "amount": inv.Amount,
			"status": inv.Status, "expires_at": inv.ExpiresAt,
		}})
		return
	}
	writeErr(w, http.StatusNotFound, "not_found", "")
}

func (f *FakeKriskris) routeBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	body := readAll(r)
	if !f.checkAuth(w, r, body) {
		return
	}
	var v struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "bad json")
		return
	}
	if len(v.IDs) > kriskrisdomain.MaxBatchIDs {
		writeErr(w, http.StatusBadRequest, "invalid", "batch too large")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := make([]map[string]any, 0, len(v.IDs))
	for _, id := range v.IDs {
		if inv, ok := f.invoices[id]; ok {
			rows = append(rows, map[string]any{"id": inv.ID, "status": inv.Status, "amount": inv.Amount})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

func readAll(r *http.Request) []byte {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	return raw
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]any{"code": errCode, "message": msg}})
}

// SetStatus flips a remote invoice's status (and optionally amount).
func (f *FakeKriskris) SetStatus(kid, status string, amount int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if inv, ok := f.invoices[kid]; ok {
		inv.Status = status
		if amount >= 0 {
			inv.Amount = amount
		}
	}
}

// RemoteStatus returns a remote invoice's status/amount.
func (f *FakeKriskris) RemoteStatus(kid string) (string, int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inv, ok := f.invoices[kid]
	if !ok {
		return "", 0, false
	}
	return inv.Status, inv.Amount, true
}

// LastCreateBody returns the raw JSON body of the most recent create call.
func (f *FakeKriskris) LastCreateBody() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.Requests) - 1; i >= 0; i-- {
		if f.Requests[i].Method == http.MethodPost && f.Requests[i].Path == "/api/v1/external/invoices" {
			return f.Requests[i].RawBody
		}
	}
	return nil
}

// AllSignaturesOK reports whether every logged request verified.
func (f *FakeKriskris) AllSignaturesOK() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Requests) == 0 {
		return false
	}
	for _, r := range f.Requests {
		if !r.SignatureOK {
			return false
		}
	}
	return true
}

func itoaPad(n int) string {
	s := "000000"
	v := ""
	for x := n; x > 0; x /= 10 {
		v = string(rune('0'+x%10)) + v
	}
	if v == "" {
		v = "0"
	}
	if len(v) < len(s) {
		v = s[:len(s)-len(v)] + v
	}
	return v
}
