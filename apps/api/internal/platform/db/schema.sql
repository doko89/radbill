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

-- billing Fase 1 (radbill, ADITIF): tabel di bawah ini dipakai billing + radiusd.
-- Jangan ubah nama tabel/kolom (kontrak antar-agen).
CREATE TABLE IF NOT EXISTS accounts (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TEXT NOT NULL,
    revoked_at  TEXT,
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS password_resets (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS customers (
    id          TEXT PRIMARY KEY,
    account_id  TEXT UNIQUE REFERENCES accounts (id) ON DELETE SET NULL,
    name        TEXT NOT NULL,
    phone       TEXT,
    address     TEXT,
    notes       TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS packages (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    price                BIGINT NOT NULL,
    period               TEXT NOT NULL,
    rate_limit           TEXT,
    address_pool         TEXT,
    validity_seconds     BIGINT,
    online_seconds_limit BIGINT,
    quota_bytes          BIGINT,
    quota_reset          TEXT NOT NULL DEFAULT 'none' CHECK (quota_reset IN ('none', 'daily', 'monthly', 'billing_cycle')),
    fup_threshold_bytes  BIGINT,
    fup_rate_limit       TEXT,
    on_exhausted         TEXT NOT NULL DEFAULT 'reject' CHECK (on_exhausted IN ('reject', 'isolate', 'throttle')),
    is_public            INTEGER NOT NULL DEFAULT 1,
    allow_self_service   INTEGER NOT NULL DEFAULT 1,
    upgrade_group        TEXT,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS nas (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    ip         TEXT NOT NULL UNIQUE,
    secret     TEXT NOT NULL,
    coa_port   INTEGER NOT NULL DEFAULT 3799,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pppoe_accounts (
    id          TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers (id),
    package_id  TEXT NOT NULL REFERENCES packages (id),
    nas_id      TEXT REFERENCES nas (id) ON DELETE SET NULL,
    username    TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'isolated', 'disabled')),
    expires_at  TEXT,
    static_ip   TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pppoe_accounts_username ON pppoe_accounts (username);

CREATE TABLE IF NOT EXISTS radius_sessions (
    id               TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL,
    pppoe_account_id TEXT NOT NULL REFERENCES pppoe_accounts (id),
    nas_id           TEXT NOT NULL REFERENCES nas (id),
    framed_ip        TEXT,
    started_at       TEXT NOT NULL,
    stopped_at       TEXT,
    bytes_in         BIGINT NOT NULL DEFAULT 0,
    bytes_out        BIGINT NOT NULL DEFAULT 0,
    terminate_cause  TEXT,
    updated_at       TEXT NOT NULL,
    UNIQUE (session_id, nas_id)
);

CREATE TABLE IF NOT EXISTS usage_counters (
    pppoe_account_id TEXT NOT NULL REFERENCES pppoe_accounts (id),
    period_start     TEXT NOT NULL,
    bytes_in         BIGINT NOT NULL DEFAULT 0,
    bytes_out        BIGINT NOT NULL DEFAULT 0,
    online_seconds   BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (pppoe_account_id, period_start)
);

CREATE TABLE IF NOT EXISTS invoices (
    id               TEXT PRIMARY KEY,
    number           TEXT NOT NULL UNIQUE,
    pppoe_account_id TEXT NOT NULL REFERENCES pppoe_accounts (id),
    customer_id      TEXT NOT NULL REFERENCES customers (id),
    total            BIGINT NOT NULL,
    due_date         TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'unpaid' CHECK (status IN ('unpaid', 'paid', 'void')),
    issued_at        TEXT NOT NULL,
    paid_at          TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_invoices_status_due ON invoices (status, due_date);

CREATE TABLE IF NOT EXISTS invoice_items (
    id          TEXT PRIMARY KEY,
    invoice_id  TEXT NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    description TEXT NOT NULL,
    qty         BIGINT NOT NULL DEFAULT 1,
    price       BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS payments (
    id         TEXT PRIMARY KEY,
    invoice_id TEXT NOT NULL REFERENCES invoices (id),
    amount     BIGINT NOT NULL,
    method     TEXT NOT NULL CHECK (method IN ('kriskris', 'manual')),
    attempt_id TEXT,
    paid_at    TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO settings (key, value, updated_at) VALUES ('acct_interim_interval', '300', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('isolir_rate_limit', '128k/128k', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('isolir_address_list', 'isolir', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;

-- billing Fase 2 (agen C1, ADITIF): kontrak DDL G2. Nama tabel/kolom di bawah
-- adalah kontrak antar-agen (C2 kriskris, C3 portal); jangan ubah.
-- payments.note ditambah lewat ensurePaymentsNoteColumn di db.go (pola avatar_url)
-- agar migrate tetap idempoten di sqlite maupun postgres.
CREATE TABLE IF NOT EXISTS payment_attempts (
    id                 TEXT PRIMARY KEY,
    invoice_id         TEXT NOT NULL REFERENCES invoices (id),
    kriskris_invoice_id TEXT NOT NULL UNIQUE,
    base_amount        BIGINT NOT NULL,
    unique_code        BIGINT NOT NULL,
    amount             BIGINT NOT NULL,
    qr_payload         TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'expired', 'cancelled', 'failed')),
    expires_at         TEXT NOT NULL,
    paid_at            TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_payment_attempts_invoice_id ON payment_attempts (invoice_id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    event       TEXT NOT NULL,
    payload     TEXT NOT NULL,
    received_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_log (
    id               TEXT PRIMARY KEY,
    kind             TEXT NOT NULL,
    target           TEXT NOT NULL,
    pppoe_account_id TEXT,
    invoice_id       TEXT,
    status           TEXT NOT NULL,
    error            TEXT,
    created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notification_log_created_at ON notification_log (created_at);

-- Seed pengaturan G2 (plain, ON CONFLICT DO NOTHING agar aman di-boot ulang).
INSERT INTO settings (key, value, updated_at) VALUES ('billing_cycle_mode', 'activation', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('billing_cycle_day', '1', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('grace_days', '3', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('company_name', 'Radbill', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('notif_provider', 'log', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('notif_from', '', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('smtp_host', '', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('smtp_port', '', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('smtp_user', '', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('smtp_pass', '', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('kriskris_base_url', 'https://api.kriskris.id', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('tpl_invoice_issued', 'Halo {{.Nama}}, tagihan {{.Nomor}} sebesar Rp{{.Total}} telah terbit. Jatuh tempo {{.JatuhTempo}}. - {{.Perusahaan}}', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('tpl_reminder', 'Halo {{.Nama}}, tagihan {{.Nomor}} sebesar Rp{{.Total}} sudah jatuh tempo {{.JatuhTempo}}. Segera bayar agar layanan tidak diisolir. - {{.Perusahaan}}', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('tpl_isolated', 'Halo {{.Nama}}, layanan {{.Username}} diisolir karena tagihan {{.Nomor}} belum lunas. Bayar untuk mengaktifkan kembali. - {{.Perusahaan}}', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
INSERT INTO settings (key, value, updated_at) VALUES ('tpl_paid', 'Halo {{.Nama}}, pembayaran tagihan {{.Nomor}} sebesar Rp{{.Total}} diterima. Terima kasih. - {{.Perusahaan}}', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;

-- billing Fase 3 (agen G3/D2, ADITIF): top-up kuota + ganti paket mandiri.
-- Kontrak antar-agen: D1 (radius usage mirror) membaca quota_topups
-- (pppoe_account_id, period_start, bytes); jangan ubah nama tabel/kolom.
CREATE TABLE IF NOT EXISTS quota_topups (
    id               TEXT PRIMARY KEY,
    pppoe_account_id TEXT NOT NULL REFERENCES pppoe_accounts (id),
    bytes            BIGINT NOT NULL,
    period_start     TEXT NOT NULL,
    invoice_id       TEXT REFERENCES invoices (id),
    created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_quota_topups_pppoe_account ON quota_topups (pppoe_account_id);

CREATE TABLE IF NOT EXISTS plan_changes (
    id               TEXT PRIMARY KEY,
    pppoe_account_id TEXT NOT NULL REFERENCES pppoe_accounts (id),
    from_package     TEXT NOT NULL,
    to_package       TEXT NOT NULL,
    effective_at     TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'cancelled')),
    created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_plan_changes_status_effective ON plan_changes (status, effective_at);

INSERT INTO settings (key, value, updated_at) VALUES ('topup_price_per_gb', '10000', '2026-01-01T00:00:00Z') ON CONFLICT(key) DO NOTHING;
