# Changelog

All notable repository changes are documented here.

The project is pre-release and currently in architecture/foundation development.

## 2026-09-22 — Phase 0 architecture

### Added

- target architecture for a Go modular-monolith backend,
- MySQL 8.x persistence strategy using database/sql and go-sql-driver/mysql,
- repository/service/transport layering rules,
- multi-tenant isolation architecture,
- backend-enforced RBAC direction,
- event, worker, scheduler, SLA, workflow, audit, and automation architecture,
- React + TypeScript frontend direction,
- security, concurrency, performance, testing, observability, and deployment principles,
- phased implementation roadmap,
- prioritized TODO register,
- factual current project status,
- ADR-0001 for Go modular monolith,
- ADR-0002 for MySQL persistence,
- documentation index.

### Runtime status

No runtime application was introduced in Phase 0. Go code, schema migrations, frontend code, Docker Compose, and tests are intentionally deferred to Phase 1 so the repository does not contain non-functional placeholders represented as complete.

## 2026-09-22 — Phase 1 core foundation

### Added

- Go 1.27 module and API runtime,
- fail-fast environment configuration validation,
- structured JSON application and HTTP access logging,
- request IDs, security headers, body limits, and server timeouts,
- graceful API shutdown,
- MySQL connection pool and readiness checks,
- Redis client and readiness checks,
- explicit migration runner without embedding database credentials in migration URLs,
- foundation schema metadata migration,
- liveness and schema-aware readiness endpoints,
- Docker Compose development services,
- Makefile build/test/lint/migration commands,
- configuration, health, and router unit tests,
- GitHub Actions build, vet, race, migration, and API smoke workflow.

### Security

- migration execution reuses an existing database handle to avoid propagating credential-bearing DSNs through migration error paths,
- malformed numeric and duration environment values fail startup instead of silently falling back.

## 2026-09-23 — Phase 1 identity, RBAC and administration foundation

### Added

- central transaction manager,
- Argon2id password hashing,
- tenants, users, memberships, groups, roles, permissions, sessions and audit schema,
- local login/logout/current-session API,
- opaque session tokens stored only as SHA-256 hashes,
- CSRF double-submit validation,
- Redis-backed login throttling,
- tenant/admin bootstrap command,
- tenant-scoped user administration API,
- backend RBAC middleware and administration API,
- tenant administration with optimistic locking,
- append-oriented audit events and tenant-scoped paginated audit API,
- session cleanup scheduler with one-shot CI mode,
- OpenAPI 3.1 contract for implemented endpoints,
- cross-tenant isolation and optimistic-lock integration test source,
- authenticated CI smoke flow creating a user, role and assignment.

### Security

- client-controlled tenant identifiers are not accepted for administrative data access; tenant scope comes from the authenticated session,
- forwarded client IP headers are intentionally ignored until trusted-proxy configuration exists,
- privileged writes combine state changes and audit records in the same MySQL transaction,
- RBAC writes require authentication, backend permission checks and CSRF validation,
- user administration cannot disable the acting administrator's own tenant membership,
- role/group membership operations enforce tenant ownership in SQL.

### Verification status

The implementation remains unmerged because GitHub Actions jobs are repeatedly terminating before checkout with no assigned runner and no executed steps. Test/build success is not claimed until the workflow actually runs.

## 2026-09-23 — Phase 1 frontend and request-security foundation

### Added

- React + TypeScript + React Router frontend,
- Vite development proxy for same-origin API access,
- cookie-session authentication context,
- automatic CSRF header handling in the frontend API client,
- permission-aware navigation and route guards,
- login, overview, tenant, user, RBAC and audit screens,
- responsive desktop/mobile layout and explicit loading/error/empty/success states,
- frontend API-client unit tests,
- frontend typecheck/test/build CI job,
- explicit trusted proxy CIDR configuration,
- safe right-to-left `X-Forwarded-For` resolution only when the direct peer is trusted,
- trusted-proxy spoofing tests,
- fixed session/CSRF cookie protocol names shared by backend, frontend and OpenAPI,
- password and login input size limits.

### Security

- untrusted peers cannot override audit/rate-limit client IPs through forwarded headers,
- `platform_admin` system-role permissions cannot be replaced,
- an administrator cannot remove their own system role through the RBAC API,
- frontend logout retains authenticated state and surfaces an error if backend revocation fails,
- session cookie protocol names cannot be changed independently of the frontend.

### Verification status

Backend and frontend GitHub Actions jobs still fail before runner allocation with empty step lists. No build/test success is claimed. `go.sum` and `web/package-lock.json` remain required before merge.

## 2026-09-23 — Phase 1 migration and administrator-safety hardening

### Changed

- split the identity/RBAC/audit schema into migration versions `000002`–`000013`, one table per version,
- each migration direction now contains a single SQL statement,
- runtime MySQL connections keep multi-statements disabled while migration connections explicitly enable them for golang-migrate,
- readiness now requires the exact clean schema version and installation metadata,
- migration CLI now supports `down-all` and `version`,
- CI verifies complete schema rollback to no applied migration version,
- duplicate RBAC assignment requests remain idempotent without generating false audit events,
- no-op tenant-name and membership updates no longer create artificial version/audit churn.

### Security

- privilege-reducing operations serialize on the tenant row and guarantee at least one active effective `platform_admin` remains,
- last-admin protection covers tenant membership deactivation, direct role removal, group membership removal and group-role removal,
- lock ordering is consistently tenant-first to reduce deadlock risk during concurrent administrative changes.
