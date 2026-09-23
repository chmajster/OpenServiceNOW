# Current Project Status

Last updated: 2026-09-23

## Current phase

Phase 1 — Foundation

The backend and frontend foundations are substantially implemented on the active branch, but Phase 1 is not complete because executable CI verification and dependency lock generation remain blocked.

## Last completed

Phase 0 architecture is merged into `main`.

Implemented on the Phase 1 branch:

- Go 1.27 module,
- `cmd/api`,
- `cmd/migrate`,
- `cmd/bootstrap`,
- `cmd/scheduler` with real session-cleanup work and `--once` verification mode,
- validated environment configuration,
- structured JSON logging and server-generated request IDs,
- HTTP access logging, body limits, security/privacy headers, server-generated request IDs and timeouts,
- controlled JSON panic recovery,
- panic-safe transaction rollback,
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
- tenant-scoped user administration with cursor pagination/search, existing-global-account membership linking and membership-triggered session revocation,
- current-tenant administration with optimistic locking,
- role/group administration with cursor pagination/search,
- optimistic locking for role permission replacement,
- append-oriented audit persistence,
- tenant-scoped lookahead cursor audit API,
- audit HTTP transport separated into auditapi to preserve acyclic package dependencies,
- OpenAPI 3.1 document for implemented endpoints,
- precision-safe decimal-string JSON contract for MySQL BIGINT identifiers, cursors, counts and optimistic-lock versions,
- Docker Compose,
- Makefile commands,
- unit tests for configuration, HTTP, password hashing, authentication gates, RBAC gates and scheduler cleanup,
- MySQL integration test source for cross-tenant isolation and optimistic locking,
- trusted-proxy-aware client IP resolution with spoofing/all-trusted-chain tests,
- canonical IP metadata and server-generated request IDs,
- optional verified TLS for MySQL and Redis,
- MySQL session timezone forced to UTC,
- bounded password input and stored Argon2 work-factor validation,
- bounded concurrent Argon2 password verification,
- system-role immutability protections,
- dynamic platform_admin inheritance of the live permission catalog,
- React + TypeScript frontend with login, overview, tenant, users, RBAC and audit screens,
- frontend API client with automatic CSRF handling,
- frontend permission-aware navigation and route guards,
- frontend responsive/loading/error/empty states,
- frontend API-client unit tests,
- Playwright browser E2E for the critical admin-to-least-privilege-user journey,
- mobile-safe logout and permission-aware read-only tenant UI,
- server-side bounded search/pagination controls in user/RBAC screens,
- role permission editor using optimistic locking,
- CI workflow covering dependency manifest stability, backend format/vet/unit/race/build/migrations/integration/smoke/advisory-lock scheduler/rollback and frontend typecheck/test/build.

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

### BLOCKER — GitHub Actions jobs still fail before the first step

A workflow YAML parsing defect was found and fixed: an inline `run:` scalar contained `version: none dirty: false`, which made GitHub treat the workflow as invalid. After the fix, GitHub correctly recognizes the workflow name and creates the `test` and `frontend` jobs.

A temporary minimal diagnostic workflow with one `echo` step was also created and then removed. It showed the same remaining failure pattern as the real workflow:

- job object is created,
- job immediately finishes with `conclusion=failure`,
- `steps=null`,
- no job logs are available.

Therefore the YAML/parser problem is resolved. The remaining CI blocker occurs before the first runner step and is separate from application build/test results.

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
- OpenAPI route inventory, local $ref resolution, unique operation IDs and decimal-string BIGINT contract,
- role permission optimistic-lock conflicts and no-op version stability,
- explicit linking of an existing global account to a second tenant without password mutation,
- audit cursor pagination and tenant isolation,
- safe bootstrap re-entry for existing users,
- bounded password-verifier saturation behavior,
- panic-safe transaction behavior,
- precision-safe BIGINT JSON serialization above JavaScript `Number.MAX_SAFE_INTEGER`,
- quoted optimistic-lock version decoding in tenant/RBAC HTTP transports.

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
- frontend production build,
- full-stack Playwright Chromium E2E through UI → API → MySQL.

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

- session cleanup scheduler uses a MySQL advisory lock so parallel replicas skip duplicate work; broader multi-job scheduler coordination will evolve with future workloads,
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
