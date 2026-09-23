# Current Project Status

Last updated: 2026-09-23

## Current phase

Phase 1 — Foundation

The backend and frontend foundations are substantially implemented on the active branch, but Phase 1 is not complete because executable CI verification and dependency lock generation are blocked.

## Last completed

Phase 0 architecture is merged into `main`.

Implemented on the Phase 1 branch:

- Go 1.27 module,
- `cmd/api`,
- `cmd/migrate`,
- `cmd/bootstrap`,
- `cmd/scheduler` with real session-cleanup work and `--once` verification mode,
- validated environment configuration,
- structured JSON logging and request IDs,
- HTTP access logging, body limits, security headers and timeouts,
- graceful API and scheduler shutdown,
- MySQL connection pool using `database/sql`,
- Redis connectivity,
- central transaction manager,
- explicit migrations,
- schema-aware liveness/readiness,
- local users and tenant memberships,
- Argon2id password hashing,
- opaque database-backed sessions,
- CSRF double-submit validation,
- Redis-backed login throttling,
- tenant bootstrap with initial platform administrator,
- groups, roles, permission catalog and assignments,
- backend permission middleware,
- tenant-scoped user administration,
- current-tenant administration with optimistic locking,
- append-oriented audit persistence,
- tenant-scoped paginated audit API,
- OpenAPI 3.1 document for implemented endpoints,
- Docker Compose,
- Makefile commands,
- unit tests for configuration, HTTP, password hashing, authentication gates, RBAC gates and scheduler cleanup,
- MySQL integration test source for cross-tenant isolation and optimistic locking,
- trusted-proxy-aware client IP resolution with spoofing tests,
- bounded password input validation,
- system-role immutability protections,
- React + TypeScript frontend with login, overview, tenant, users, RBAC and audit screens,
- frontend API client with automatic CSRF handling,
- frontend permission-aware navigation and route guards,
- frontend responsive/loading/error/empty states,
- frontend API-client unit tests,
- CI workflow covering backend format/vet/unit/race/build/migrations/integration/smoke/scheduler/rollback and frontend typecheck/test/build.

## Currently in progress

Phase 1 remains IN PROGRESS.

Remaining implementation work:

- `cmd/worker` only when the first real asynchronous job workload is introduced,
- production reverse-proxy/static-frontend packaging when deployment work begins,
- external identity providers such as LDAP/OIDC in later identity/integration work.

Remaining verification work:

- obtain a GitHub-hosted runner that actually executes workflow steps,
- generate and commit `go.sum` from a successful Go module resolution,
- generate and commit `web/package-lock.json` from successful npm dependency resolution,
- run `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, integration tests and backend build,
- execute migrations and complete API/auth/RBAC smoke flow,
- execute scheduler and migration rollback verification,
- run frontend typecheck, tests and production build.

## Known problems

### BLOCKER — GitHub Actions runner is not executing the job

Repeated workflow runs finish as failure before the first workflow step:

- `runner_id = 0`,
- empty runner name,
- empty step list.

This does not constitute a failing build or failing test because no checkout/build/test command is executed.

The assistant execution environment contains Go 1.23.2 and cannot resolve `proxy.golang.org`, so it cannot independently download the Go 1.27 toolchain or module dependencies.

### Dependency lock files

The following lock artifacts are not present because dependency resolution has not successfully run in an environment with network access:

- `go.sum`,
- `web/package-lock.json`.

Both must be generated and committed before this PR is considered merge-ready.

## Tests

Test code exists for:

- configuration validation,
- MySQL DSN round-trip handling,
- health endpoints,
- HTTP security/error behavior,
- Argon2id hashing and verification,
- authentication middleware,
- CSRF rejection,
- backend permission enforcement,
- login throttling,
- effective-permission response,
- RBAC route authorization,
- session cleanup scheduling,
- OpenAPI route inventory,
- cross-tenant user isolation,
- cross-tenant RBAC membership rejection,
- tenant optimistic locking,
- system-role mutation protections,
- last-effective-platform-admin lockout protection across membership/group/role removal paths,
- migration inventory/version invariants and one-statement migration enforcement,
- trusted-proxy client-IP resolution and spoofing rejection,
- password input bounds,
- frontend API client CSRF behavior and error mapping,
- OpenAPI route inventory.

CI is configured to run:

- `gofmt` verification,
- `go vet ./...`,
- `go test ./...`,
- `go test -race ./...`,
- `go build ./...`,
- migration apply,
- MySQL integration tests,
- tenant/admin bootstrap,
- API readiness smoke test,
- admin login,
- tenant user creation,
- role creation and assignment,
- login as the created user and effective-permission verification,
- logout/revocation,
- one scheduler cleanup iteration,
- migration rollback,
- frontend TypeScript typecheck,
- frontend unit tests,
- frontend production build.

These checks are authored but are not recorded as passing until the runner executes them.

## Database migrations

Implemented migration versions:

- `000001_foundation`,
- `000002_tenants`,
- `000003_users`,
- `000004_tenant_memberships`,
- `000005_groups`,
- `000006_group_memberships`,
- `000007_permissions`,
- `000008_roles`,
- `000009_role_permissions`,
- `000010_user_role_assignments`,
- `000011_group_role_assignments`,
- `000012_sessions`,
- `000013_audit_events`.

Each up/down migration contains one SQL statement. Runtime readiness requires migration version 13, `dirty=false`, and the foundation metadata singleton. The migration CLI supports one-step rollback, full rollback and version inspection.

## Technical debt

Known non-blocking design work:

- scheduler should remain a single logical scheduler instance until distributed locking/leader coordination is implemented,
- `cmd/worker` is intentionally absent until a real durable job workload exists,
- production packaging for the built frontend is not defined yet,
- external authentication providers are not implemented yet.

No fake endpoint, placeholder UI or empty worker executable is used to hide these gaps.

## Next recommended tasks

1. Restore executable CI.
2. Generate and commit `go.sum` and `web/package-lock.json`.
3. Fix any backend/frontend build, test or migration defects exposed by the first real CI execution.
4. Re-run the complete Phase 1 end-to-end verification.
5. Merge only after all required checks are green and the PR has no blockers.
