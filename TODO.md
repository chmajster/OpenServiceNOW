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
- server-generated request IDs and access logs,
- body limits and HTTP 413 handling,
- security/privacy headers and no-store responses,
- bounded request IDs,
- controlled JSON panic recovery,
- request/server timeouts,
- panic-safe transaction rollback,
- graceful shutdown,
- consistent JSON error envelope,
- precision-safe decimal-string serialization for BIGINT identifiers/cursors/versions,
- unit tests authored.

Remaining:
- execute format/vet/unit/race/build checks successfully.

[P0] [-] MySQL, transactions and migrations.

Implemented:
- `database/sql` + go-sql-driver/mysql,
- safe DSN generation through driver config,
- optional MySQL TLS modes,
- UTC MySQL session timezone,
- configurable pool,
- bounded PingContext,
- central transaction manager,
- explicit migration runner,
- migration versions `000001` through `000013`,
- one SQL statement per migration direction,
- runtime DSN with multi-statements disabled,
- migration-only DSN compatible with golang-migrate `WithInstance`,
- one-step rollback, full rollback and migration-version commands,
- readiness bound to the exact clean schema version,
- tests binding migration inventory to `RequiredSchemaVersion`,
- integration test source.

Remaining:
- execute the authored latest → down-one → latest → down-all → latest → readiness integration lifecycle successfully,
- successful migration/integration verification,
- generate and commit `go.sum` from real dependency resolution.

[P0] [-] Redis foundation.

Implemented:
- client configuration,
- optional TLS with minimum TLS 1.2,
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
- session revocation, including bulk revocation when tenant membership is disabled,
- Redis login throttling,
- secure cookie settings,
- bootstrap tenant/admin command,
- tenant-scoped user administration with bounded cursor pagination/search,
- explicit membership linking for an already-existing active global identity without changing its password/profile,
- system-role binding checks on membership activation/deactivation so disabled privileged memberships cannot be reactivated by delegated user administrators,
- session cleanup scheduler with MySQL advisory-lock replica coordination,
- explicit trusted-proxy CIDRs and safe forwarded-IP resolution,
- bounded login/password input sizes,
- bounded stored Argon2 work factors,
- bounded concurrent Argon2 verification with AUTH_BUSY overload handling,
- safe idempotent bootstrap requiring the existing user's password,
- unit and smoke tests authored.

Remaining:
- executable verification.

[P0] [-] Groups, roles, permissions and assignments.

Implemented:
- schema,
- repositories,
- service layer,
- group membership,
- role assignments to users/groups,
- tenant-scoped paginated readback for group members, direct user roles and group roles,
- UI add/remove flows for membership and user/group role assignments with refetch after every mutation,
- cursor pagination/search for roles and groups,
- optimistic-lock permission replacement,
- immutable system-role permissions,
- protection against removing the last active effective `platform_admin`,
- dynamic `platform_admin` inheritance of every permission present in the live catalog,
- reserved `platform_admin` role name,
- idempotent assignments without false audit events,
- backend `RequirePermission` middleware,
- tenant-scoped administration API,
- audit integration,
- auth/permission/CSRF route tests authored,
- cross-tenant assignment-read integration coverage,
- user-detail assignment reads and user-bearing assignment writes require both `admin.rbac.manage` and `admin.users.manage`,
- all direct/group system-role mutations and membership changes on system-role groups require an already-effective `platform_admin`,
- privilege-sensitive RBAC mutations serialize on the tenant row to close group-role/member race windows.

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
- contract tests checking parseability, route inventory, local $ref resolution and unique operation IDs,
- explicit `x-required-permissions` metadata on every administrative operation,
- contract test preventing future admin endpoints from omitting permission metadata.

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
- `make migrate-down-all`,
- `make migrate-version`,
- `make bootstrap`,
- `make scheduler-once`,
- `make frontend`,
- `make frontend-test`,
- `make e2e`.

Remaining:
- successful browser E2E execution in CI.

[P1] [-] React + TypeScript frontend foundation.

Implemented:
- React + TypeScript + React Router + Vite,
- same-origin API proxy for development,
- API client with cookie credentials and automatic CSRF header,
- session/authentication context,
- permission-aware navigation and route guards,
- login screen,
- overview/health screen,
- tenant administration with optimistic-lock conflict handling,
- tenant user administration,
- RBAC role/group/assignment administration,
- paginated audit screen,
- responsive layout,
- loading/error/empty/success states,
- API-client unit tests,
- Playwright browser E2E covering admin login, user creation, direct role assignment, security-group membership, group-role assignment, audit visibility, logout, least-privilege login and permission-aware navigation,
- two-session stale-version conflict E2E with server refetch,
- desktop Chromium and Pixel 7 Chromium projects using isolated test data,
- CI typecheck/test/build/E2E jobs.

Remaining:
- successful typecheck/test/build execution,
- generate and commit `web/package-lock.json`,
- production static-frontend/reverse-proxy packaging in deployment work.

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
- rollback,
- frontend typecheck,
- frontend tests,
- frontend build,
- full-stack Playwright E2E.

Blocked:
- workflow YAML parsing defect has been fixed,
- all jobs use `ubuntu-latest`,
- duplicate feature-branch `push` + `pull_request` executions have been removed and superseded runs are cancelled through workflow concurrency,
- latest checked run #511 (`36271136402`) still fails before runner assignment for `test`, `frontend` and `e2e`: `runner_id=0`, empty runner name/group and zero executed steps,
- GitHub Status reported Actions operational at the same time,
- executable verification is therefore blocked by GitHub-hosted runner allocation/account or repository Actions policy/quota/billing rather than application code.

[P1] [!] Generate and commit dependency lock files.

Required:
- `go.sum`,
- `web/package-lock.json`.

Blocked:
- dependency network is unavailable in the assistant runtime,
- GitHub Actions runners have not started far enough to resolve dependencies.

## Phase 2+

[P1] [ ] Implement shared work-record primitives.
[P1] [ ] Implement durable jobs and `cmd/worker`.
[P1] [ ] Implement transactional outbox and domain-event delivery.
[P1] [ ] Implement attachment storage/security foundation.
[P1] [ ] Implement unified activity timeline.
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
