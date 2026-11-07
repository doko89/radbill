-- Shared schema for postgres and sqlite.
-- Only uses the compatible subset: TEXT primary keys, BIGINT integers,
-- RFC3339 timestamps stored as TEXT, $1 placeholders, RETURNING.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    avatar_url    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS orders (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id),
    item       TEXT NOT NULL,
    qty        BIGINT NOT NULL,
    status     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders (user_id);

CREATE TABLE IF NOT EXISTS oidc_links (
    user_id    TEXT NOT NULL REFERENCES users (id),
    issuer     TEXT NOT NULL,
    sub        TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (issuer, sub)
);

CREATE INDEX IF NOT EXISTS idx_oidc_links_user_id ON oidc_links (user_id);
