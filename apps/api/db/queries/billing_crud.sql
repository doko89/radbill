-- billing Fase 1: CRUD customers/packages/pppoe/nas + sessions/usage.

-- name: ListBillingCustomers :many
SELECT * FROM customers ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountBillingCustomers :one
SELECT COUNT(*) FROM customers;

-- name: GetBillingCustomer :one
SELECT * FROM customers WHERE id = $1;

-- name: CreateBillingCustomer :one
INSERT INTO customers (id, account_id, name, phone, address, notes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateBillingCustomer :one
UPDATE customers SET account_id = $2, name = $3, phone = $4, address = $5, notes = $6, updated_at = $7
WHERE id = $1
RETURNING *;

-- name: DeleteBillingCustomer :exec
DELETE FROM customers WHERE id = $1;

-- name: SetBillingCustomerAccount :exec
UPDATE customers SET account_id = $2, updated_at = $3 WHERE id = $1;

-- name: ListBillingPackages :many
SELECT * FROM packages ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountBillingPackages :one
SELECT COUNT(*) FROM packages;

-- name: GetBillingPackage :one
SELECT * FROM packages WHERE id = $1;

-- name: CreateBillingPackage :one
INSERT INTO packages (id, name, price, period, rate_limit, address_pool, validity_seconds, online_seconds_limit, quota_bytes, quota_reset, fup_threshold_bytes, fup_rate_limit, on_exhausted, is_public, allow_self_service, upgrade_group, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING *;

-- name: UpdateBillingPackage :one
UPDATE packages SET name = $2, price = $3, period = $4, rate_limit = $5, address_pool = $6, validity_seconds = $7, online_seconds_limit = $8, quota_bytes = $9, quota_reset = $10, fup_threshold_bytes = $11, fup_rate_limit = $12, on_exhausted = $13, is_public = $14, allow_self_service = $15, upgrade_group = $16, updated_at = $17
WHERE id = $1
RETURNING *;

-- name: DeleteBillingPackage :exec
DELETE FROM packages WHERE id = $1;

-- name: ListBillingPPPoE :many
SELECT * FROM pppoe_accounts ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountBillingPPPoE :one
SELECT COUNT(*) FROM pppoe_accounts;

-- name: GetBillingPPPoE :one
SELECT * FROM pppoe_accounts WHERE id = $1;

-- name: GetBillingPPPoEByUsername :one
SELECT * FROM pppoe_accounts WHERE username = $1;

-- name: ListBillingPPPoEByCustomer :many
SELECT * FROM pppoe_accounts WHERE customer_id = $1 ORDER BY created_at DESC, id DESC;

-- name: CreateBillingPPPoE :one
INSERT INTO pppoe_accounts (id, customer_id, package_id, nas_id, username, password, status, expires_at, static_ip, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateBillingPPPoE :one
UPDATE pppoe_accounts SET customer_id = $2, package_id = $3, nas_id = $4, username = $5, password = $6, status = $7, expires_at = $8, static_ip = $9, updated_at = $10
WHERE id = $1
RETURNING *;

-- name: DeleteBillingPPPoE :exec
DELETE FROM pppoe_accounts WHERE id = $1;

-- name: ListBillingNAS :many
SELECT * FROM nas ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountBillingNAS :one
SELECT COUNT(*) FROM nas;

-- name: GetBillingNAS :one
SELECT * FROM nas WHERE id = $1;

-- name: GetBillingNASByIP :one
SELECT * FROM nas WHERE ip = $1;

-- name: CreateBillingNAS :one
INSERT INTO nas (id, name, ip, secret, coa_port, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateBillingNAS :one
UPDATE nas SET name = $2, ip = $3, secret = $4, coa_port = $5, updated_at = $6
WHERE id = $1
RETURNING *;

-- name: DeleteBillingNAS :exec
DELETE FROM nas WHERE id = $1;

-- name: ListBillingOnlineSessions :many
SELECT * FROM radius_sessions WHERE stopped_at IS NULL ORDER BY started_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountBillingOnlineSessions :one
SELECT COUNT(*) FROM radius_sessions WHERE stopped_at IS NULL;

-- name: ListBillingSessionHistory :many
SELECT * FROM radius_sessions ORDER BY started_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: CountBillingSessions :one
SELECT COUNT(*) FROM radius_sessions;

-- name: ListBillingSessionsByPPPoE :many
SELECT * FROM radius_sessions WHERE pppoe_account_id = $1 ORDER BY started_at DESC, id DESC LIMIT $2 OFFSET $3;

-- name: GetBillingUsageCounter :one
SELECT * FROM usage_counters WHERE pppoe_account_id = $1 AND period_start = $2;

-- name: ListBillingUsageByAccount :many
SELECT * FROM usage_counters WHERE pppoe_account_id = $1 ORDER BY period_start DESC;

-- name: UpsertBillingUsageCounter :one
INSERT INTO usage_counters (pppoe_account_id, period_start, bytes_in, bytes_out, online_seconds)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(pppoe_account_id, period_start) DO UPDATE SET
  bytes_in = EXCLUDED.bytes_in,
  bytes_out = EXCLUDED.bytes_out,
  online_seconds = EXCLUDED.online_seconds
RETURNING *;

-- name: DeleteBillingUsageByAccount :exec
DELETE FROM usage_counters WHERE pppoe_account_id = $1;
