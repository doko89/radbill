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

// kriskris API contract (https://kriskris.id/panduan, Bab 4; verified
// 2026-11-07, mirrored in ../README.md):
//
//   - Base URL: KRISKRIS_BASE_URL env or kriskris_base_url setting,
//     default "https://kriskris.id". Paths below are appended to it.
//   - Request auth: HMAC-SHA256 over the 5-line canonical string, sent as
//     X-Kriskris-Signature ("v1="+hex), timestamp in X-Kriskris-Timestamp
//     (RFC3339 UTC, ±5 min), key identity in X-API-Key. 60 req/min/key.
//   - POST /api/v1/external/invoices with JSON body
//     {"amount":<base rupiah>,"label":<invoice number>}. The SERVER picks
//     the unique code 10..99; response 201:
//     {"ok":true,"data":{"id","status","amount","base_amount",
//     "unique_code","label","qr_payload","expires_at"}}.
//   - POST /api/v1/external/invoices/{id}/cancel with body {}.
//     404 / already-cancelled is treated as success (idempotent cancel).
//   - GET /api/v1/external/invoices/{id} returns id/status/amount/
//     base_amount/unique_code/label/paid_at/expires_at.
//     404 maps to ErrRemoteNotFound.
//   - GET /api/v1/external/invoices/status?ids=a,b (max 50, query is the
//     signed 4th canonical line), response {"ok":true,"data":[
//     {"id","found","status","amount","base_amount","unique_code",
//     "label","paid_at","expires_at"}, ...]}.
//   - Errors: {"ok":false,"error":"msg","data":{"code"}} with 422 for
//     business codes (duplicate_pending, gobiz_offline, quota_exceeded,
//     invalid_amount); 429 carries data.retry_after (seconds).
const (
	headerTimestamp = "X-Kriskris-Timestamp"
	headerSignature = "X-Kriskris-Signature"
	headerAPIKey    = "X-API-Key"

	// signatureScheme prefixes request signatures: "v1="+hex(HMAC).
	signatureScheme = "v1"

	pathCreate = "/api/v1/external/invoices"
	pathStatus = "/api/v1/external/invoices/status"
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
	ID         string
	Status     string // pending|paid|expired|cancelled (lowercased)
	Amount     int64  // base + unique code (transfer total)
	BaseAmount int64  // our base, echoed by the server
	UniqueCode int64  // server-picked 10..99
	Label      string
	QRPayload  string
	PaidAt     string // RFC3339, "" unless paid
	ExpiresAt  string // RFC3339, may be "" when the server omits it
}

// BatchItem is one row of a batch-status response.
type BatchItem struct {
	ID         string
	Found      bool
	Status     string // lowercased
	Amount     int64
	BaseAmount int64
	UniqueCode int64
}

// CreateInvoice creates a kriskris QR invoice. baseAmount is integer rupiah
// (our invoice total); the server adds the unique code. label is our
// invoice number (optional upstream, max 64 chars).
func (c *Client) CreateInvoice(ctx context.Context, baseAmount int64, label string) (*RemoteInvoice, error) {
	body, err := json.Marshal(map[string]any{
		"amount": baseAmount,
		"label":  label,
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

// BatchStatus fetches statuses for up to MaxBatchIDs invoices in one call
// via GET <pathStatus>?ids=a,b (the raw query is the signed 4th line).
func (c *Client) BatchStatus(ctx context.Context, ids []string) ([]BatchItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > kriskrisdomain.MaxBatchIDs {
		return nil, fmt.Errorf("kriskris: batch of %d exceeds max %d", len(ids), kriskrisdomain.MaxBatchIDs)
	}
	enc := make([]string, 0, len(ids))
	for _, id := range ids {
		enc = append(enc, url.QueryEscape(id))
	}
	raw, err := c.do(ctx, http.MethodGet, pathStatus, "ids="+strings.Join(enc, ","), nil)
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
	req.Header.Set(headerSignature, signatureScheme+"="+kriskrisdomain.SignRequest(c.apiSecret, canonical))
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
	code, message := decodeErrorEnvelope(status, raw)
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

func decodeErrorEnvelope(status int, raw []byte) (code, message string) {
	var v struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
		Data    *struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		if status == http.StatusTooManyRequests {
			return kriskrisdomain.CodeRateLimited, strings.TrimSpace(string(raw))
		}
		return "", strings.TrimSpace(string(raw))
	}
	var errStr string
	var errObj struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if len(v.Error) > 0 {
		if err := json.Unmarshal(v.Error, &errStr); err != nil {
			_ = json.Unmarshal(v.Error, &errObj)
		}
	}
	// Real shape: {"ok":false,"error":"msg","data":{"code",...}}.
	if v.Data != nil && v.Data.Code != "" {
		return v.Data.Code, errStr
	}
	if errObj.Code != "" {
		return errObj.Code, errObj.Message
	}
	if v.Code != "" {
		return v.Code, v.Message
	}
	if errStr != "" {
		if status == http.StatusTooManyRequests {
			return kriskrisdomain.CodeRateLimited, errStr
		}
		return "", errStr
	}
	if status == http.StatusTooManyRequests {
		return kriskrisdomain.CodeRateLimited, ""
	}
	return "", ""
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
		ID:         src.ID,
		Status:     strings.ToLower(strings.TrimSpace(src.Status)),
		Amount:     src.Amount,
		BaseAmount: src.BaseAmount,
		UniqueCode: src.UniqueCode,
		Label:      strings.TrimSpace(src.Label),
		QRPayload:  firstNonEmpty(src.QRPayload, src.QRString, src.QRData),
		PaidAt:     strings.TrimSpace(src.PaidAt),
		ExpiresAt:  strings.TrimSpace(src.ExpiresAt),
	}
	if id := strings.TrimSpace(src.InvoiceID); inv.ID == "" {
		inv.ID = id
	}
	return inv, nil
}

type invoiceJSON struct {
	ID         string `json:"id"`
	InvoiceID  string `json:"invoice_id"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	BaseAmount int64  `json:"base_amount"`
	UniqueCode int64  `json:"unique_code"`
	Label      string `json:"label"`
	QRPayload  string `json:"qr_payload"`
	QRString   string `json:"qr_string"`
	QRData     string `json:"qr_data"`
	PaidAt     string `json:"paid_at"`
	ExpiresAt  string `json:"expires_at"`
	Found      *bool  `json:"found"`
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
		found := true
		if r.Found != nil {
			found = *r.Found
		}
		out = append(out, BatchItem{
			ID:         id,
			Found:      found,
			Status:     strings.ToLower(strings.TrimSpace(r.Status)),
			Amount:     r.Amount,
			BaseAmount: r.BaseAmount,
			UniqueCode: r.UniqueCode,
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
