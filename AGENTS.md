# AGENTS.md — goreact-boilerplate

Go (Fiber 3, sqlc) + React (Vite, TS strict) monorepo. One binary serves API + embedded SPA.

## Commands

- `make setup` deps, `make dev` (API :3000 + web :5173), `make build` → `./bin/api`
- `make test` backend tests, `make vet` (gofmt + go vet + tsc), `make sqlc` after query edits
- `make smoke` probes a running :3000 (CSRF cookie, CSP header, 403 without token)

## Backend (`apps/api`)

- Layout: `internal/<context>/{domain,application,infrastructure,presentation}`; `shared/` and `platform/` only for cross-cutting code.
- `domain` is pure: no fiber/sql/os imports. Interfaces live in `domain`, implemented in `infrastructure`.
- Errors: `apperr.Invalid(field, msg)` → 400, `ErrOrderNotFound` → 404, `ErrNotOpen` → 409, bad credentials → 401. Never leak internal errors (generic 500).
- SQL must run on **both** sqlite and postgres: TEXT ids, BIGINT ints, RFC3339 TEXT timestamps, `$1` placeholders. Schema auto-migrates on boot.
- Security invariants (covered by `server_test.go`/`oidc_test.go`/`uploads_test.go`, keep them green): HttpOnly `session` cookie, CSRF header on unsafe `/api/*` methods, CSP on API + static responses. OIDC/LDAP stay disabled by default; external users carry empty password hashes and can never password-login. Uploads validate webp magic + size server-side; `/uploads/*` serving is allowlisted (no raw path join) on both storage drivers.

## Frontend (`apps/web`)

- Layout: `src/features/<name>/{api.ts, pages/, components/}`, shared code in `src/shared/`. Features never import each other.
- All API I/O goes through `shared/lib/api.ts` (cookies + CSRF + one retry). Validate at boundaries with zod. No `any`, no localStorage tokens.
- TS is strict (`noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`): fix type errors, don't loosen config.

### Design skills (read before doing the work)
- Build/change frontend UI: read the `ui-ux-pro-max` skill first.
- Logo/brand-mark creation: use the `logo-design` skill.
- Visual polish pass: use the `emil-design-eng` skill.

## Before commit

- `make vet && make test` must pass. Keep `.env`, `bin/`, `dist/`, `*.db` out of git (already ignored).
- Match the neighboring file's pattern; don't add deps or layers the task didn't ask for.
