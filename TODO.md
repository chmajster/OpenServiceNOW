# OpenServiceNOW TODO

Legend:

- `[P0] [ ]` — critical, not started
- `[P1] [-]` — high priority, in progress
- `[P2] [x]` — normal priority, completed
- `[P3] [!]` — improvement/blocker requiring attention
- `P4` — optional

Only work that actually exists and meets the applicable Definition of Done should be marked complete.

## Phase 0 — Architecture

[P2] [x] Analyze current repository and existing stack.

Completed:
- repository contains only AGENTS.md before Phase 0,
- no runtime code, database, frontend, tests, CI, or migrations existed.

[P2] [x] Define Go modular-monolith architecture.

Completed:
- executable boundaries,
- business module boundaries,
- layering/dependency rules,
- worker/scheduler/event direction,
- extraction criteria.

[P2] [x] Define MySQL persistence architecture.

Completed:
- MySQL 8.x system of record,
- database/sql + go-sql-driver/mysql,
- repository pattern,
- explicit migrations,
- transaction approach,
- pool configuration,
- tenant-aware access principles.

[P2] [x] Define cross-cutting architecture.

Completed:
- multi-tenancy,
- RBAC,
- authentication direction,
- audit,
- optimistic locking,
- events,
- jobs,
- SLA,
- workflow,
- observability,
- graceful shutdown,
- frontend,
- testing,
- deployment direction.

[P2] [x] Establish architecture decision records.

Completed:
- ADR-0001 Go modular monolith,
- ADR-0002 MySQL persistence.

[P2] [x] Establish project management documentation.

Completed:
- ARCHITECTURE.md,
- ROADMAP.md,
- TODO.md,
- STATUS.md,
- CHANGELOG.md,
- docs/README.md.

## Phase 1 — Foundation

[P0] [ ] Initialize Go module and executable entry points.

Required:
- go.mod,
- cmd/api,
- cmd/worker,
- cmd/scheduler,
- clean package boundaries,
- buildable binaries.

[P0] [ ] Implement configuration subsystem.

Required:
- environment parsing,
- required/optional values,
- safe defaults,
- typed durations/integers,
- startup validation,
- no secret logging,
- tests.

[P0] [ ] Implement structured logging.

Required:
- level,
- timestamp,
- request_id,
- tenant/user context where available,
- module/operation,
- duration,
- redaction rules,
- tests for sensitive fields where practical.

[P0] [ ] Implement HTTP server foundation.

Required:
- chi router,
- request ID,
- panic recovery,
- access logging,
- body-size limit,
- security headers,
- timeouts,
- consistent error envelope,
- graceful shutdown,
- httptest coverage.

[P0] [ ] Implement MySQL connection layer.

Required:
- database/sql,
- go-sql-driver/mysql,
- DSN construction without logging password,
- configurable pool,
- PingContext health test,
- error handling,
- integration test harness.

[P0] [ ] Implement migration runner.

Required:
- explicit up/down SQL migrations,
- deterministic startup/CLI behavior,
- migration status visibility,
- MySQL integration tests,
- no AutoMigrate.

[P0] [ ] Add first schema migration.

Initial scope should cover only foundational entities that are actually implemented in the same phase, not speculative tables for all future modules.

[P0] [ ] Implement Redis connection layer.

Required:
- configuration,
- health behavior,
- context/timeout,
- graceful shutdown,
- tests.

[P0] [ ] Implement health/readiness endpoints.

Required:
- process liveness,
- MySQL readiness,
- Redis readiness according to service dependency semantics,
- bounded timeouts,
- no secret leakage.

[P0] [ ] Implement tenant foundation.

Required:
- tenants table/model,
- request tenant context,
- tenant-safe repository helpers/patterns,
- cross-tenant denial tests.

[P0] [ ] Implement local users and authentication foundation.

Required:
- local user model,
- Argon2id or bcrypt,
- login/session or token strategy,
- logout/revocation semantics,
- secure cookie/token handling,
- brute-force/rate-limit plan,
- tests.

[P0] [ ] Implement groups, roles, permissions, and assignments.

Required:
- schema,
- repositories,
- service layer,
- backend authorization enforcement,
- tests.

[P0] [ ] Implement audit foundation.

Required:
- append-oriented audit schema,
- actor/tenant/object/request metadata,
- old/new values where applicable,
- integration with privileged foundation actions,
- tests.

[P1] [ ] Establish OpenAPI contract generation/validation approach.

[P1] [ ] Add Docker Compose for MySQL and Redis development dependencies.

[P1] [ ] Add Makefile targets.

Expected:
- make dev,
- make build,
- make test,
- make lint,
- make migrate-up,
- make migrate-down,
- make seed,
- make frontend,
- make e2e.

Targets should only be added when the underlying command exists and works.

[P1] [ ] Add frontend foundation.

Required:
- React,
- TypeScript,
- router,
- API client boundary,
- authentication state,
- error/loading primitives,
- test/build baseline.

[P1] [ ] Add CI once build/test commands exist.

Required:
- backend build,
- gofmt verification,
- go vet,
- go test,
- frontend typecheck/build/test,
- integration tests where infrastructure is available.

## Phase 2+

[P2] [ ] Implement task foundation.
[P2] [ ] Implement Incident Management.
[P2] [ ] Implement SLA engine.
[P2] [ ] Implement Requests and Service Catalog.
[P2] [ ] Implement Workflow and Approval engines.
[P2] [ ] Implement Change and Problem Management.
[P2] [ ] Implement CMDB and Asset Management.
[P2] [ ] Implement Knowledge Base.
[P2] [ ] Implement Notifications and Integrations.
[P2] [ ] Implement Automation.
[P2] [ ] Implement Reporting and Dashboards.
[P2] [ ] Implement Self-Service Portal.
[P2] [ ] Implement Advanced Administration.
[P2] [ ] Perform final hardening phase.

Detailed scope and exit criteria are defined in ROADMAP.md.
