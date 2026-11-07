package kriskrisinfra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	kriskrisdomain "boilerplate/api/internal/kriskris/domain"
	"boilerplate/api/internal/shared/id"
)

// Table/column contract (DDL owned by C1; this file only reads/writes):
//
//	payment_attempts(id, invoice_id, kriskris_invoice_id UNIQUE, base_amount,
//	  unique_code, amount, qr_payload, status, expires_at, paid_at,
//	  created_at, updated_at)
//	webhook_deliveries(delivery_id PK, event, payload, received_at)
//	invoices(id, number UNIQUE, pppoe_account_id, customer_id, total,
//	  due_date, status, issued_at, paid_at, created_at, updated_at)
//	payments(id, invoice_id, amount, method, attempt_id, note, paid_at,
//	  created_at)   -- note: TEXT, '' for base rows, 'unique_code' for code rows
//	pppoe_accounts(id, customer_id, package_id, status, expires_at, ...)
//	packages(id, validity_seconds NULL, ...)
//	settings(key, value, updated_at)
//
// All SQL runs on both sqlite and postgres: $1 placeholders, TEXT ids,
// BIGINT amounts, RFC3339 TEXT timestamps in UTC.

// ErrNotFound is returned for unknown invoices/attempts.
var ErrNotFound = errors.New("kriskris: not found")

// ErrAmountMismatch means the paid amount differs from the attempt amount.
var ErrAmountMismatch = errors.New("kriskris: amount mismatch")

// ErrInvoiceVoid means the invoice is void so payment cannot be applied.
var ErrInvoiceVoid = errors.New("kriskris: invoice void")

// notifyChannel/payload mirror G1 network/notify (channel radbill_events,
// {"type":"pppoe","action":"upsert","id":...}) without importing it.
const notifyChannel = "radbill_events"

// Store is hand-written SQL storage (no sqlc, per task constraints).
type Store struct {
	pool *sql.DB
}

// NewStore builds a Store over pool.
func NewStore(pool *sql.DB) *Store {
	return &Store{pool: pool}
}

// Invoice is the billing slice needed for payment.
type Invoice struct {
	ID             string
	Number         string
	PPPoEAccountID string
	CustomerID     string
	Total          int64
	Status         string
}

