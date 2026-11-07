// Package kriskrispres adapts kriskris use cases to Fiber HTTP handlers.
//
// Auth contract (dictated by the G2 split; C1 guarantees it):
//
//   - C1 mounts Require-like JWT middleware BEFORE PayInvoice/AttemptStatus.
//   - The middleware stores the caller identity in Locals:
//     "billing_customer_id" (string; "" = admin, skips ownership checks) and
//     "billing_role" (string, informational). These handlers never parse JWT
//     themselves and never take customer identity from URL/body (anti-IDOR).
//   - Webhook is PUBLIC (no JWT): authenticity comes from HMAC-SHA256 over
//     the raw body. C1 must exempt POST /api/v1/webhooks/kriskris from the
//     boilerplate CSRF middleware (external callers carry no CSRF token)
//     while keeping CSRF on Pay/Status.
//
// Route params (C1 wiring): PayInvoice and AttemptStatus read the invoice id
// from c.Params("id"); AttemptStatus additionally honors ?attempt_id= for a
// single-attempt lookup. Suggested routes:
//
//	POST /api/v1/portal/invoices/:id/pay      -> PayInvoice   (JWT + CSRF)
//	GET  /api/v1/portal/invoices/:id/pay      -> AttemptStatus (JWT)
//	POST /api/v1/webhooks/kriskris            -> Webhook      (public, no CSRF)
package kriskrispres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	kriskrisapp "boilerplate/api/internal/kriskris/application"
	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
)

// Locals keys written by C1's auth middleware (see package doc).
const (
	LocalsCustomerID = "billing_customer_id"
	LocalsRole       = "billing_role"
)

// webhookHeader is the callback signature header (PRD §6.4: "sha256=<hex>"
// over the raw body).
const webhookHeader = "X-Kriskris-Signature"

// webhookTimestampHeader carries the callback timestamp when kriskris sends
// one; freshness (±5 min) is enforced only when present (ASSUMED name).
const webhookTimestampHeader = "X-Kriskris-Timestamp"

// Handler serves kriskris pay/status/webhook endpoints.
type Handler struct {
	pool    *sql.DB
	svc     *kriskrisapp.Service
	initErr error
}

// NewHandler builds a Handler over pool. It reads configuration itself:
// KRISKRIS_BASE_URL (else kriskris_base_url setting), KRISKRIS_API_KEY /
// KRISKRIS_API_SECRET / KRISKRIS_CALLBACK_SECRET (else encrypted settings,
// decrypted with SETTINGS_ENC_KEY). A config failure is stored and every
// endpoint answers generic 500 until configured (never leaks details).
func NewHandler(pool *sql.DB) *Handler {
	svc, err := kriskrisapp.NewServiceFromPool(pool)
	if err != nil {
		log.Printf("kriskris: unconfigured: %v", err)
	}
	return &Handler{pool: pool, svc: svc, initErr: err}
}

// NewHandlerWithService builds a Handler over an explicit service (tests and
// C1 custom wiring). pool may be nil when the service owns storage.
func NewHandlerWithService(pool *sql.DB, svc *kriskrisapp.Service) *Handler {
	return &Handler{pool: pool, svc: svc}
}

// SetPaidHook forwards the G3 apply-on-paid hook to the service (no-op
// while the handler is unconfigured).
func (h *Handler) SetPaidHook(hook func(ctx context.Context, invoiceID string)) {
	if h.svc != nil {
		h.svc.SetPaidHook(hook)
	}
}

// PayInvoice creates a QR attempt for invoice :id: {data:{attempt_id,
// qr_payload, expires_at, amount, base_amount, unique_code, message}}.
func (h *Handler) PayInvoice(c fiber.Ctx) error {
	if h.initErr != nil || h.svc == nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
	}
	invoiceID := strings.TrimSpace(c.Params("id"))
	if invoiceID == "" {
		return writeError(c, fiber.StatusBadRequest, "invalid", "missing invoice id")
	}
	res, err := h.svc.Pay(c.Context(), invoiceID, localsString(c, LocalsCustomerID))
	if err != nil {
		return h.mapPayError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": fiber.Map{
		"attempt_id":  res.AttemptID,
		"qr_payload":  res.QRPayload,
		"expires_at":  res.ExpiresAt,
		"amount":      res.Amount,
		"base_amount": res.Base,
		"unique_code": res.Code,
		"message":     res.Message,
	}})
}

