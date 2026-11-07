// Package kriskrisapp holds the kriskris payment use cases (Pay, Status,
// webhook confirmation) and the backup poller. Public surface dictated by
// the G2 contract: StartPoller + Service methods used by kriskrispres.
package kriskrisapp

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
	"boilerplate/api/internal/shared/id"
)

// Webhook event we act on (PRD §6.4: callback "invoice.paid").
const EventInvoicePaid = "invoice.paid"

// Service errors (presentation maps these to HTTP statuses).
var (
	// ErrInvoiceNotFound covers unknown invoices AND other customers'
	// invoices (IDOR-safe 404, never 403, to avoid leaking existence).
	ErrInvoiceNotFound = errors.New("kriskris: invoice not found")
	// ErrUnknownAttempt means kriskris referenced an attempt we never made.
	ErrUnknownAttempt = errors.New("kriskris: unknown attempt")
	// ErrNotPaidYet means re-verification shows the invoice unpaid.
	ErrNotPaidYet = errors.New("kriskris: remote invoice not paid")
)

// NotPayableError means the invoice is paid/void and cannot take payment.
type NotPayableError struct {
	Status string
}

// Error implements error.
func (e *NotPayableError) Error() string {
	return "kriskris: invoice not payable (" + e.Status + ")"
}

// Service orchestrates pay/confirm/status over Store + kriskris Client.
type Service struct {
	store  *kriskrisinfra.Store
	client *kriskrisinfra.Client
	config kriskrisinfra.Config
	// paidHook is the G3 apply-on-paid hook (nil = Fase 2 behavior).
	paidHook func(ctx context.Context, invoiceID string)

	now          func() time.Time
	sleep        func(time.Duration)
	asyncTimeout time.Duration
	log          *log.Logger
}

// NewService builds a Service over an explicit store+client (tests, C1).
func NewService(store *kriskrisinfra.Store, client *kriskrisinfra.Client, cfg kriskrisinfra.Config) *Service {
	return &Service{
		store:        store,
		client:       client,
		config:       cfg,
		now:          time.Now,
		sleep:        time.Sleep,
		asyncTimeout: 30 * time.Second,
		log:          log.Default(),
	}
}

