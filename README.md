# Boilerplate

Monorepo: Go API (pragmatic DDD) + React SPA (feature-based). One binary serves both.

```text
apps/api/   # Fiber 3, sqlc, sqlite/postgres, session cookie + CSRF + CSP
apps/web/   # React + Vite, feature-based, same-origin fetch
packages/api-contract/openapi.yaml
```

## Quickstart

```bash
make setup     # go mod download + npm install
make dev       # API :3000 + web :5173 (proxy /api -> :3000)
```

Open http://localhost:5173, login `admin` / `admin123` (seeded from `.env`).

Production single binary:

```bash
make build     # web bundle -> embedded into bin/api
./bin/api      # serves API + SPA on :3000
```

## Backend layout (one folder = one bounded context)

```text
apps/api/
  cmd/api/main.go
  internal/
    shared/        # config (.env), apperr, httpx, id
    platform/      # db (open/migrate/seed), session (HMAC cookie), httpserver (wiring)
    auth/          # domain/ application/ infrastructure/ presentation/
    order/         # domain/ application/ infrastructure/ presentation/
  db/queries/      # sqlc queries (one set runs on both drivers)
  web/dist/        # embed target, filled by make build-web
```

Rules: `presentation -> application -> domain <- infrastructure`, interfaces in
`domain`, sqlc + bcrypt in `infrastructure`, no framework imports in `domain`.

## Frontend layout (one folder = one feature)

```text
apps/web/src/
  app/             # router
  shared/lib/      # api client (cookies + CSRF header + retry), cookie reader, webp convert
  shared/ui/       # Layout, RequireAuth
  features/auth/   # api.ts (zod), AuthProvider, pages/LoginPage
  features/order/  # api.ts (zod), pages/OrdersPage
  features/profile/ # api.ts (upload), pages/ProfilePage
```

Features never import each other, only `shared/`. No token in localStorage:
auth state lives in the HttpOnly cookie, UI state in `AuthProvider`.

## Environment (.env)

| Key               | Default          | Notes                          |
| ----------------- | ---------------- | ------------------------------ |
| APP_ENV           | dev              | dev \| prod \| test             |
| APP_PORT          | 3000             |                                |
| DB_DRIVER         | sqlite           | sqlite \| postgres              |
| SQLITE_PATH       | ./data/app.db    | `:memory:` allowed              |
| DATABASE_URL      | —                | required for postgres           |
| SESSION_SECRET    | —                | required in prod                |
| SESSION_TTL_HOURS | 12               |                                |
| ADMIN_USER        | admin            | seed when users table empty     |
| ADMIN_PASS        | admin123         | required in prod                |
| OIDC_ENABLED      | false            | + OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/REDIRECT_URL/LABEL |
| LDAP_ENABLED      | false            | + LDAP_URL/BASE_DN/BIND_DN… (see `.env.example`) |
| UPLOAD_DIR        | ./data/uploads   | avatar storage served at /uploads/* |
| STORAGE_DRIVER    | local            | local \| s3 (+ S3_ENDPOINT/BUCKET/keys, see `.env.example`) |

`.env` is resolved by walking upward from cwd, so both `make dev-*` and
`./bin/api` find the root file. Real env vars always override `.env`.

## Security

- **HttpOnly session**: HMAC-signed token in `session` cookie
  (`HttpOnly`, `SameSite=Lax`, `Secure` in prod). No JWT library, no storage.
- **CSRF**: Fiber csrf middleware on `/api/*`. SPA reads `csrf_token` cookie
  and sends `X-CSRF-Token` on POST/PATCH; client retries once on rotation.
- **CSP**: helmet middleware, `default-src 'self'`, `frame-ancestors 'none'`,
  re-applied on static responses. `img-src` additionally allows `https:` for
  remote provider avatars. Same-origin design means no CORS at all.
- Passwords: bcrypt. Login errors are uniform (no user enumeration).

## Login methods

Password login is always on. Two more methods ship disabled and activate
per project via env (see `.env.example`):

- **OIDC** (`OIDC_ENABLED=true`): authorization-code flow for Google or any
  OIDC provider. `GET /api/auth/oidc/login` → provider → callback verifies
  state/nonce/ID-token, finds-or-creates the user (link in `oidc_links`
  table), sets the same HttpOnly session cookie, lands on `/`. Discovery
  runs at boot and fails fast on misconfiguration.
- **LDAP** (`LDAP_ENABLED=true`): `POST /api/auth/ldap/login` binds against
  your directory (search mode via `LDAP_BIND_DN`, or direct bind via
  `LDAP_USER_DN_TEMPLATE`). LDAP users are stored lowercased with an empty
  password hash, so password login always rejects them; a username owned by
  a local password account returns 409 instead of linking. The directory is
  contacted lazily on login, never at boot. Use `ldaps://` in production.

`GET /api/auth/methods` reports enabled methods; the login page renders LDAP
tabs and the OIDC button from it — no frontend flags to sync.

## Avatars & uploads

- `POST /api/uploads/avatar` (auth + CSRF, multipart `file`) stores the
  avatar and links it to the user. Files land in `UPLOAD_DIR/avatar/` and
  serve at `/uploads/avatar/<uuid>.webp` with immutable caching.
- The SPA converts images to WebP 80% via canvas (`shared/lib/image.ts`)
  before upload; the server re-validates magic bytes + 512 KB cap and
  rejects anything else. Uploading again replaces the old file.
- OIDC logins store the provider `picture` claim as `avatar_url`, refreshed
  on each login — unless the user uploaded a local avatar, which always wins.
- `GET /api/auth/me` (and all login responses) include `avatarUrl`.

## Object storage

`STORAGE_DRIVER=local` (default) keeps uploads under `UPLOAD_DIR`.
`STORAGE_DRIVER=s3` uses any S3-compatible backend (AWS S3, MinIO, R2):

```bash
STORAGE_DRIVER=s3
S3_ENDPOINT=localhost:9000   # host[:port], no scheme
S3_REGION=us-east-1
S3_BUCKET=boilerplate        # auto-created when missing
S3_ACCESS_KEY=...
S3_SECRET_KEY=...
S3_USE_SSL=false             # local MinIO over plain HTTP
```

URLs stay `/uploads/...` on both drivers: the API proxies S3 objects, so
buckets can remain private and the SPA needs no storage awareness.

## Database

`DB_DRIVER=sqlite` (dev, zero deps) or `postgres` (prod). One sqlc query set
runs on both: TEXT ids, BIGINT ints, RFC3339 TEXT timestamps, `$1` params.
Schema auto-migrates on boot (`CREATE TABLE IF NOT EXISTS`).

```bash
make sqlc   # regenerate after editing db/queries/*.sql
```

## Commands

| Command     | Effect                                  |
| ----------- | --------------------------------------- |
| make setup  | install Go + npm deps                   |
| make dev    | API + web together                      |
| make build  | full build -> `./bin/api`               |
| make build-linux-arm64 | web bundle + cross build -> `./bin/api-linux-arm64` |
| make run    | build + run single binary               |
| make test   | backend tests                           |
| make vet    | gofmt + go vet + frontend typecheck     |
| make sqlc   | regenerate sqlc                         |
| make clean  | drop bin/, data/, embedded bundle       |
| make smoke  | probe running :3000 (CSRF + CSP + 403)  |
