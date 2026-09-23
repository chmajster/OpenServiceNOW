# Current Project Status

Last updated: 2026-09-23

## Current phase

Phase 1 — Foundation

The backend foundation is substantially implemented on the active branch, but Phase 1 is not complete because executable CI verification is blocked and the frontend foundation is still missing.

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
- CI workflow covering format, vet, unit, race, build, migrations, integration tests, bootstrap, authenticated smoke flow, scheduler and rollback.

## Currently in progress

Phase 1 remains IN PROGRESS.

Remaining implementation work:

- React + TypeScript frontend foundation,
- frontend authentication state and API client,
- frontend tests/build,
- `cmd/worker` only when the first real asynchronous job workload is introduced,
- trusted-proxy configuration before honoring forwarded client IP headers,
- external identity providers such as LDAP/OIDC in later Phase 1/10 work as planned.

Remaining verification work:

- obtain a GitHub-hosted runner that actually executes workflow steps,
- generate and commit `go.sum` from a successful module resolution,
- run `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, integration tests and build,
- execute migrations and complete API/auth/RBAC smoke flow,
- execute migration rollback verification.

## Known problems

### BLOCKER — GitHub Actions runner is not executing the job

Repeated workflow runs finish as failure before the first workflow step:

- `runner_id = 0`,
- empty runner name,
- empty step list.

This does not constitute a failing build or failing test because no checkout/build/test command is executed.

The assistant execution environment contains Go 1.23.2 and cannot resolve `proxy.golang.org`, so it cannot independently download the Go 1.27 toolchain or module dependencies.

### Dependency lock file

`go.sum` is not present yet because module resolution has not successfully run in an environment with dependency network access. It must be generated and committed before this PR is considered release-ready.

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
- tenant optimistic locking.

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
- migration rollback.

These checks are authored but are not recorded as passing until the runner executes them.

## Database migrations

Implemented:

- `000001_foundation.up.sql`,
- `000001_foundation.down.sql`,
- `000002_identity.up.sql`,
- `000002_identity.down.sql`.

The identity migration contains:

- tenants,
- users,
- tenant memberships,
- groups and memberships,
- permissions,
- roles,
- user/group role assignments,
- sessions,
- audit events.

## Technical debt

Known non-blocking design work:

- forwarded client IPs are intentionally ignored until trusted-proxy configuration exists,
- scheduler should remain a single logical scheduler instance until distributed locking/leader coordination is implemented,
- `cmd/worker` is intentionally absent until a real durable job workload exists,
- frontend foundation is still absent,
- external authentication providers are not implemented yet.

No fake endpoint, placeholder UI or empty worker executable is used to hide these gaps.

## Next recommended tasks

1. Restore executable CI and generate `go.sum`.
2. Fix any build/test/migration defects exposed by the first real CI execution.
3. Establish React + TypeScript frontend foundation.
4. Implement login/session bootstrap in the frontend.
5. Add frontend build/test to CI.
6. Re-run the complete Phase 1 end-to-end verification before merge.
