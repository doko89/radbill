// Package portalpres serves the self-contained customer portal API
// (agen C3, G2). Hand-written database/sql only ($1 placeholders, TEXT ids,
// RFC3339 TEXT timestamps; no sqlc). C1 registers the four methods:
//
//	Summary       GET /api/v1/portal/summary
//	Invoices      GET /api/v1/portal/invoices
//	InvoiceDetail GET /api/v1/portal/invoices/:id
//	Usage         GET /api/v1/portal/usage?days=30
//
// User context comes ONLY from c.Locals("billing_customer_id") (string, set
// by C1 from JWT claims). Empty means admin/bypass: queries run unscoped.
// Customer-scoped callers NEVER read customer identity from URL/body
// (anti-IDOR); another customer's invoice detail returns 404.
//
// PPPoE passwords are never selected, let alone serialized.
package portalpres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"boilerplate/api/internal/shared/apperr"
)

// customerLocalsKey is the dictated Locals key C1 fills from JWT claims.
const customerLocalsKey = "billing_customer_id"

// maxUsageDays caps ?days= on Usage.
const maxUsageDays = 90

// defaultUsageDays is the fallback window on Usage.
const defaultUsageDays = 30

// Handler serves /api/v1/portal/* endpoints.
type Handler struct {
	pool *sql.DB
}

// NewHandler builds a Handler on the shared pool. C1 imports exactly this.
func NewHandler(pool *sql.DB) *Handler {
	return &Handler{pool: pool}
}

// customerOf returns the dictated customer scope: "" = admin/bypass
// (unscoped), otherwise every query MUST filter by it.
func customerOf(c fiber.Ctx) string {
	id, _ := c.Locals(customerLocalsKey).(string)
	return id
}

// CustomerDTO is the public shape of the portal owner.
type CustomerDTO struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Phone *string `json:"phone"`
}

// PackageDTO is the public subset of a package (no secrets).
type PackageDTO struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Price              int64   `json:"price"`
	RateLimit          *string `json:"rate_limit"`
	QuotaBytes         *int64  `json:"quota_bytes"`
	OnlineSecondsLimit *int64  `json:"online_seconds_limit"`
	ValiditySeconds    *int64  `json:"validity_seconds"`
}

// AccountDTO is one PPPoE account of the customer (1:N per PRD §6.1).
// Password is deliberately absent.
type AccountDTO struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Status    string     `json:"status"`
	ExpiresAt *string    `json:"expires_at"`
	Package   PackageDTO `json:"package"`
}

