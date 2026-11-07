// Package kriskrisinfra implements the kriskris HTTP client, the encrypted
// settings loader, and hand-written SQL storage (database/sql, $1
// placeholders, RFC3339 TEXT timestamps — no sqlc, to avoid regen conflicts).
package kriskrisinfra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
)

// kriskris API assumptions (no official client spec was available; every
// choice is pinned here and mirrored in ../README.md so C1/orchestrator can
// correct them in one place):
//
//   - Base URL: KRISKRIS_BASE_URL env or kriskris_base_url setting, e.g.
//     "https://api.kriskris.example". Paths below are appended to it.
//   - Request auth: HMAC-SHA256 over the 5-line canonical string
//     (PRD §6.4), sent as X-Kriskris-Signature, with the request timestamp
//     in X-Kriskris-Timestamp (RFC3339 UTC) and the API key identity in
//     X-Kriskris-Api-Key. Header names are ASSUMED.
//   - POST /api/v1/external/invoices with JSON body
//     {"amount":<int rupiah>,"label":<invoice number>,"expiry_minutes":15}.
//     Field names are ASSUMED. Response: {"data":{"id","qr_payload","amount",
//     "status","expires_at"}} (a flat object without "data" is also accepted;
//     "qr_string"/"qr_data" are accepted as qr_payload aliases).
//   - POST /api/v1/external/invoices/{id}/cancel with body {} — ASSUMED path.
//     404 / already-cancelled is treated as success (idempotent cancel).
//   - GET /api/v1/external/invoices/{id} — ASSUMED full path (PRD writes
//     "GET /external/invoices/:id"). 404 maps to ErrRemoteNotFound.
//   - POST /api/v1/external/invoices/batch with {"ids":[...]} (max 50),
//     response {"data":[{"id","status","amount"},...]} — path AND shape
//     ASSUMED ("invoices" envelope key also accepted).
//   - Error envelope {"error":{"code","message"}} ("code" at top level also
//     accepted). Business codes: duplicate_pending, gobiz_offline,
//     quota_exceeded, invalid_amount (PRD §6.4).
const (
	headerTimestamp = "X-Kriskris-Timestamp"
	headerSignature = "X-Kriskris-Signature"
	headerAPIKey    = "X-Kriskris-Api-Key"

	pathCreate = "/api/v1/external/invoices"
	pathBatch  = "/api/v1/external/invoices/batch"
)

// ErrRemoteNotFound means kriskris has no such invoice (HTTP 404 on GET).
var ErrRemoteNotFound = fmt.Errorf("kriskris: invoice not found")

// Client talks to the kriskris API.
type Client struct {
	baseURL   string
	apiKey    string
	apiSecret string

	// HTTP is the underlying client (default: 10s timeout). Overridable in tests.
	HTTP *http.Client
	// Now returns the current time (default: time.Now). Overridable in tests.
	Now func() time.Time
}

// NewClient builds a Client. baseURL has trailing "/" trimmed.
func NewClient(baseURL, apiKey, apiSecret string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		apiSecret: apiSecret,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		Now:       time.Now,
	}
}

// RemoteInvoice is a kriskris invoice as decoded from create/get responses.
type RemoteInvoice struct {
	ID        string
	Status    string // pending|paid|expired|cancelled|failed (lowercased)
	Amount    int64
	QRPayload string
	ExpiresAt string // RFC3339, may be "" when the server omits it
}

// BatchItem is one row of a batch-status response.
type BatchItem struct {
	ID     string
	Status string // lowercased
	Amount int64
}

// CreateInvoice creates a kriskris QR invoice. amount is integer rupiah
// (base + unique code), label is our invoice number.
func (c *Client) CreateInvoice(ctx context.Context, amount int64, label string, expiryMinutes int) (*RemoteInvoice, error) {
	body, err := json.Marshal(map[string]any{
		"amount":         amount,
		"label":          label,
		"expiry_minutes": expiryMinutes,
	})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, pathCreate, "", body)
	if err != nil {
		return nil, err
	}
	inv, err := decodeInvoice(raw)
	if err != nil {
		return nil, err
	}
	if inv.ID == "" {
		return nil, fmt.Errorf("kriskris: create response missing id")
	}
	return inv, nil
}

// CancelInvoice cancels a kriskris invoice. Unknown/already-cancelled remote
// invoices are success (nil) so re-pay after a half-failed cancel is safe.
func (c *Client) CancelInvoice(ctx context.Context, kriskrisID string) error {
	body := []byte(`{}`)
	raw, err := c.do(ctx, http.MethodPost, pathCreate+"/"+escapeID(kriskrisID)+"/cancel", "", body)
	if err != nil {
		if biz := kriskrisdomain.BizCode(err); biz == "not_found" || biz == "already_cancelled" {
			return nil
		}
		return err
	}
	_ = raw
	return nil
}

