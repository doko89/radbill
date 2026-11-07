-- billing Fase 1: auth panel (accounts, refresh_tokens, password_resets).

-- name: GetBillingAccountByUsername :one
SELECT * FROM accounts WHERE username = $1;

-- name: GetBillingAccountByID :one
SELECT * FROM accounts WHERE id = $1;

-- name: CreateBillingAccount :one
INSERT INTO accounts (id, username, password_hash, role, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateBillingAccountPassword :exec
UPDATE accounts SET password_hash = $2, updated_at = $3 WHERE id = $1;

-- name: UpdateBillingAccountStatus :exec
UPDATE accounts SET status = $2, updated_at = $3 WHERE id = $1;

-- name: CountBillingAccounts :one
SELECT COUNT(*) FROM accounts;

-- name: GetBillingCustomerByAccountID :one
SELECT * FROM customers WHERE account_id = $1;

-- name: CreateBillingRefreshToken :one
INSERT INTO refresh_tokens (id, account_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetBillingRefreshToken :one
SELECT * FROM refresh_tokens WHERE token_hash = $1;

-- name: RevokeBillingRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = $2 WHERE token_hash = $1;

-- name: RevokeBillingRefreshTokensForAccount :exec
UPDATE refresh_tokens SET revoked_at = $2 WHERE account_id = $1 AND revoked_at IS NULL;

-- name: CreateBillingPasswordReset :one
INSERT INTO password_resets (id, account_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetBillingPasswordReset :one
SELECT * FROM password_resets WHERE token_hash = $1;

-- name: MarkBillingPasswordResetUsed :exec
UPDATE password_resets SET used_at = $2 WHERE id = $1;
