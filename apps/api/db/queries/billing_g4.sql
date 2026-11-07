-- billing Fase 4 (agen E1/G4, ADITIF): audit_logs + agregat dashboard/metrik.
-- File G1/G2/G3 tidak diubah. Semua query memakai subset sqlite+postgres
-- (placeholder $1, tanpa cast ::, tanpa fungsi khusus-driver).
--
-- Konvensi filter List/CountAuditLogs (tanpa guard IS NULL agar ramah sqlc):
-- $1 actor: '' cocok semua (username LIKE '%%'); selain itu cocok actor_id
--   persis ATAU actor_username mengandung substring.
-- $2 action: prefix LIKE ($2||'%'); '' cocok semua, 'customer' cocok
--   'customer' + 'customer.*', 'customer.created' cocok persis (+ turunan).
-- $3 from: created_at >= $3; '' cocok semua (created_at tak pernah '').
-- $4 to: created_at <= $4; pemanggil mengirim sentinel jauh saat kosong.

-- name: CreateAuditLog :one
INSERT INTO audit_logs (id, actor_id, actor_username, action, target_type, target_id, detail, ip, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE (actor_id = $1 OR actor_username LIKE '%' || $1 || '%')
  AND action LIKE ($2 || '%')
  AND created_at >= $3
  AND created_at <= $4
ORDER BY created_at DESC, id DESC LIMIT $5 OFFSET $6;

-- name: CountAuditLogs :one
SELECT COUNT(*) FROM audit_logs
WHERE (actor_id = $1 OR actor_username LIKE '%' || $1 || '%')
  AND action LIKE ($2 || '%')
  AND created_at >= $3
  AND created_at <= $4;

-- name: ListG4PaymentsSince :many
SELECT amount FROM payments WHERE paid_at >= $1 ORDER BY paid_at ASC, id ASC;

-- name: ListG4UnpaidTotals :many
SELECT total, due_date FROM invoices WHERE status = 'unpaid';

-- name: ListG4PPPoECounts :many
SELECT status, COUNT(*) AS n FROM pppoe_accounts GROUP BY status;

-- name: ListG4RecentPayments :many
SELECT p.id, i.number, p.amount, p.paid_at FROM payments p
JOIN invoices i ON i.id = p.invoice_id
ORDER BY p.paid_at DESC, p.id DESC LIMIT $1;

-- name: CountG4PendingAttempts :one
SELECT COUNT(*) FROM payment_attempts WHERE status = 'pending';

-- name: CountG4FailedAttemptsSince :one
SELECT COUNT(*) FROM payment_attempts WHERE status = 'failed' AND updated_at >= $1;
