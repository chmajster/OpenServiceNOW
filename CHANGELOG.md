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