// NewServiceFromPool loads config (env, else settings table) and builds a
// Service over pool. It returns an error when unconfigured; callers that
// cannot return errors (NewHandler, StartPoller) degrade gracefully.
func NewServiceFromPool(pool *sql.DB) (*Service, error) {
	cfg, err := kriskrisinfra.LoadConfig(pool)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return NewService(kriskrisinfra.NewStore(pool), kriskrisinfra.NewClient(cfg.BaseURL, cfg.APIKey, cfg.APISecret), cfg), nil
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(now func() time.Time) { s.now = now }

// SetSleep overrides backoff sleep (tests).
func (s *Service) SetSleep(sleep func(time.Duration)) { s.sleep = sleep }

// SetAsyncTimeout overrides the webhook background-processing timeout.
func (s *Service) SetAsyncTimeout(d time.Duration) { s.asyncTimeout = d }

// SetPaidHook installs the G3 apply-on-paid hook (nil = Fase 2 behavior).
// The hook fires after a confirmed payment commits; it must be idempotent
// and must never fail the confirmation (fire-and-log at the call site).
func (s *Service) SetPaidHook(hook func(ctx context.Context, invoiceID string)) {
	s.paidHook = hook
}

// AsyncTimeout returns the webhook background-processing timeout.
func (s *Service) AsyncTimeout() time.Duration { return s.asyncTimeout }

// CallbackSecret returns the webhook HMAC secret.
func (s *Service) CallbackSecret() string { return s.config.CallbackSecret }

// Store exposes the store for the poller/tests.
func (s *Service) Store() *kriskrisinfra.Store { return s.store }

// Client exposes the kriskris client for the poller.
func (s *Service) Client() *kriskrisinfra.Client { return s.client }

// PayResult is the portal payload for a fresh QR attempt.
type PayResult struct {
	AttemptID string
	QRPayload string
	ExpiresAt string // RFC3339 UTC
	Amount    int64  // base + unique code (what the customer transfers)
	Base      int64  // invoice total
	Code      int64  // unique code 10..99
	Message   string // UI sentence explaining the unique code (Indonesian)
}

// Pay cancels stale pending attempts, creates a kriskris invoice for
// base+unique-code, stores the attempt, and returns the QR payload.
// customerID scopes ownership ("" = admin, skips the check).
func (s *Service) Pay(ctx context.Context, invoiceID, customerID string) (*PayResult, error) {
	inv, err := s.store.GetInvoice(ctx, invoiceID)
	if err != nil {
		if errors.Is(err, kriskrisinfra.ErrNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}
	if customerID != "" && inv.CustomerID != customerID {
		return nil, ErrInvoiceNotFound
	}
	if inv.Status != kriskrisdomain.InvoiceUnpaid {
		return nil, &NotPayableError{Status: inv.Status}
	}

	// Cancel stale pending attempts first (PRD §6.4 step 2). Remote cancel
	// is best-effort: the local row is closed either way so only one QR is
	// ever "current"; a late payment on a superseded QR still settles via
	// ApplyPaidTx (see Store docs).
	now := s.now().UTC()
	stale, err := s.store.ListPendingAttempts(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	for _, att := range stale {
		if cerr := s.client.CancelInvoice(ctx, att.KriskrisID); cerr != nil {
			s.log.Printf("kriskris: cancel stale %s: %v", att.KriskrisID, cerr)
		}
		if err := s.store.SetAttemptStatus(ctx, att.ID, kriskrisdomain.AttemptCancelled, now); err != nil {
			return nil, err
		}
	}

	code, err := randomUniqueCode()
	if err != nil {
		return nil, err
	}
	amount := kriskrisdomain.TotalWithUniqueCode(inv.Total, code)

	remote, err := s.createWithRetry(ctx, amount, inv.Number)
	if err != nil {
		return nil, err
	}
	expires := remote.ExpiresAt
	if expires == "" {
		expires = now.Add(kriskrisdomain.QRValidity).Format(time.RFC3339)
	} else if t, perr := time.Parse(time.RFC3339, expires); perr == nil {
		expires = t.UTC().Format(time.RFC3339)
	}
	expTime, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return nil, fmt.Errorf("kriskris: bad expires_at %q: %w", expires, err)
	}
	att := &kriskrisinfra.Attempt{
		ID:         id.New(),
		InvoiceID:  inv.ID,
		KriskrisID: remote.ID,
		BaseAmount: inv.Total,
		UniqueCode: code,
		Amount:     amount,
		QRPayload:  remote.QRPayload,
		Status:     kriskrisdomain.AttemptPending,
		ExpiresAt:  expTime,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if att.QRPayload == "" {
		return nil, errors.New("kriskris: create response missing qr_payload")
	}
	if err := s.store.InsertAttempt(ctx, att); err != nil {
		return nil, err
	}
	return &PayResult{
		AttemptID: att.ID,
		QRPayload: att.QRPayload,
		ExpiresAt: expires,
		Amount:    amount,
		Base:      inv.Total,
		Code:      code,
		Message: fmt.Sprintf("Total %s, termasuk kode unik %d. Transfer tepat hingga 3 digit terakhir agar terverifikasi otomatis.",
			formatIDR(amount), code),
	}, nil
}

// createWithRetry creates the remote invoice, retrying duplicate_pending with
// a short backoff (PRD §6.4 table).
func (s *Service) createWithRetry(ctx context.Context, amount int64, label string) (*kriskrisinfra.RemoteInvoice, error) {
	backoffs := []time.Duration{500 * time.Millisecond, time.Second}
	err := error(nil)
	var inv *kriskrisinfra.RemoteInvoice
	for attempt := 0; ; attempt++ {
		inv, err = s.client.CreateInvoice(ctx, amount, label, int(kriskrisdomain.QRValidity.Minutes()))
		if err == nil {
			return inv, nil
		}
		if !kriskrisdomain.IsBizCode(err, kriskrisdomain.CodeDuplicatePending) || attempt >= len(backoffs) {
			return nil, err
		}
		s.log.Printf("kriskris: duplicate_pending, retry %d: %v", attempt+1, err)
		s.sleep(backoffs[attempt])
	}
}

func randomUniqueCode() (int64, error) {
	span := big.NewInt(kriskrisdomain.MaxUniqueCode - kriskrisdomain.MinUniqueCode + 1)
	n, err := rand.Int(rand.Reader, span)
	if err != nil {
		return 0, fmt.Errorf("kriskris: random code: %w", err)
	}
	return kriskrisdomain.MinUniqueCode + n.Int64(), nil
}

func formatIDR(n int64) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	b.WriteString("Rp")
	rem := len(s) % 3
	if rem == 0 {
		rem = 3
	}
	b.WriteString(s[:rem])
	for i := rem; i < len(s); i += 3 {
		b.WriteByte('.')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Claim records a webhook delivery; ok=false means duplicate (answer 2xx,
// ignore). Fast by design: the HTTP layer calls this synchronously and
// confirms asynchronously.
func (s *Service) Claim(ctx context.Context, deliveryID, event, payload string) (ok bool, err error) {
	return s.store.ClaimDelivery(ctx, deliveryID, event, payload, s.now().UTC())
}

// ConfirmPaid re-verifies one kriskris invoice (GET, never trust the
// callback payload) and applies payment when remote status is paid and the
// amount matches the attempt. Idempotent; safe to call from webhook and
// poller concurrently (single transactional apply in Store.ApplyPaidTx).
func (s *Service) ConfirmPaid(ctx context.Context, kriskrisID string) error {
	if kriskrisID == "" {
		return ErrUnknownAttempt
	}
	att, err := s.store.GetAttemptByKriskrisID(ctx, kriskrisID)
	if err != nil {
		if errors.Is(err, kriskrisinfra.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrUnknownAttempt, kriskrisID)
		}
		return err
	}
	if att.Status == kriskrisdomain.AttemptPaid {
		return nil
	}
	remote, err := s.client.GetInvoice(ctx, kriskrisID)
	if err != nil {
		return err
	}
	if remote.Status != kriskrisdomain.AttemptPaid {
		return fmt.Errorf("%w: remote status %q", ErrNotPaidYet, remote.Status)
	}
	if remote.Amount != att.Amount {
		// PRD §6.4: kriskris docs show inconsistent figures — never apply
		// on mismatch; leave pending for manual reconciliation.
		return fmt.Errorf("%w: attempt %d, remote %d", kriskrisinfra.ErrAmountMismatch, att.Amount, remote.Amount)
	}
	_, err = s.store.ApplyPaidTx(ctx, att.ID, remote.Amount, s.now().UTC())
	// G3 (agen D2, ADITIF): top-up apply-on-paid (best-effort, idempotent).
	if err == nil && s.paidHook != nil {
		s.paidHook(ctx, att.InvoiceID)
	}
	return err
}

// ProcessWebhookSync claims then confirms (used by tests and sync callers;
// the HTTP handler claims synchronously and confirms in a goroutine).
func (s *Service) ProcessWebhookSync(ctx context.Context, deliveryID, event, kriskrisID, payload string) (duplicate bool, err error) {
	ok, err := s.Claim(ctx, deliveryID, event, payload)
	if err != nil || !ok {
		return !ok && err == nil, err
	}
	if event != EventInvoicePaid || kriskrisID == "" {
		return false, nil
	}
	return false, s.ConfirmPaid(ctx, kriskrisID)
}

// AttemptView is the portal-facing attempt slice.
type AttemptView struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	Base      int64  `json:"base_amount"`
	Code      int64  `json:"unique_code"`
	QRPayload string `json:"qr_payload"`
	ExpiresAt string `json:"expires_at"`
}

// StatusResult is the portal polling payload.
type StatusResult struct {
	InvoiceID     string       `json:"invoice_id"`
	Number        string       `json:"number"`
	InvoiceStatus string       `json:"invoice_status"`
	Total         int64        `json:"total"`
	Attempt       *AttemptView `json:"attempt"`
}

// Status returns the invoice payment state plus the latest attempt (nil when
// the customer never pressed Pay). customerID scopes ownership ("" = admin).
func (s *Service) Status(ctx context.Context, invoiceID, customerID string) (*StatusResult, error) {
	inv, err := s.store.GetInvoice(ctx, invoiceID)
	if err != nil {
		if errors.Is(err, kriskrisinfra.ErrNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}
	if customerID != "" && inv.CustomerID != customerID {
		return nil, ErrInvoiceNotFound
	}
	att, err := s.store.GetLatestAttempt(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	res := &StatusResult{
		InvoiceID:     inv.ID,
		Number:        inv.Number,
		InvoiceStatus: inv.Status,
		Total:         inv.Total,
	}
	if att != nil {
		res.Attempt = &AttemptView{
			ID:        att.ID,
			Status:    att.Status,
			Amount:    att.Amount,
			Base:      att.BaseAmount,
			Code:      att.UniqueCode,
			QRPayload: att.QRPayload,
			ExpiresAt: att.ExpiresAt.UTC().Format(time.RFC3339),
		}
	}
	return res, nil
}