// GetInvoice fetches one kriskris invoice (webhook re-verification path).
func (c *Client) GetInvoice(ctx context.Context, kriskrisID string) (*RemoteInvoice, error) {
	raw, err := c.do(ctx, http.MethodGet, pathCreate+"/"+escapeID(kriskrisID), "", nil)
	if err != nil {
		return nil, err
	}
	return decodeInvoice(raw)
}

// BatchStatus fetches statuses for up to MaxBatchIDs invoices in one call.
func (c *Client) BatchStatus(ctx context.Context, ids []string) ([]BatchItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > kriskrisdomain.MaxBatchIDs {
		return nil, fmt.Errorf("kriskris: batch of %d exceeds max %d", len(ids), kriskrisdomain.MaxBatchIDs)
	}
	body, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, pathBatch, "", body)
	if err != nil {
		return nil, err
	}
	return decodeBatch(raw)
}

func (c *Client) do(ctx context.Context, method, path, rawQuery string, body []byte) ([]byte, error) {
	u := c.baseURL + path
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	stamp := c.Now().UTC().Format(time.RFC3339)
	canonical := kriskrisdomain.CanonicalRequest(stamp, method, path, rawQuery, body)
	req.Header.Set(headerTimestamp, stamp)
	req.Header.Set(headerSignature, kriskrisdomain.SignRequest(c.apiSecret, canonical))
	req.Header.Set(headerAPIKey, c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kriskris: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("kriskris: read %s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	return nil, asRequestError(method, path, resp.StatusCode, raw)
}

func asRequestError(method, path string, status int, raw []byte) error {
	code, message := decodeErrorEnvelope(raw)
	if code == "" {
		code = fmt.Sprintf("http_%d", status)
	}
	if status == http.StatusNotFound && method == http.MethodGet {
		return fmt.Errorf("%w (GET %s)", ErrRemoteNotFound, path)
	}
	if status == http.StatusNotFound {
		// Non-GET 404 (e.g. cancel of an unknown invoice): surface as a
		// business code so CancelInvoice can treat it as success.
		return &kriskrisdomain.BizError{Code: "not_found", Message: message}
	}
	if biz := kriskrisdomain.NewBizError(code, message); biz != nil {
		return biz
	}
	return fmt.Errorf("kriskris: %s %s: status %d", method, path, status)
}

func decodeErrorEnvelope(raw []byte) (code, message string) {
	var v struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", strings.TrimSpace(string(raw))
	}
	if v.Error != nil && v.Error.Code != "" {
		return v.Error.Code, v.Error.Message
	}
	return v.Code, v.Message
}

func decodeInvoice(raw []byte) (*RemoteInvoice, error) {
	// Envelope first ({"data":{...}}), then flat ({...}).
	var env struct {
		Data *invoiceJSON `json:"data"`
	}
	var src invoiceJSON
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("kriskris: decode invoice: %w", err)
	}
	if env.Data != nil {
		src = *env.Data
	} else if err := json.Unmarshal(raw, &src); err != nil {
		return nil, fmt.Errorf("kriskris: decode invoice: %w", err)
	}
	inv := &RemoteInvoice{
		ID:        src.ID,
		Status:    strings.ToLower(strings.TrimSpace(src.Status)),
		Amount:    src.Amount,
		QRPayload: firstNonEmpty(src.QRPayload, src.QRString, src.QRData),
		ExpiresAt: strings.TrimSpace(src.ExpiresAt),
	}
	if id := strings.TrimSpace(src.InvoiceID); inv.ID == "" {
		inv.ID = id
	}
	return inv, nil
}

type invoiceJSON struct {
	ID        string `json:"id"`
	InvoiceID string `json:"invoice_id"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	QRPayload string `json:"qr_payload"`
	QRString  string `json:"qr_string"`
	QRData    string `json:"qr_data"`
	ExpiresAt string `json:"expires_at"`
}

func decodeBatch(raw []byte) ([]BatchItem, error) {
	var v struct {
		Data     []invoiceJSON `json:"data"`
		Invoices []invoiceJSON `json:"invoices"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("kriskris: decode batch: %w", err)
	}
	rows := v.Data
	if rows == nil {
		rows = v.Invoices
	}
	out := make([]BatchItem, 0, len(rows))
	for _, r := range rows {
		id := r.ID
		if id == "" {
			id = r.InvoiceID
		}
		out = append(out, BatchItem{
			ID:     id,
			Status: strings.ToLower(strings.TrimSpace(r.Status)),
			Amount: r.Amount,
		})
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// escapeID escapes one path segment (kriskris invoice id).
func escapeID(id string) string {
	return url.PathEscape(id)
}
