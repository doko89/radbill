-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (id, username, password_hash, created_at, avatar_url)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateUserAvatar :exec
UPDATE users SET avatar_url = $1 WHERE id = $2;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;
