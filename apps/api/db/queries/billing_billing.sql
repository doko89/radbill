-- billing Fase 1: invoices/invoice_items/payments/settings.

-- name: ListBillingInvoices :many
SELECT * FROM invoices ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: ListBillingInvoicesByStatus :many
SELECT * FROM invoices WHERE status = $1 ORDER BY due_date ASC, id DESC LIMIT $2 OFFSET $3;

-- name: CountBillingInvoices :one
SELECT COUNT(*) FROM invoices;

-- name: CountBillingInvoicesByStatus :one
SELECT COUNT(*) FROM invoices WHERE status = $1;

-- name: GetBillingInvoice :one
SELECT * FROM invoices WHERE id = $1;

-- name: GetBillingInvoiceByNumber :one
SELECT * FROM invoices WHERE number = $1;

-- name: ListBillingInvoicesByCustomer :many
SELECT * FROM invoices WHERE customer_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3;

-- name: CreateBillingInvoice :one
INSERT INTO invoices (id, number, pppoe_account_id, customer_id, total, due_date, status, issued_at, paid_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateBillingInvoiceStatus :one
UPDATE invoices SET status = $2, paid_at = $3, updated_at = $4
WHERE id = $1
RETURNING *;

-- name: ListBillingInvoiceItems :many
SELECT * FROM invoice_items WHERE invoice_id = $1 ORDER BY id ASC;

-- name: CreateBillingInvoiceItem :one
INSERT INTO invoice_items (id, invoice_id, description, qty, price)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateBillingPayment :one
INSERT INTO payments (id, invoice_id, amount, method, attempt_id, paid_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListBillingPaymentsByInvoice :many
SELECT * FROM payments WHERE invoice_id = $1 ORDER BY paid_at DESC, id DESC;

-- name: GetBillingSetting :one
SELECT * FROM settings WHERE key = $1;

-- name: ListBillingSettings :many
SELECT * FROM settings ORDER BY key ASC;

-- name: UpsertBillingSetting :one
INSERT INTO settings (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
RETURNING *;
