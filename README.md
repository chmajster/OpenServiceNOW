# OpenServiceNOW

OpenServiceNOW is an enterprise ITSM / IT Operations platform implemented independently from ServiceNow code, branding, and UI.

## Current state

Phase 1 — Foundation is in progress.

The repository now contains a working Go API foundation with:

- validated environment configuration,
- structured JSON logging,
- request IDs and structured HTTP access logs,
- graceful shutdown,
- MySQL 8.x connectivity through `database/sql`,
- configurable MySQL connection pool,
- Redis connectivity,
- explicit SQL migrations through `golang-migrate`,
- liveness and readiness endpoints,
- readiness validation of both dependencies and migrated schema,
- Docker Compose development dependencies,
- baseline unit/race/build/smoke CI.

Identity, authentication, tenants, RBAC, audit runtime, worker/scheduler workloads, frontend, and business modules are not implemented yet.

## Target stack

Backend:

- Go 1.27,
- `net/http` + chi,
- `database/sql`,
- `go-sql-driver/mysql`,
- explicit SQL migrations.

Data:

- MySQL 8.x as the durable system of record,
- Redis for cache, locks, rate limits, temporary coordination, and jobs.

Frontend target:

- React,
- TypeScript,
- API-driven architecture.

## Local development

Start MySQL and Redis:

```bash
make dev
```

Set runtime configuration:

```bash
cp .env.example .env
set -a
source .env
set +a
```

Apply the foundation migration:

```bash
make migrate-up
```

Start the API:

```bash
go run ./cmd/api
```

Health endpoints:

```text
GET /health/live
GET /health/ready
```

The readiness endpoint returns HTTP 503 until MySQL, Redis, and the required foundation schema are available.

## Development commands

```bash
make build
make test
make lint
make migrate-up
make migrate-down
```

## Architecture and project state

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [ROADMAP.md](ROADMAP.md)
- [TODO.md](TODO.md)
- [STATUS.md](STATUS.md)
- [docs/adr](docs/adr)

Repository working rules are defined in [AGENTS.md](AGENTS.md).