func (h *Handler) mapPayError(c fiber.Ctx, err error) error {
	var npe *kriskrisapp.NotPayableError
	switch {
	case errors.Is(err, kriskrisapp.ErrInvoiceNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "invoice not found")
	case errors.As(err, &npe):
		if npe.Status == kriskrisdomain.InvoicePaid {
			return writeError(c, fiber.StatusConflict, "conflict", "invoice already paid")
		}
		return writeError(c, fiber.StatusConflict, "conflict", "invoice is void")
	}
	// Business codes (PRD §6.4): gobiz_offline/quota_exceeded are 503 with a
	// machine-readable code so the frontend hides the pay button and offers
	// manual payment. Messages are user-safe Indonesian.
	switch kriskrisdomain.BizCode(err) {
	case kriskrisdomain.CodeGobizOffline:
		log.Printf("kriskris: gobiz_offline: %v", err)
		return writeError(c, fiber.StatusServiceUnavailable, kriskrisdomain.CodeGobizOffline,
			"Layanan QRIS sedang offline. Silakan bayar manual atau coba lagi nanti.")
	case kriskrisdomain.CodeQuotaExceeded:
		log.Printf("kriskris: quota_exceeded: %v", err)
		return writeError(c, fiber.StatusServiceUnavailable, kriskrisdomain.CodeQuotaExceeded,
			"Kuota pembayaran QRIS habis. Silakan bayar manual atau hubungi admin.")
	case kriskrisdomain.CodeDuplicatePending:
		log.Printf("kriskris: duplicate_pending exhausted: %v", err)
		return writeError(c, fiber.StatusServiceUnavailable, kriskrisdomain.CodeDuplicatePending,
			"Pembayaran sedang diproses. Coba lagi beberapa saat.")
	case kriskrisdomain.CodeInvalidAmount:
		log.Printf("kriskris: invalid_amount: %v", err)
	}
	log.Printf("kriskris: pay: %v", err)
	return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
}

// AttemptStatus serves GET /portal/payment-attempts/:id. The :id is an
// ATTEMPT id (portal polling) and returns the flat attempt view; invoice
// ids keep the legacy StatusResult shape ({data:{invoice_id, number,
// invoice_status, total, attempt}}), or a single attempt with ?attempt_id=.
func (h *Handler) AttemptStatus(c fiber.Ctx) error {
	if h.initErr != nil || h.svc == nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
	}
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return writeError(c, fiber.StatusBadRequest, "invalid", "missing id")
	}
	customerID := localsString(c, LocalsCustomerID)
	if att, err := h.svc.Store().GetAttempt(c.Context(), id); err == nil {
		inv, err := h.svc.Store().GetInvoice(c.Context(), att.InvoiceID)
		if err != nil || (customerID != "" && inv.CustomerID != customerID) {
			return writeError(c, fiber.StatusNotFound, "not_found", "attempt not found")
		}
		var paidAt any
		if att.PaidAt != nil {
			paidAt = att.PaidAt.UTC().Format(time.RFC3339)
		}
		return c.JSON(fiber.Map{"data": fiber.Map{
			"id":          att.ID,
			"invoice_id":  att.InvoiceID,
			"status":      att.Status,
			"amount":      att.Amount,
			"base_amount": att.BaseAmount,
			"unique_code": att.UniqueCode,
			"qr_payload":  att.QRPayload,
			"expires_at":  att.ExpiresAt.UTC().Format(time.RFC3339),
			"paid_at":     paidAt,
		}})
	}
	invoiceID := id
	if attemptID := strings.TrimSpace(c.Query("attempt_id")); attemptID != "" {
		return h.attemptStatus(c, invoiceID, attemptID, customerID)
	}
	res, err := h.svc.Status(c.Context(), invoiceID, customerID)
	if err != nil {
		if errors.Is(err, kriskrisapp.ErrInvoiceNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "invoice not found")
		}
		log.Printf("kriskris: status: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
	}
	return c.JSON(fiber.Map{"data": res})
}

func (h *Handler) attemptStatus(c fiber.Ctx, invoiceID, attemptID, customerID string) error {
	res, err := h.svc.Status(c.Context(), invoiceID, customerID)
	if err != nil {
		if errors.Is(err, kriskrisapp.ErrInvoiceNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "invoice not found")
		}
		log.Printf("kriskris: status: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
	}
	// ?attempt_id= pins one attempt; anything else for this invoice is 404
	// (prevents cross-invoice probing).
	if res.Attempt == nil || res.Attempt.ID != attemptID {
		att, err := h.svc.Store().GetAttempt(c.Context(), attemptID)
		if err != nil || att.InvoiceID != invoiceID {
			return writeError(c, fiber.StatusNotFound, "not_found", "attempt not found")
		}
		res.Attempt = &kriskrisapp.AttemptView{
			ID:        att.ID,
			Status:    att.Status,
			Amount:    att.Amount,
			Base:      att.BaseAmount,
			Code:      att.UniqueCode,
			QRPayload: att.QRPayload,
			ExpiresAt: att.ExpiresAt.UTC().Format(time.RFC3339),
		}
	}
	return c.JSON(fiber.Map{"data": res})
}

