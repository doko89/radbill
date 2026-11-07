-- name: GetOIDCLink :one
SELECT * FROM oidc_links WHERE issuer = $1 AND sub = $2;

-- name: CreateOIDCLink :exec
INSERT INTO oidc_links (user_id, issuer, sub, created_at)
VALUES ($1, $2, $3, $4);
