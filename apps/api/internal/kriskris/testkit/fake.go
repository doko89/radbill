package kriskristest

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"io"
	"math/big"
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
	Base      int64
	Code      int64
	Amount    int64
	Status    string
	QRPayload string
	PaidAt    string
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

// FakeKriskris is an httptest kriskris server mirroring the real contract
// (https://kriskris.id/panduan Bab 4): X-API-Key + "v1=" HMAC auth,
// server-picked unique codes, {ok,data} envelopes, GET status batch.
// The HMAC check recomputes the signature from the received values, so it
// verifies the client's signing path end to end (the canonical format
// itself is pinned by the fixed-vector domain test). Timestamp freshness
// is deliberately NOT enforced (tests pin the client clock; freshness is
// covered by the domain unit test).
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
	mux.HandleFunc("/api/v1/external/invoices/status", f.routeStatus)
	mux.HandleFunc("/api/v1/external/invoices/", f.routeItem)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	return f
}

func (f *FakeKriskris) checkAuth(w http.ResponseWriter, r *http.Request, body []byte) bool {
	f.T.Helper()
	ts := r.Header.Get("X-Kriskris-Timestamp")
	sig := r.Header.Get("X-Kriskris-Signature")
	key := r.Header.Get("X-API-Key")
	canonical := kriskrisdomain.CanonicalRequest(ts, r.Method, r.URL.Path, r.URL.RawQuery, body)
	want := "v1=" + kriskrisdomain.SignRequest(f.APISecret, canonical)
	okSig := subtle.ConstantTimeCompare([]byte(sig), []byte(want)) == 1 && sig != ""
	f.mu.Lock()
	f.Requests = append(f.Requests, LoggedRequest{
		Method: r.Method, Path: r.URL.Path, RawBody: append([]byte(nil), body...),
		Timestamp: ts, SignatureOK: okSig, APIKey: key,
	})
	f.mu.Unlock()
	if key != f.APIKey {
		writeErr(w, http.StatusUnauthorized, "", "invalid api key")
		return false
	}
	if !okSig {
		writeErr(w, http.StatusUnauthorized, "", "invalid signature")
		return false
	}
	return true
}

func (f *FakeKriskris) routeCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "", "")
		return
	}
	body := readAll(r)
	if !f.checkAuth(w, r, body) {
		return
	}
	var v struct {
		Amount int64  `json:"amount"`
		Label  string `json:"label"`
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
	if v.Amount < kriskrisdomain.MinInvoiceAmount {
		writeErr(w, http.StatusUnprocessableEntity, kriskrisdomain.CodeInvalidAmount,
			"amount below minimum Rp1.000")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	used := map[int64]bool{}
	for _, inv := range f.invoices {
		if inv.Status == "pending" {
			used[inv.Amount] = true
		}
	}
	code := int64(0)
	for range 90 {
		n, err := rand.Int(rand.Reader, big.NewInt(90))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", "rand failed")
			return
		}
		if c := n.Int64() + 10; !used[v.Amount+c] {
			code = c
			break
		}
	}
	if code == 0 {
		writeErr(w, http.StatusUnprocessableEntity, kriskrisdomain.CodeDuplicatePending,
			"no unique code left")
		return
	}
	f.seq++
	kid := "kk-test-" + itoaPad(f.seq)
	exp := time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339)
	inv := &FakeInvoice{
		ID: kid, Label: v.Label, Base: v.Amount, Code: code,
		Amount: v.Amount + code, Status: "pending",
		QRPayload: "QR:" + kid + ":PAY", ExpiresAt: exp,
	}
	f.invoices[kid] = inv
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": map[string]any{
		"id": inv.ID, "status": inv.Status, "amount": inv.Amount,
		"base_amount": inv.Base, "unique_code": inv.Code, "label": inv.Label,
		"qr_payload": inv.QRPayload, "expires_at": inv.ExpiresAt,
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
			writeErr(w, http.StatusNotFound, "", "unknown invoice")
			return
		}
		if inv.Status == "pending" {
			inv.Status = "cancelled"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"id": kid, "status": inv.Status}})
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
			writeErr(w, http.StatusNotFound, "", "unknown invoice")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{
			"id": inv.ID, "status": inv.Status, "amount": inv.Amount,
			"base_amount": inv.Base, "unique_code": inv.Code, "label": inv.Label,
			"qr_payload": inv.QRPayload, "paid_at": inv.PaidAt, "expires_at": inv.ExpiresAt,
		}})
		return
	}
	writeErr(w, http.StatusNotFound, "", "")
}

func (f *FakeKriskris) routeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "", "")
		return
	}
	body := readAll(r)
	if !f.checkAuth(w, r, body) {
		return
	}
	idsParam := r.URL.Query().Get("ids")
	var ids []string
	if idsParam != "" {
		ids = strings.Split(idsParam, ",")
	}
	if len(ids) > kriskrisdomain.MaxBatchIDs {
		writeErr(w, http.StatusBadRequest, "", "batch too large")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if inv, ok := f.invoices[id]; ok {
			rows = append(rows, map[string]any{
				"id": id, "found": true, "status": inv.Status,
				"amount": inv.Amount, "base_amount": inv.Base,
				"unique_code": inv.Code, "label": inv.Label,
				"paid_at": inv.PaidAt, "expires_at": inv.ExpiresAt,
			})
		} else {
			rows = append(rows, map[string]any{"id": id, "found": false})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": rows})
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

// writeErr emits the real failure envelope {ok:false,error,data:{code}}.
func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	body := map[string]any{"ok": false, "error": msg}
	if errCode != "" {
		body["data"] = map[string]any{"code": errCode}
	}
	writeJSON(w, code, body)
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
		if status == "paid" && inv.PaidAt == "" {
			inv.PaidAt = time.Now().UTC().Format(time.RFC3339)
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
