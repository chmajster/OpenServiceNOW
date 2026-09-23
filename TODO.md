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

[P1] [-] Go runtime and executable boundaries.

Implemented:
- `go.mod`,
- `cmd/api`,
- `cmd/migrate`,
- `cmd/bootstrap`,
- `cmd/scheduler` with real session-cleanup workload,
- clean platform/domain package boundaries.

Remaining:
- executable CI verification,
- `cmd/worker` only when a real durable job workload exists.

[P0] [-] Configuration, logging and HTTP foundation.

Implemented:
- typed environment parsing,
- fail-fast validation,
- structured JSON logging,
- request IDs and access logs,
- body limits,
- security headers,
- request/server timeouts,
- graceful shutdown,
- consistent JSON error envelope,
- unit tests authored.

Remaining:
- execute format/vet/unit/race/build checks successfully.

[P0] [-] MySQL, transactions and migrations.

Implemented:
- `database/sql` + go-sql-driver/mysql,
- safe DSN generation through driver config,
- configurable pool,
- bounded PingContext,
- central transaction manager,
- explicit migration runner,
- migration 000001 foundation,
- migration 000002 identity/RBAC/audit,
- migration rollback files,
- integration test source.

Remaining:
- successful migration/integration verification,
- generate and commit `go.sum`.

[P0] [-] Redis foundation.

Implemented:
- client configuration,
- bounded ping,
- login rate-limit storage,
- readiness integration.

Remaining:
- executable verification in CI.

[P0] [-] Health/readiness.

Implemented:
- liveness,
- MySQL readiness,
- Redis readiness,
- migrated-schema readiness,
- tests authored.

Remaining:
- executable verification.

[P0] [-] Tenant foundation.

Implemented:
- tenants schema/model/repository/service/API,
- authenticated tenant scope,
- no client-supplied tenant selector on admin APIs,
- optimistic locking via `version`,
- audit integration,
- cross-tenant integration tests authored.

Remaining:
- execute MySQL isolation/locking tests successfully.

[P0] [-] Local users and authentication foundation.

Implemented:
- local users and tenant memberships,
- Argon2id hashing,
- opaque session tokens stored only as hashes,
- CSRF token binding,
- login/logout/me flow,
- session revocation,
- Redis login throttling,
- secure cookie settings,
- bootstrap tenant/admin command,
- tenant-scoped user administration,
- session cleanup scheduler,
- unit and smoke tests authored.

Remaining:
- executable verification,
- trusted-proxy configuration before forwarded IP support.

[P0] [-] Groups, roles, permissions and assignments.

Implemented:
- schema,
- repositories,
- service layer,
- group membership,
- role assignments to users/groups,
- permission replacement,
- backend `RequirePermission` middleware,
- tenant-scoped administration API,
- audit integration,
- auth/permission/CSRF route tests authored.

Remaining:
- executable integration verification.

[P0] [-] Audit foundation.

Implemented:
- append-oriented audit table,
- actor/tenant/object/request/source/IP metadata,
- old/new JSON values,
- audit events in privileged transactions,
- tenant-scoped paginated audit reader/API.

Remaining:
- executable verification and later retention policy.

[P1] [-] OpenAPI contract.

Implemented:
- OpenAPI 3.1 JSON document,
- only currently implemented endpoints are documented,
- cookie session and CSRF security schemes,
- contract test checking parseability and route inventory.

Remaining:
- execute contract test successfully,
- later introduce stricter schema validation/generation if useful.

[P1] [-] Docker Compose development environment.

Implemented:
- MySQL 8.4,
- Redis 8.2,
- health checks,
- persistent MySQL development volume.

Remaining:
- successful end-to-end execution in an environment with Docker.

[P1] [-] Makefile commands.

Implemented:
- `make dev`,
- `make build`,
- `make test`,
- `make lint`,
- `make migrate-up`,
- `make migrate-down`,
- `make bootstrap`,
- `make scheduler-once`.

Remaining:
- frontend/e2e targets after those subsystems exist.

[P1] [ ] React + TypeScript frontend foundation.

Required:
- React,
- TypeScript,
- router,
- API client boundary,
- authentication state,
- loading/empty/error primitives,
- test/build baseline.

[P1] [!] Execute CI successfully.

Implemented workflow:
- formatting,
- vet,
- unit,
- race,
- build,
- migrations,
- MySQL integration tests,
- bootstrap,
- auth/RBAC/user smoke flow,
- scheduler one-shot,
- rollback.

Blocked:
- GitHub job repeatedly ends before checkout with no runner and no steps.

[P1] [!] Generate and commit `go.sum`.

Blocked:
- dependency network is unavailable in the assistant runtime,
- GitHub Actions runner has not started far enough to resolve modules.

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