// Webhook handles POST /api/v1/webhooks/kriskris (public, HMAC-verified).
// It verifies the signature, claims the delivery (idempotent), and answers
// 2xx FAST (<10s: claim is one indexed insert); confirmation (GET
// re-verification + transactional apply) runs in a goroutine with timeout.
func (h *Handler) Webhook(c fiber.Ctx) error {
	if h.initErr != nil || h.svc == nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
	}
	if h.svc.Store().ConfirmMode(c.Context()) == kriskrisinfra.ModePoll {
		// Poll mode: confirmations come from the poller only (private
		// network); callbacks are disabled, not silently swallowed.
		// Read live (not boot config) so flipping the setting in
		// /admin/settings takes effect without a restart.
		return writeError(c, fiber.StatusNotFound, "webhook_disabled", "webhooks disabled in poll mode")
	}
	raw := append([]byte(nil), c.Body()...) // copy: fasthttp reuses buffers
	secret := h.svc.CallbackSecret()
	if secret == "" || !kriskrisdomain.VerifyWebhookSignature(secret, raw, c.Get(webhookHeader)) {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "bad signature")
	}
	if ts := strings.TrimSpace(c.Get(webhookTimestampHeader)); ts != "" {
		t, err := kriskrisdomain.ParseTimestamp(ts)
		if err != nil || !kriskrisdomain.TimestampFresh(t, time.Now().UTC()) {
			return writeError(c, fiber.StatusUnauthorized, "unauthorized", "stale timestamp")
		}
	}
	parsed, err := parseWebhook(raw)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid payload")
	}
	claimed, err := h.svc.Claim(c.Context(), parsed.deliveryID, parsed.event, string(raw))
	if err != nil {
		log.Printf("kriskris: webhook claim: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "internal", "payment unavailable")
	}
	if !claimed {
		return c.JSON(fiber.Map{"data": fiber.Map{"duplicate": true}}) // 200, ignore
	}
	if parsed.kriskrisID == "" {
		return c.JSON(fiber.Map{"data": fiber.Map{"received": true}}) // 200, ignore
	}
	switch parsed.event {
	case kriskrisapp.EventInvoiceExpired:
		// Fast synchronous close (one indexed update); idempotent.
		if err := h.svc.ExpireAttempt(c.Context(), parsed.kriskrisID); err != nil {
			log.Printf("kriskris: webhook expire %s: %v", parsed.kriskrisID, err)
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"received": true}})
	case kriskrisapp.EventInvoicePaid:
		// Confirmed in a goroutine below.
	default:
		return c.JSON(fiber.Map{"data": fiber.Map{"received": true}}) // 200, ignore
	}
	svc := h.svc
	kid := parsed.kriskrisID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), svc.AsyncTimeout())
		defer cancel()
		if err := svc.ConfirmPaid(ctx, kid); err != nil {
			// Logged, not retried here: the poller re-verifies pending
			// attempts every ~5s, so a transient failure self-heals.
			log.Printf("kriskris: webhook confirm %s: %v", kid, err)
		}
	}()
	return c.JSON(fiber.Map{"data": fiber.Map{"received": true}})
}

// webhookPayload is the tolerant parse of a kriskris callback. Real shape
// (guide Bab 5) is flat: delivery_id/event/invoice_id (+merchant_id,
// amount, ... ignored). Aliases (deliveryId/type/data.id) are accepted.
// NEVER trusted for settlement — ConfirmPaid re-fetches via GET.
type webhookPayload struct {
	deliveryID string
	event      string
	kriskrisID string
}

func parseWebhook(raw []byte) (*webhookPayload, error) {
	var v struct {
		DeliveryID string `json:"delivery_id"`
		DeliveryId string `json:"deliveryId"`
		ID         string `json:"id"`
		Event      string `json:"event"`
		Type       string `json:"type"`
		InvoiceID  string `json:"invoice_id"`
		InvoiceId  string `json:"invoiceId"`
		Data       *struct {
			ID        string `json:"id"`
			InvoiceID string `json:"invoice_id"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	p := &webhookPayload{
		event: firstNonEmpty(v.Event, v.Type),
	}
	if v.Data != nil {
		p.kriskrisID = firstNonEmpty(v.Data.ID, v.Data.InvoiceID)
	}
	if p.kriskrisID == "" {
		p.kriskrisID = firstNonEmpty(v.InvoiceID, v.InvoiceId)
	}
	p.deliveryID = firstNonEmpty(v.DeliveryID, v.DeliveryId)
	if p.deliveryID == "" {
		if p.event != "" && p.kriskrisID != "" {
			// Fallback idempotency key: one claim per (event, invoice).
			p.deliveryID = "evt:" + p.event + ":" + p.kriskrisID
		} else {
			sum := sha256.Sum256(raw)
			p.deliveryID = "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	return p, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// localsString reads a string Locals value tolerantly (C1 sets plain strings).
func localsString(c fiber.Ctx, key string) string {
	switch v := c.Locals(key).(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}
