-- billing Fase 3 (agen G3/D2, ADITIF): quota_topups + plan_changes,
-- paket publik portal, dan query laporan. File G1/G2 tidak diubah.

-- name: CreateQuotaTopup :one
INSERT INTO quota_topups (id, pppoe_account_id, bytes, period_start, invoice_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetQuotaTopupByInvoice :one
SELECT * FROM quota_topups WHERE invoice_id = $1;

-- name: SumQuotaTopupsByAccountPeriod :one
SELECT COALESCE(SUM(bytes), 0) FROM quota_topups WHERE pppoe_account_id = $1 AND period_start = $2;

-- name: CreatePlanChange :one
INSERT INTO plan_changes (id, pppoe_account_id, from_package, to_package, effective_at, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPlanChange :one
SELECT * FROM plan_changes WHERE id = $1;

-- name: ListPlanChangesByAccount :many
SELECT * FROM plan_changes WHERE pppoe_account_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3;

-- name: ListPlanChanges :many
SELECT * FROM plan_changes ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountPlanChanges :one
SELECT COUNT(*) FROM plan_changes;

-- name: ListPlanChangesByCustomer :many
SELECT pc.* FROM plan_changes pc
JOIN pppoe_accounts a ON a.id = pc.pppoe_account_id
WHERE a.customer_id = $1 ORDER BY pc.created_at DESC, pc.id DESC LIMIT $2 OFFSET $3;

-- name: CountPlanChangesByCustomer :one
SELECT COUNT(*) FROM plan_changes pc
JOIN pppoe_accounts a ON a.id = pc.pppoe_account_id
WHERE a.customer_id = $1;

-- name: CountActivePlanChangesByAccountSince :one
SELECT COUNT(*) FROM plan_changes
WHERE pppoe_account_id = $1 AND status IN ('pending', 'applied') AND created_at >= $2;

-- name: ListDuePlanChanges :many
SELECT * FROM plan_changes WHERE status = 'pending' AND effective_at <= $1 ORDER BY effective_at ASC, id ASC LIMIT $2;

-- name: UpdatePlanChangeStatus :one
UPDATE plan_changes SET status = $2 WHERE id = $1
RETURNING *;

-- name: ListPublicPackages :many
SELECT * FROM packages WHERE is_public = 1 ORDER BY price ASC, name ASC, id ASC;

-- name: ListG3PaymentsInRange :many
SELECT id, invoice_id, amount, method, paid_at FROM payments
WHERE paid_at >= $1 AND paid_at < $2 ORDER BY paid_at ASC, id ASC LIMIT $3;

-- name: ListG3UnpaidInvoices :many
SELECT i.id, i.number, i.customer_id, i.total, i.due_date, c.name AS customer_name
FROM invoices i JOIN customers c ON c.id = i.customer_id
WHERE i.status = 'unpaid' ORDER BY i.due_date ASC, i.id ASC LIMIT $1;

-- name: ListG3SessionsInRange :many
SELECT pppoe_account_id, started_at, stopped_at, bytes_in, bytes_out FROM radius_sessions
WHERE started_at >= $1 AND started_at < $2 ORDER BY started_at ASC LIMIT $3;

-- name: ListG3PPPoEStatus :many
SELECT id, status, created_at, updated_at FROM pppoe_accounts ORDER BY id ASC LIMIT $1 OFFSET $2;
