-- name: ListOrdersByUser :many
SELECT * FROM orders
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: GetOrder :one
SELECT * FROM orders WHERE id = $1 AND user_id = $2;

-- name: CreateOrder :one
INSERT INTO orders (id, user_id, item, qty, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateOrderStatus :one
UPDATE orders SET status = $3
WHERE id = $1 AND user_id = $2
RETURNING *;
