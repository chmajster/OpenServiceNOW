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
- initial tenant/admin bootstrap command,
- Docker Compose development services,
- unit/race/build/migration/authentication smoke CI.

Worker/scheduler workloads, external identity providers, user/RBAC administration APIs, OpenAPI, frontend, and ITSM business modules are not implemented yet.

## Target stack

- Go 1.27,
- `net/http` + chi,
- `database/sql` + `go-sql-driver/mysql`,
- MySQL 8.x,
- Redis,
- React + TypeScript frontend.

## Local development

Start dependencies:

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

Start the API:

```bash
go run ./cmd/api
```

Health endpoints:

```text
GET /health/live
GET /health/ready
```

Authentication endpoints:

```text
POST /api/v1/auth/login
GET  /api/v1/auth/me
POST /api/v1/auth/logout
```

Login expects:

```json
{
  "tenant": "default",
  "email": "admin@example.test",
  "password": "your-password"
}
```

The server sets an HttpOnly session cookie and a separate CSRF cookie. Unsafe authenticated requests require the CSRF cookie value in the `X-CSRF-Token` header.

The API does not trust `X-Forwarded-For` by default. Until trusted-proxy configuration is implemented, rate limiting and audit IPs use the direct peer address.

## Development commands

```bash
make build
make test
make lint
make migrate-up
make migrate-down
make bootstrap
```

`make migrate-down` rolls back one migration.

## Architecture and project state

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [ROADMAP.md](ROADMAP.md)
- [TODO.md](TODO.md)
- [STATUS.md](STATUS.md)
- [docs/adr](docs/adr)

Repository working rules are defined in [AGENTS.md](AGENTS.md).
