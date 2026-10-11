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

Durable MySQL-backed jobs, worker/scheduler execution and shared work-record infrastructure are implemented. The root Compose stack runs the complete current application through DevBox. Further business capabilities and external identity providers continue according to the roadmap.

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

## One-command local installer

OpenServiceNOW includes an executable `install.sh` for the repository's current local runtime. It does not change or depend on DevBox orchestration rules.

Requirements:

- Docker Engine/Desktop with Docker Compose v2,
- Go 1.27 or newer,
- Node.js 22.12 or newer with npm,
- curl and standard Unix shell tools.

Install, migrate, bootstrap, build and start the current stack:

```bash
./install.sh
```

The installer creates `.env` from `.env.example` when needed, generates `BOOTSTRAP_ADMIN_PASSWORD` when it is empty, starts MySQL and Redis through the repository Compose file, installs Go/npm dependencies, applies migrations, bootstraps the initial administrator, builds the backend/frontend and starts the API plus the Vite frontend.

Operational modes:

```bash
./install.sh --status
./install.sh --stop
./install.sh --start
./install.sh --restart
./install.sh --no-start
./install.sh --uninstall
./install.sh --uninstall --purge-data
```

Ordinary uninstall preserves `.env` and the MySQL Docker volume. `--purge-data` explicitly removes both. Runtime PID/log files are stored under `.opensnow/`. The current frontend proxy targets the API on port 8080, so the installer rejects another `HTTP_ADDR` port instead of starting a partially working deployment.

## DevBox deployment

The root `compose.yaml` packages the complete application: React assets served by a non-root Nginx reverse proxy, Go API, worker, scheduler and a private Redis cache. It uses a shared DevBox MySQL server with an application-specific database/account. The existing `docker-compose.yml` remains the infrastructure-only development stack selected explicitly by `make dev` and `install.sh`.

In DevBox, add this local source and choose Docker Compose. Select `frontend` as the HTTP service, container port `8080`, a free host port and readiness path `/health/ready`. Leave the start command empty. Create/bind a MySQL database with a separate application account and schema migration privileges. Set `BOOTSTRAP_ADMIN_PASSWORD` in the application's SecretStore and optionally set public `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_NAME` and tenant variables. Then deploy.

DevBox injects `DB_*` credentials at runtime; the backend entrypoint maps them to `MYSQL_*`. API startup applies the explicit migrations and performs idempotent administrator bootstrap before accepting traffic. Redis/API readiness gates frontend, worker and scheduler startup. The frontend proxies `/api/` and `/health/` to the API on the same origin, preserving session cookies and CSRF checks. Only the frontend port is published; SQL data stays in DevBox's persistent MySQL volume.

Go 1.27 and Node build tooling run inside multi-stage Docker builds, so host interpreters are unnecessary for this deployment. Original sources remain visible through DevBox's RW mounts; rebuild the application after changing Go or frontend code. Secrets, Git metadata, local dependencies and build/test artifacts are excluded from the image build context. The HTTP example uses development cookie settings; configure TLS and secure cookies when deploying HTTPS.

## Local development commands

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
npm ci
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

On 2026-10-11, local Go formatting/vet/unit/race/build, real MySQL 8.4 integration and all 24 migrations (including full rollback/reapply), frontend typecheck/unit/build/audit and installer/Compose checks passed. A real DevBox rebuild preserved the administrator account and MySQL data; login/logout, CSRF rejection, MySQL/Redis readiness, RW source mounts and SELECT 1 from API/worker/scheduler passed.

All six desktop/mobile Playwright tests passed against the real container stack. Final GitHub CI is recorded in [PR #7](https://github.com/chmajster/OpenServiceNOW/pull/7). The browser suite validates administrator login, user/role/group creation and assignments, audit visibility, least-privilege login and server-side denial, plus two-session tenant/role conflicts. This verifies the current application deployment, without declaring all future roadmap capabilities complete.

## Architecture and project state

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [ROADMAP.md](ROADMAP.md)
- [TODO.md](TODO.md)
- [STATUS.md](STATUS.md)
- [docs/adr](docs/adr)

Repository working rules are defined in [AGENTS.md](AGENTS.md).
