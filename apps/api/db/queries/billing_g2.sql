-- billing Fase 2 (agen C1, ADITIF): payment_attempts + webhook_deliveries
-- (kontrak C2 kriskris), notification_log (notify), dan query scheduler.
-- File G1 (billing_auth/crud/billing.sql) tidak diubah.

-- name: CreatePaymentAttempt :one
INSERT INTO payment_attempts (id, invoice_id, kriskris_invoice_id, base_amount, unique_code, amount, qr_payload, status, expires_at, paid_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetPaymentAttempt :one
SELECT * FROM payment_attempts WHERE id = $1;

-- name: GetPaymentAttemptByKriskrisID :one
SELECT * FROM payment_attempts WHERE kriskris_invoice_id = $1;

-- name: ListPaymentAttemptsByInvoice :many
SELECT * FROM payment_attempts WHERE invoice_id = $1 ORDER BY created_at DESC, id DESC;

-- name: ListPendingPaymentAttempts :many
SELECT * FROM payment_attempts WHERE status = 'pending' ORDER BY expires_at ASC LIMIT $1;

-- name: UpdatePaymentAttemptStatus :one
UPDATE payment_attempts SET status = $2, paid_at = $3, updated_at = $4
WHERE id = $1
RETURNING *;

-- name: CancelPendingAttemptsForInvoice :exec
UPDATE payment_attempts SET status = 'cancelled', updated_at = $2
WHERE invoice_id = $1 AND status = 'pending';

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (delivery_id, event, payload, received_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE delivery_id = $1;

-- name: CreateNotificationLog :one
INSERT INTO notification_log (id, kind, target, pppoe_account_id, invoice_id, status, error, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListNotificationLog :many
SELECT * FROM notification_log ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountNotificationLogSince :one
SELECT COUNT(*) FROM notification_log WHERE kind = $1 AND invoice_id = $2 AND created_at >= $3;

-- name: ListBillablePPPoE :many
SELECT * FROM pppoe_accounts WHERE status IN ('active', 'isolated') ORDER BY id ASC LIMIT $1 OFFSET $2;

-- name: GetUnpaidInvoiceByPPPoE :one
SELECT * FROM invoices WHERE pppoe_account_id = $1 AND status = 'unpaid' ORDER BY issued_at ASC, id ASC LIMIT 1;

-- name: GetLatestInvoiceByPPPoE :one
SELECT * FROM invoices WHERE pppoe_account_id = $1 ORDER BY issued_at DESC, id DESC LIMIT 1;

-- name: ListOverdueUnpaidInvoices :many
SELECT * FROM invoices WHERE status = 'unpaid' AND due_date < $1 ORDER BY due_date ASC, id ASC LIMIT $2 OFFSET $3;