// QuotaDTO reports quota as-is from package + counters (no enforcement; G3).
type QuotaDTO struct {
	LimitBytes     *int64 `json:"limit_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	RemainingBytes *int64 `json:"remaining_bytes"`
}

// OnlineTimeDTO reports online-time as-is from package + counters (G3).
type OnlineTimeDTO struct {
	LimitSeconds     *int64 `json:"limit_seconds"`
	UsedSeconds      int64  `json:"used_seconds"`
	RemainingSeconds *int64 `json:"remaining_seconds"`
}

// DueDTO is the nearest unpaid invoice, if any.
type DueDTO struct {
	InvoiceID string `json:"invoice_id"`
	Number    string `json:"number"`
	Total     int64  `json:"total"`
	DueDate   string `json:"due_date"`
}

// SummaryDTO is the GET /api/v1/portal/summary shape.
type SummaryDTO struct {
	Customer         *CustomerDTO  `json:"customer"`
	Accounts         []AccountDTO  `json:"accounts"`
	DefaultAccountID *string       `json:"default_account_id"`
	Quota            QuotaDTO      `json:"quota"`
	OnlineTime       OnlineTimeDTO `json:"online_time"`
	Due              *DueDTO       `json:"due"`
	UnpaidCount      int64         `json:"unpaid_count"`
	UnpaidTotal      int64         `json:"unpaid_total"`
}

// PendingAttemptDTO is a resumable QR attempt on an invoice.
type PendingAttemptDTO struct {
	ID        string `json:"id"`
	Amount    int64  `json:"amount"`
	QRPayload string `json:"qr_payload"`
	ExpiresAt string `json:"expires_at"`
}

// InvoiceDTO mirrors the billing list shape plus owner labels.
type InvoiceDTO struct {
	ID             string             `json:"id"`
	Number         string             `json:"number"`
	PPPoEAccountID string             `json:"pppoe_account_id"`
	PPPoEUsername  string             `json:"pppoe_username"`
	PackageName    string             `json:"package_name"`
	Total          int64              `json:"total"`
	DueDate        string             `json:"due_date"`
	Status         string             `json:"status"`
	IssuedAt       string             `json:"issued_at"`
	PaidAt         *string            `json:"paid_at"`
	PendingAttempt *PendingAttemptDTO `json:"pending_attempt"`
}

// InvoiceItemDTO is one invoice line.
type InvoiceItemDTO struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Qty         int64  `json:"qty"`
	Price       int64  `json:"price"`
}

// PaymentDTO is one recorded payment.
type PaymentDTO struct {
	ID        string  `json:"id"`
	Amount    int64   `json:"amount"`
	Method    string  `json:"method"`
	AttemptID *string `json:"attempt_id"`
	PaidAt    string  `json:"paid_at"`
}

// InvoiceDetailDTO is GET /api/v1/portal/invoices/:id.
type InvoiceDetailDTO struct {
	InvoiceDTO
	Items    []InvoiceItemDTO `json:"items"`
	Payments []PaymentDTO     `json:"payments"`
}

// UsagePointDTO is one day of usage.
type UsagePointDTO struct {
	Date          string `json:"date"`
	BytesIn       int64  `json:"bytes_in"`
	BytesOut      int64  `json:"bytes_out"`
	OnlineSeconds int64  `json:"online_seconds"`
}

// UsageTotalsDTO sums usage_counters over the scoped accounts.
type UsageTotalsDTO struct {
	BytesIn       int64 `json:"bytes_in"`
	BytesOut      int64 `json:"bytes_out"`
	OnlineSeconds int64 `json:"online_seconds"`
}

// UsageDTO is GET /api/v1/portal/usage.
type UsageDTO struct {
	Days   int             `json:"days"`
	Points []UsagePointDTO `json:"points"`
	Totals UsageTotalsDTO  `json:"totals"`
}

// Summary returns service status, remaining quota/time as-is, nearest due,
// the active package, and the account list (multi-account per PRD 1:N;
// quota/online-time summarize the default = first account).
func (h *Handler) Summary(c fiber.Ctx) error {
	ctx := c.Context()
	custID := customerOf(c)

	accounts, err := h.listAccounts(ctx, custID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}

	var customer *CustomerDTO
	if custID != "" {
		customer, err = h.getCustomer(ctx, custID)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
		}
	}

	out := SummaryDTO{Accounts: accounts}
	if customer != nil {
		out.Customer = customer
	}
	var defPkg *PackageDTO
	if len(accounts) > 0 {
		id := accounts[0].ID
		out.DefaultAccountID = &id
		pkg := accounts[0].Package
		defPkg = &pkg
		usedIn, usedOut, usedOnline, err := h.counterSums(ctx, accounts[0].ID)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
		}
		usedBytes := usedIn + usedOut
		out.Quota.UsedBytes = usedBytes
		out.Quota.LimitBytes = pkg.QuotaBytes
		if pkg.QuotaBytes != nil {
			rem := *pkg.QuotaBytes - usedBytes
			if rem < 0 {
				rem = 0
			}
			out.Quota.RemainingBytes = &rem
		}
		out.OnlineTime.UsedSeconds = usedOnline
		out.OnlineTime.LimitSeconds = pkg.OnlineSecondsLimit
		if pkg.OnlineSecondsLimit != nil {
			rem := *pkg.OnlineSecondsLimit - usedOnline
			if rem < 0 {
				rem = 0
			}
			out.OnlineTime.RemainingSeconds = &rem
		}
	}
	_ = defPkg

	due, err := h.nearestDue(ctx, custID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	out.Due = due

	count, total, err := h.unpaidSums(ctx, custID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	out.UnpaidCount = count
	out.UnpaidTotal = total

	return c.JSON(fiber.Map{"data": out})
}

// Invoices lists the caller's own invoices, newest first, each with its
// pending QR attempt (if any) for the "lanjutkan bayar" button.
// Optional ?status=unpaid|paid|void filters.
func (h *Handler) Invoices(c fiber.Ctx) error {
	ctx := c.Context()
	status := c.Query("status")
	switch status {
	case "", "unpaid", "paid", "void":
	default:
		return writeFieldError(c, apperr.Invalid("status", "must be unpaid|paid|void"))
	}

	rows, err := h.listInvoices(ctx, customerOf(c), status)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	if err := h.attachPending(ctx, rows); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": rows, "total": len(rows)})
}

// InvoiceDetail returns one own invoice with items, payments, and its
// pending QR attempt. Another customer's id (or unknown id) is 404.
func (h *Handler) InvoiceDetail(c fiber.Ctx) error {
	ctx := c.Context()
	id := c.Params("id")
	if id == "" {
		return writeError(c, fiber.StatusNotFound, "not_found", "invoice not found")
	}
	inv, err := h.getInvoice(ctx, customerOf(c), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return writeError(c, fiber.StatusNotFound, "not_found", "invoice not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	items, err := h.listItems(ctx, inv.ID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	payments, err := h.listPayments(ctx, inv.ID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	if err := h.attachPending(ctx, []*InvoiceDTO{inv}); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": InvoiceDetailDTO{
		InvoiceDTO: *inv,
		Items:      items,
		Payments:   payments,
	}})
}

// Usage returns a daily bytes up/down + online-seconds series for the last
// ?days= (default 30, max 90). Points come from radius_sessions grouped by
// day in Go (portable across sqlite/postgres); totals come from
// usage_counters summed over the scoped accounts.
func (h *Handler) Usage(c fiber.Ctx) error {
	ctx := c.Context()
	days := defaultUsageDays
	if raw := c.Query("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return writeFieldError(c, apperr.Invalid("days", "must be an integer 1..90"))
		}
		if n < 1 || n > maxUsageDays {
			return writeFieldError(c, apperr.Invalid("days", "must be an integer 1..90"))
		}
		days = n
	}

	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)
	points := make([]UsagePointDTO, days)
	for i := range points {
		points[i] = UsagePointDTO{Date: cutoff.AddDate(0, 0, i).Format("2006-01-02")}
	}
	index := make(map[string]int, days)
	for i := range points {
		index[points[i].Date] = i
	}

	accountIDs, err := h.scopedAccountIDs(ctx, customerOf(c))
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	if len(accountIDs) > 0 {
		if err := h.foldSessions(ctx, accountIDs, cutoff.Format(time.RFC3339), now, index, points); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
		}
	}
	totals, err := h.usageTotals(ctx, accountIDs)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": UsageDTO{Days: days, Points: points, Totals: totals}})
}

// --- queries (hand SQL, sqlite+postgres subset) ---

func (h *Handler) getCustomer(ctx context.Context, custID string) (*CustomerDTO, error) {
	const q = `SELECT id, name, phone FROM customers WHERE id = $1`
	var out CustomerDTO
	var phone sql.NullString
	if err := h.pool.QueryRowContext(ctx, q, custID).Scan(&out.ID, &out.Name, &phone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	out.Phone = nullStr(phone)
	return &out, nil
}

func (h *Handler) listAccounts(ctx context.Context, custID string) ([]AccountDTO, error) {
	const base = `SELECT a.id, a.username, a.status, a.expires_at,
		p.id, p.name, p.price, p.rate_limit, p.quota_bytes, p.online_seconds_limit, p.validity_seconds
		FROM pppoe_accounts a JOIN packages p ON p.id = a.package_id`
	var rows *sql.Rows
	var err error
	if custID == "" {
		rows, err = h.pool.QueryContext(ctx, base+` ORDER BY a.username ASC, a.id ASC LIMIT 200`)
	} else {
		rows, err = h.pool.QueryContext(ctx, base+` WHERE a.customer_id = $1 ORDER BY a.username ASC, a.id ASC`, custID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccountDTO{}
	for rows.Next() {
		var a AccountDTO
		var expires, rate sql.NullString
		var quota, online, validity sql.NullInt64
		if err := rows.Scan(&a.ID, &a.Username, &a.Status, &expires,
			&a.Package.ID, &a.Package.Name, &a.Package.Price, &rate, &quota, &online, &validity); err != nil {
			return nil, err
		}
		a.ExpiresAt = nullStr(expires)
		a.Package.RateLimit = nullStr(rate)
		a.Package.QuotaBytes = nullInt(quota)
		a.Package.OnlineSecondsLimit = nullInt(online)
		a.Package.ValiditySeconds = nullInt(validity)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (h *Handler) counterSums(ctx context.Context, accountID string) (in, out, online int64, err error) {
	const q = `SELECT COALESCE(SUM(bytes_in),0), COALESCE(SUM(bytes_out),0), COALESCE(SUM(online_seconds),0)
		FROM usage_counters WHERE pppoe_account_id = $1`
	err = h.pool.QueryRowContext(ctx, q, accountID).Scan(&in, &out, &online)
	return in, out, online, err
}

func (h *Handler) nearestDue(ctx context.Context, custID string) (*DueDTO, error) {
	const base = `SELECT id, number, total, due_date FROM invoices WHERE status = 'unpaid'`
	var row *sql.Row
	if custID == "" {
		row = h.pool.QueryRowContext(ctx, base+` ORDER BY due_date ASC, id ASC LIMIT 1`)
	} else {
		row = h.pool.QueryRowContext(ctx, base+` AND customer_id = $1 ORDER BY due_date ASC, id ASC LIMIT 1`, custID)
	}
	var d DueDTO
	if err := row.Scan(&d.InvoiceID, &d.Number, &d.Total, &d.DueDate); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (h *Handler) unpaidSums(ctx context.Context, custID string) (count, total int64, err error) {
	const base = `SELECT COUNT(*), COALESCE(SUM(total),0) FROM invoices WHERE status = 'unpaid'`
	if custID == "" {
		err = h.pool.QueryRowContext(ctx, base).Scan(&count, &total)
	} else {
		err = h.pool.QueryRowContext(ctx, base+` AND customer_id = $1`, custID).Scan(&count, &total)
	}
	return count, total, err
}

const invoiceCols = `SELECT i.id, i.number, i.pppoe_account_id, a.username, p.name,
	i.total, i.due_date, i.status, i.issued_at, i.paid_at
	FROM invoices i
	JOIN pppoe_accounts a ON a.id = i.pppoe_account_id
	JOIN packages p ON p.id = a.package_id`

func (h *Handler) listInvoices(ctx context.Context, custID, status string) ([]*InvoiceDTO, error) {
	q := invoiceCols
	var args []any
	switch {
	case custID != "" && status != "":
		q += ` WHERE i.customer_id = $1 AND i.status = $2`
		args = []any{custID, status}
	case custID != "":
		q += ` WHERE i.customer_id = $1`
		args = []any{custID}
	case status != "":
		q += ` WHERE i.status = $1`
		args = []any{status}
	}
	q += ` ORDER BY i.issued_at DESC, i.id DESC LIMIT 200`
	rows, err := h.pool.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*InvoiceDTO{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (h *Handler) getInvoice(ctx context.Context, custID, id string) (*InvoiceDTO, error) {
	q := invoiceCols + ` WHERE i.id = $1`
	args := []any{id}
	if custID != "" {
		q += ` AND i.customer_id = $2`
		args = append(args, custID)
	}
	row := h.pool.QueryRowContext(ctx, q, args...)
	var paid sql.NullString
	var inv InvoiceDTO
	err := row.Scan(&inv.ID, &inv.Number, &inv.PPPoEAccountID, &inv.PPPoEUsername,
		&inv.PackageName, &inv.Total, &inv.DueDate, &inv.Status, &inv.IssuedAt, &paid)
	if err != nil {
		return nil, err
	}
	inv.PaidAt = nullStr(paid)
	return &inv, nil
}

type invoiceScanner interface {
	Scan(dest ...any) error
}

func scanInvoice(row invoiceScanner) (*InvoiceDTO, error) {
	var inv InvoiceDTO
	var paid sql.NullString
	if err := row.Scan(&inv.ID, &inv.Number, &inv.PPPoEAccountID, &inv.PPPoEUsername,
		&inv.PackageName, &inv.Total, &inv.DueDate, &inv.Status, &inv.IssuedAt, &paid); err != nil {
		return nil, err
	}
	inv.PaidAt = nullStr(paid)
	return &inv, nil
}

// attachPending fills each invoice's newest pending attempt (if any).
func (h *Handler) attachPending(ctx context.Context, invoices []*InvoiceDTO) error {
	if len(invoices) == 0 {
		return nil
	}
	ids := make([]string, len(invoices))
	byID := make(map[string]*InvoiceDTO, len(invoices))
	for i, inv := range invoices {
		ids[i] = inv.ID
		byID[inv.ID] = inv
	}
	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "$" + strconv.Itoa(i+1)
		args[i] = id
	}
	q := `SELECT invoice_id, id, amount, qr_payload, expires_at FROM payment_attempts
		WHERE status = 'pending' AND invoice_id IN (` + placeholders + `)
		ORDER BY created_at DESC, id DESC`
	rows, err := h.pool.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var invoiceID string
		var p PendingAttemptDTO
		if err := rows.Scan(&invoiceID, &p.ID, &p.Amount, &p.QRPayload, &p.ExpiresAt); err != nil {
			return err
		}
		if inv, ok := byID[invoiceID]; ok && inv.PendingAttempt == nil {
			attempt := p
			inv.PendingAttempt = &attempt
		}
	}
	return rows.Err()
}

func (h *Handler) listItems(ctx context.Context, invoiceID string) ([]InvoiceItemDTO, error) {
	const q = `SELECT id, description, qty, price FROM invoice_items WHERE invoice_id = $1 ORDER BY id ASC`
	rows, err := h.pool.QueryContext(ctx, q, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvoiceItemDTO{}
	for rows.Next() {
		var it InvoiceItemDTO
		if err := rows.Scan(&it.ID, &it.Description, &it.Qty, &it.Price); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (h *Handler) listPayments(ctx context.Context, invoiceID string) ([]PaymentDTO, error) {
	const q = `SELECT id, amount, method, attempt_id, paid_at FROM payments WHERE invoice_id = $1 ORDER BY paid_at ASC, id ASC`
	rows, err := h.pool.QueryContext(ctx, q, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PaymentDTO{}
	for rows.Next() {
		var p PaymentDTO
		var attempt sql.NullString
		if err := rows.Scan(&p.ID, &p.Amount, &p.Method, &attempt, &p.PaidAt); err != nil {
			return nil, err
		}
		p.AttemptID = nullStr(attempt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (h *Handler) scopedAccountIDs(ctx context.Context, custID string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if custID == "" {
		rows, err = h.pool.QueryContext(ctx, `SELECT id FROM pppoe_accounts ORDER BY id ASC LIMIT 2000`)
	} else {
		rows, err = h.pool.QueryContext(ctx, `SELECT id FROM pppoe_accounts WHERE customer_id = $1 ORDER BY id ASC`, custID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// foldSessions aggregates sessions in-window into day buckets. Grouping in Go
// keeps the SQL portable (no date_trunc vs strftime split).
func (h *Handler) foldSessions(ctx context.Context, accountIDs []string, cutoff string, now time.Time, index map[string]int, points []UsagePointDTO) error {
	placeholders := ""
	args := make([]any, 0, len(accountIDs)+1)
	for i, id := range accountIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "$" + strconv.Itoa(i+1)
		args = append(args, id)
	}
	args = append(args, cutoff)
	q := `SELECT started_at, stopped_at, bytes_in, bytes_out FROM radius_sessions
		WHERE pppoe_account_id IN (` + placeholders + `) AND started_at >= $` + strconv.Itoa(len(args)) +
		` ORDER BY started_at ASC LIMIT 5000`
	rows, err := h.pool.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var started, stopped sql.NullString
		var in, out int64
		if err := rows.Scan(&started, &stopped, &in, &out); err != nil {
			return err
		}
		day, ok := dayOf(started.String)
		if !ok {
			continue
		}
		i, ok := index[day]
		if !ok || i < 0 || i >= len(points) {
			continue
		}
		points[i].BytesIn += in
		points[i].BytesOut += out
		points[i].OnlineSeconds += sessionSeconds(started.String, stopped, now)
	}
	return rows.Err()
}

func (h *Handler) usageTotals(ctx context.Context, accountIDs []string) (UsageTotalsDTO, error) {
	var t UsageTotalsDTO
	if len(accountIDs) == 0 {
		return t, nil
	}
	placeholders := ""
	args := make([]any, len(accountIDs))
	for i, id := range accountIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "$" + strconv.Itoa(i+1)
		args[i] = id
	}
	q := `SELECT COALESCE(SUM(bytes_in),0), COALESCE(SUM(bytes_out),0), COALESCE(SUM(online_seconds),0)
		FROM usage_counters WHERE pppoe_account_id IN (` + placeholders + `)`
	err := h.pool.QueryRowContext(ctx, q, args...).Scan(&t.BytesIn, &t.BytesOut, &t.OnlineSeconds)
	return t, err
}

// --- small helpers ---

func nullStr(v sql.NullString) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// dayOf extracts the UTC calendar day from an RFC3339 TEXT timestamp.
func dayOf(ts string) (string, bool) {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "", false
	}
	return t.UTC().Format("2006-01-02"), true
}

// sessionSeconds is stopped-start (open sessions count up to now).
func sessionSeconds(started string, stopped sql.NullString, now time.Time) int64 {
	start, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return 0
	}
	end := now
	if stopped.Valid && stopped.String != "" {
		e, err := time.Parse(time.RFC3339, stopped.String)
		if err != nil {
			return 0
		}
		end = e
	}
	secs := int64(end.Sub(start).Seconds())
	if secs < 0 {
		return 0
	}
	return secs
}

// writeError uses the billing envelope {error:{code,message}} so the shared
// frontend client parses it (incl. future gobiz_offline/quota_exceeded).
func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}

func writeFieldError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	return writeError(c, fiber.StatusBadRequest, "invalid", "invalid request")
}