// Attempt is one QR payment attempt.
type Attempt struct {
	ID         string
	InvoiceID  string
	KriskrisID string
	BaseAmount int64
	UniqueCode int64
	Amount     int64
	QRPayload  string
	Status     string
	ExpiresAt  time.Time
	PaidAt     *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// GetInvoice returns one invoice or ErrNotFound.
func (s *Store) GetInvoice(ctx context.Context, invoiceID string) (*Invoice, error) {
	inv := &Invoice{}
	err := s.pool.QueryRowContext(ctx, `
SELECT id, number, pppoe_account_id, customer_id, total, status
FROM invoices WHERE id=$1`, invoiceID,
	).Scan(&inv.ID, &inv.Number, &inv.PPPoEAccountID, &inv.CustomerID, &inv.Total, &inv.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("kriskris: get invoice: %w", err)
	}
	return inv, nil
}

// GetAttempt returns one attempt by local id or ErrNotFound.
func (s *Store) GetAttempt(ctx context.Context, attemptID string) (*Attempt, error) {
	return s.scanAttempt(ctx, `SELECT id, invoice_id, kriskris_invoice_id, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at
FROM payment_attempts WHERE id=$1`, attemptID)
}

// GetAttemptByKriskrisID returns one attempt by remote id or ErrNotFound.
func (s *Store) GetAttemptByKriskrisID(ctx context.Context, kriskrisID string) (*Attempt, error) {
	return s.scanAttempt(ctx, `SELECT id, invoice_id, kriskris_invoice_id, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at
FROM payment_attempts WHERE kriskris_invoice_id=$1`, kriskrisID)
}

// GetLatestAttempt returns the newest attempt for an invoice, or (nil, nil).
func (s *Store) GetLatestAttempt(ctx context.Context, invoiceID string) (*Attempt, error) {
	att, err := s.scanAttempt(ctx, `SELECT id, invoice_id, kriskris_invoice_id, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at
FROM payment_attempts WHERE invoice_id=$1 ORDER BY created_at DESC LIMIT 1`, invoiceID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return att, err
}

// ListPendingAttempts returns pending attempts of one invoice (oldest first).
func (s *Store) ListPendingAttempts(ctx context.Context, invoiceID string) ([]*Attempt, error) {
	return s.listAttempts(ctx, `SELECT id, invoice_id, kriskris_invoice_id, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at
FROM payment_attempts WHERE invoice_id=$1 AND status='pending' ORDER BY created_at ASC`, invoiceID)
}

// ListPendingBatch returns up to limit pending attempts across invoices
// (oldest first) for the poller.
func (s *Store) ListPendingBatch(ctx context.Context, limit int) ([]*Attempt, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.listAttempts(ctx, `SELECT id, invoice_id, kriskris_invoice_id, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at
FROM payment_attempts WHERE status='pending' ORDER BY created_at ASC LIMIT `+itoa(limit))
}

func (s *Store) listAttempts(ctx context.Context, query string, args ...any) ([]*Attempt, error) {
	rows, err := s.pool.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("kriskris: list attempts: %w", err)
	}
	defer rows.Close()
	var out []*Attempt
	for rows.Next() {
		att, err := scanAttemptRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, rows.Err()
}

func (s *Store) scanAttempt(ctx context.Context, query string, args ...any) (*Attempt, error) {
	row := s.pool.QueryRowContext(ctx, query, args...)
	att, err := scanAttemptRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return att, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAttemptRow(row scannable) (*Attempt, error) {
	var (
		att                           Attempt
		expiresAt, createdAt, updated string
		paidAt                        sql.NullString
	)
	if err := row.Scan(&att.ID, &att.InvoiceID, &att.KriskrisID, &att.BaseAmount,
		&att.UniqueCode, &att.Amount, &att.QRPayload, &att.Status,
		&expiresAt, &paidAt, &createdAt, &updated); err != nil {
		return nil, err
	}
	var err error
	if att.ExpiresAt, err = time.Parse(time.RFC3339, expiresAt); err != nil {
		return nil, fmt.Errorf("kriskris: parse attempt expires_at: %w", err)
	}
	if att.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return nil, fmt.Errorf("kriskris: parse attempt created_at: %w", err)
	}
	if att.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
		return nil, fmt.Errorf("kriskris: parse attempt updated_at: %w", err)
	}
	if paidAt.Valid && paidAt.String != "" {
		t, err := time.Parse(time.RFC3339, paidAt.String)
		if err != nil {
			return nil, fmt.Errorf("kriskris: parse attempt paid_at: %w", err)
		}
		att.PaidAt = &t
	}
	return &att, nil
}

// InsertAttempt stores a new pending attempt.
func (s *Store) InsertAttempt(ctx context.Context, att *Attempt) error {
	stamp := att.CreatedAt.UTC().Format(time.RFC3339)
	if stamp == "0001-01-01T00:00:00Z" {
		stamp = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.pool.ExecContext(ctx, `
INSERT INTO payment_attempts (id, invoice_id, kriskris_invoice_id, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL,$10,$10)`,
		att.ID, att.InvoiceID, att.KriskrisID, att.BaseAmount, att.UniqueCode,
		att.Amount, att.QRPayload, kriskrisdomain.AttemptPending,
		att.ExpiresAt.UTC().Format(time.RFC3339), stamp)
	if err != nil {
		return fmt.Errorf("kriskris: insert attempt: %w", err)
	}
	return nil
}

// SetAttemptStatus moves one attempt to status (paid_at set when paid).
func (s *Store) SetAttemptStatus(ctx context.Context, attemptID, status string, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339)
	var paidAt any
	if status == kriskrisdomain.AttemptPaid {
		paidAt = stamp
	}
	_, err := s.pool.ExecContext(ctx, `
UPDATE payment_attempts SET status=$1, paid_at=COALESCE($2, paid_at), updated_at=$3 WHERE id=$4`,
		status, paidAt, stamp, attemptID)
	if err != nil {
		return fmt.Errorf("kriskris: set attempt status: %w", err)
	}
	return nil
}

// MarkExpired flips pending attempts to expired (best-effort per id).
func (s *Store) MarkExpired(ctx context.Context, attemptIDs []string, now time.Time) error {
	for _, aid := range attemptIDs {
		if _, err := s.pool.ExecContext(ctx, `
UPDATE payment_attempts SET status='expired', updated_at=$1 WHERE id=$2 AND status='pending'`,
			now.UTC().Format(time.RFC3339), aid); err != nil {
			return fmt.Errorf("kriskris: mark expired: %w", err)
		}
	}
	return nil
}

// ClaimDelivery inserts a webhook delivery; ok=false means a duplicate that
// must be answered 2xx and ignored. Works on sqlite and postgres.
func (s *Store) ClaimDelivery(ctx context.Context, deliveryID, event, payload string, now time.Time) (ok bool, err error) {
	res, err := s.pool.ExecContext(ctx, `
INSERT INTO webhook_deliveries (delivery_id, event, payload, received_at)
VALUES ($1,$2,$3,$4) ON CONFLICT(delivery_id) DO NOTHING`,
		deliveryID, event, payload, now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("kriskris: claim delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("kriskris: claim delivery rows: %w", err)
	}
	return n == 1, nil
}

// ApplyPaidTx applies a confirmed payment in ONE transaction (used by both
// webhook and poller): invoice→paid, two payment rows (base + unique code),
// PPPoE expiry extension, isolated→active. pg_notify is emitted best-effort
// AFTER commit. Idempotent: an already-paid attempt returns applied=false.
//
// Attempts in pending/cancelled/expired are all applicable: a customer may
// legitimately pay a superseded/expired QR (late transfer); the money still
// settles the invoice and manual reconciliation is avoided.
func (s *Store) ApplyPaidTx(ctx context.Context, attemptID string, paidAmount int64, now time.Time) (applied bool, err error) {
	now = now.UTC()
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var (
		invoiceID, status  string
		baseAmount, amount int64
	)
	err = tx.QueryRowContext(ctx, `
SELECT invoice_id, status, base_amount, amount FROM payment_attempts WHERE id=$1`, attemptID,
	).Scan(&invoiceID, &status, &baseAmount, &amount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("kriskris: apply read attempt: %w", err)
	}
	if status == kriskrisdomain.AttemptPaid {
		return false, tx.Commit() // idempotent replay
	}
	if status != kriskrisdomain.AttemptPending && status != kriskrisdomain.AttemptCancelled &&
		status != kriskrisdomain.AttemptExpired {
		return false, fmt.Errorf("kriskris: attempt %s has status %s", attemptID, status)
	}
	if paidAmount != amount {
		return false, fmt.Errorf("%w: attempt wants %d, paid %d", ErrAmountMismatch, amount, paidAmount)
	}

	var invStatus, pppoeID string
	if err := tx.QueryRowContext(ctx, `SELECT status, pppoe_account_id FROM invoices WHERE id=$1`, invoiceID).Scan(&invStatus, &pppoeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("kriskris: apply: invoice gone: %w", ErrNotFound)
		}
		return false, fmt.Errorf("kriskris: apply read invoice: %w", err)
	}
	switch invStatus {
	case kriskrisdomain.InvoicePaid:
		// Paid by another path (manual/concurrent): close the attempt, no-op.
		if err := updateAttemptInTx(ctx, tx, attemptID, kriskrisdomain.AttemptPaid, now); err != nil {
			return false, err
		}
		return false, tx.Commit()
	case kriskrisdomain.InvoiceVoid:
		// Money arrived for a void invoice: park the attempt as failed for
		// manual reconciliation and stop polling it.
		if err := updateAttemptInTx(ctx, tx, attemptID, kriskrisdomain.AttemptFailed, now); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, ErrInvoiceVoid
	case kriskrisdomain.InvoiceUnpaid:
		// continue below
	default:
		return false, fmt.Errorf("kriskris: unknown invoice status %q", invStatus)
	}

	stamp := now.Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `
UPDATE invoices SET status='paid', paid_at=$1, updated_at=$1 WHERE id=$2 AND status='unpaid'`, stamp, invoiceID)
	if err != nil {
		return false, fmt.Errorf("kriskris: apply invoice: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		// Lost a race with another payer: re-read; paid wins as no-op.
		var st string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM invoices WHERE id=$1`, invoiceID).Scan(&st); err != nil {
			return false, err
		}
		if st == kriskrisdomain.InvoicePaid {
			if err := updateAttemptInTx(ctx, tx, attemptID, kriskrisdomain.AttemptPaid, now); err != nil {
				return false, err
			}
			return false, tx.Commit()
		}
		return false, fmt.Errorf("kriskris: invoice %s not payable (%s)", invoiceID, st)
	}

	// Base row settles the invoice; code row is other income (note='unique_code').
	if _, err := tx.ExecContext(ctx, `
INSERT INTO payments (id, invoice_id, amount, method, attempt_id, note, paid_at, created_at)
VALUES ($1,$2,$3,'kriskris',$4,'',$5,$5)`, id.New(), invoiceID, baseAmount, attemptID, stamp); err != nil {
		return false, fmt.Errorf("kriskris: apply base payment: %w", err)
	}
	uniqueCode := amount - baseAmount
	if _, err := tx.ExecContext(ctx, `
INSERT INTO payments (id, invoice_id, amount, method, attempt_id, note, paid_at, created_at)
VALUES ($1,$2,$3,'kriskris',$4,'unique_code',$5,$5)`, id.New(), invoiceID, uniqueCode, attemptID, stamp); err != nil {
		return false, fmt.Errorf("kriskris: apply code payment: %w", err)
	}

	if err := extendPPPoEInTx(ctx, tx, pppoeID, now); err != nil {
		return false, err
	}
	if err := updateAttemptInTx(ctx, tx, attemptID, kriskrisdomain.AttemptPaid, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("kriskris: apply commit: %w", err)
	}

	// Best-effort cache invalidation for radiusd. Probed blind: on postgres
	// pg_notify delivers; on sqlite the statement errors and is ignored
	// (radiusd falls back to polling). Deliberately AFTER commit.
	emitNotify(ctx, s.pool, pppoeID)
	return true, nil
}

func updateAttemptInTx(ctx context.Context, tx *sql.Tx, attemptID, status string, now time.Time) error {
	stamp := now.Format(time.RFC3339)
	var paidAt any
	if status == kriskrisdomain.AttemptPaid {
		paidAt = stamp
	}
	_, err := tx.ExecContext(ctx, `
UPDATE payment_attempts SET status=$1, paid_at=COALESCE($2, paid_at), updated_at=$3 WHERE id=$4`,
		status, paidAt, stamp, attemptID)
	if err != nil {
		return fmt.Errorf("kriskris: update attempt: %w", err)
	}
	return nil
}

// extendPPPoEInTx extends expiry by package validity (30-day fallback) from
// max(now, current expiry) and reactivates isolated accounts.
func extendPPPoEInTx(ctx context.Context, tx *sql.Tx, pppoeID string, now time.Time) error {
	var (
		status    string
		expiresAt sql.NullString
		packageID string
	)
	if err := tx.QueryRowContext(ctx, `SELECT status, expires_at, package_id FROM pppoe_accounts WHERE id=$1`, pppoeID).Scan(&status, &expiresAt, &packageID); err != nil {
		return fmt.Errorf("kriskris: apply read pppoe: %w", err)
	}
	var validity sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT validity_seconds FROM packages WHERE id=$1`, packageID).Scan(&validity); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("kriskris: apply read package: %w", err)
	}
	var validityPtr *int64
	if validity.Valid {
		validityPtr = &validity.Int64
	}
	var current *time.Time
	if expiresAt.Valid && expiresAt.String != "" {
		t, err := time.Parse(time.RFC3339, expiresAt.String)
		if err != nil {
			return fmt.Errorf("kriskris: parse pppoe expires_at: %w", err)
		}
		current = &t
	}
	next := kriskrisdomain.ExtensionBase(current, now).Add(kriskrisdomain.ExtensionPeriod(validityPtr))
	if status == "isolated" {
		status = "active"
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE pppoe_accounts SET status=$1, expires_at=$2, updated_at=$3 WHERE id=$4`,
		status, next.Format(time.RFC3339), now.Format(time.RFC3339), pppoeID); err != nil {
		return fmt.Errorf("kriskris: apply pppoe: %w", err)
	}
	return nil
}

// emitNotify tries pg_notify and ignores every error (sqlite path).
func emitNotify(ctx context.Context, pool *sql.DB, pppoeID string) {
	payload := `{"type":"pppoe","action":"upsert","id":` + quoteJSON(pppoeID) + `}`
	if _, err := pool.ExecContext(ctx, `SELECT pg_notify('`+notifyChannel+`', $1)`, payload); err != nil {
		log.Printf("kriskris: pg_notify skipped (%v)", err)
	}
}

func quoteJSON(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\\' {
			out = append(out, '\\', c)
		} else if c < 0x20 {
			out = append(out, '?')
		} else {
			out = append(out, c)
		}
	}
	return string(append(out, '"'))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
