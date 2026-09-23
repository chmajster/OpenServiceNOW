# OpenServiceNOW

OpenServiceNOW is an enterprise ITSM / IT Operations platform implemented independently from ServiceNow code, branding, and UI.

## Current state

Phase 1 — Foundation is in progress.

Implemented foundation capabilities:

- Go API runtime with graceful shutdown,
- validated environment configuration,
- structured JSON application/access logs and request IDs,
- MySQL 8.x through `database/sql`,
- Redis,
- explicit versioned migrations,
- liveness and schema-aware readiness,
- transaction manager,
- tenant/user/group/RBAC/session/audit schema,
- local password authentication with Argon2id,
- opaque database-backed sessions,
- CSRF double-submit validation,
- login rate limiting in Redis,
- backend permission evaluation,
- tenant/admin bootstrap command,
- cursor-paginated/searchable tenant user administration, including linking an existing active global identity to another tenant without changing its credentials/profile,
- tenant administration with optimistic locking,
- cursor-paginated/searchable RBAC administration for groups, roles, permissions and assignments,
- immutable system-role permissions, optimistic-lock role edits, dynamic superadmin permission inheritance, self-removal protection and a transactional guarantee that each tenant retains at least one active effective platform administrator,
- tenant-scoped paginated audit API,
- trusted-proxy-aware client IP resolution,
- optional MySQL/Redis TLS and forced UTC MySQL sessions,
- bounded Argon2 password verification concurrency,
- panic-safe transactions and JSON panic recovery,
- bounded session-cleanup scheduler,
- OpenAPI 3.1 contract,
- React + TypeScript administration frontend,
- Docker Compose development services,
- unit/integration/race/build/migration/authentication/RBAC/frontend CI definitions.

The durable background-job worker, external identity providers and ITSM business modules are not implemented yet. The Phase 1 branch is not considered verified until CI actually receives runners and executes its steps.

## Stack

Backend:

- Go 1.27,
- `net/http` + chi,
- `database/sql` + `go-sql-driver/mysql`,
- MySQL 8.x,
- Redis.

Frontend:

- React,
- TypeScript,
- React Router,
- Vite,
- same-origin cookie session + CSRF model.

## Local development

Start MySQL and Redis:

```bash
make dev
```

Create local configuration:

```bash
cp .env.example .env
```

Set a strong value for `BOOTSTRAP_ADMIN_PASSWORD`, then load the environment:

```bash
set -a
source .env
set +a
```

Apply migrations and create the first tenant/admin:

```bash
make migrate-up
make bootstrap
```

The bootstrap is idempotent for tenant membership, role and permission assignment. Re-running it does not reset the password of an existing user.

Start the backend:

```bash
go run ./cmd/api
```

Start the frontend in a second shell:

```bash
cd web
npm install
npm run dev
```

The Vite development server listens on `127.0.0.1:5173` and proxies `/api` and `/health` to `127.0.0.1:8080`.

### Initial login

Default example bootstrap values use tenant `default`. Login requires:

```json
{
  "tenant": "default",
  "email": "admin@example.test",
  "password": "your-password"
}
```

The server sets:

- `opensnow_session` — HttpOnly session cookie,
- `opensnow_csrf` — readable CSRF cookie.

Unsafe authenticated requests require the same CSRF value in the `X-CSRF-Token` header. Cookie names are protocol constants shared by backend, frontend and OpenAPI.

### Reverse proxy client IP

The backend ignores forwarded IP headers unless the direct peer belongs to a CIDR configured in:

```text
TRUSTED_PROXY_CIDRS
```

Example for an internal reverse-proxy network:

```text
TRUSTED_PROXY_CIDRS=10.20.0.0/16,fd00:20::/64
```

When the direct peer is not trusted, rate limiting and audit use the canonical direct peer IP, preventing a client from spoofing `X-Forwarded-For`. Trust-all CIDRs are rejected.

### Datastore TLS

Development defaults to loopback plaintext connections. Production deployments can enable verified TLS with:

```text
MYSQL_TLS_MODE=true
REDIS_TLS_ENABLED=true
REDIS_TLS_SERVER_NAME=redis.internal.example
```

Compatibility modes such as MySQL `skip-verify` or Redis `REDIS_TLS_INSECURE_SKIP_VERIFY=true` are explicit operator choices. MySQL sessions always force UTC for consistent `DATETIME` handling.

## Endpoints

Health:

```text
GET /health/live
GET /health/ready
```

Authentication:

```text
POST /api/v1/auth/login
GET  /api/v1/auth/me
POST /api/v1/auth/logout
```

Administration currently covers:

- current tenant,
- local tenant users,
- RBAC permissions/roles/groups/assignments,
- tenant audit history.

Tenant scope is derived from the authenticated session, never from a client-supplied tenant identifier.

The OpenAPI document is stored at:

```text
api/openapi/openapi.json
```

## Development commands

```bash
make build
make test
make lint
make migrate-up
make migrate-down
make migrate-down-all
make migrate-version
make bootstrap
make scheduler-once
make frontend
make frontend-test
make e2e
```

`make migrate-down` rolls back one migration. `make migrate-down-all` rolls the schema back to no applied version. Readiness requires the exact current migration version and rejects dirty or stale schemas.

### Browser E2E

`make e2e` runs the Playwright suite against an already running local API/frontend. Before using it locally:

1. start MySQL/Redis and apply migrations,
2. bootstrap the E2E tenant/admin,
3. start the API,
4. start Vite on `127.0.0.1:5173`,
5. export `E2E_TENANT`, `E2E_ADMIN_EMAIL`, `E2E_ADMIN_PASSWORD`, `E2E_OPERATOR_EMAIL`, and `E2E_OPERATOR_PASSWORD`,
6. install Chromium once with `cd web && npx playwright install chromium`,
7. run `make e2e`.

CI performs those infrastructure/bootstrap/startup steps automatically in its dedicated `e2e` job.

## Verification status

The workflow YAML parser defect has been fixed and GitHub now creates the `test`, `frontend`, and `e2e` jobs. However, those jobs still terminate before their first step with no job logs. A temporary one-step diagnostic workflow showed the same pre-step failure and was removed afterward. Therefore this branch does **not** claim that build, tests, migrations, frontend compilation, or browser E2E pass.

Two dependency lock artifacts are intentionally still missing because dependency resolution has not successfully executed:

- `go.sum`,
- `web/package-lock.json`.

Both must be generated and committed before the Phase 1 PR is considered merge-ready.

The browser E2E suite uses Playwright/Chromium and validates the critical full-stack path: administrator login → local user creation → least-privilege role creation → role assignment → logout → login as the new user → permission-aware tenant-only UI.

## Architecture and project state

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [ROADMAP.md](ROADMAP.md)
- [TODO.md](TODO.md)
- [STATUS.md](STATUS.md)
- [docs/adr](docs/adr)

Repository working rules are defined in [AGENTS.md](AGENTS.md).
