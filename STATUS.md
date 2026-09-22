# Current Project Status

Last updated: 2026-09-22

## Current phase

Phase 1 — Foundation

The first runtime foundation slice is implemented on the active branch.

## Last completed

Phase 0 architecture is merged.

Current foundation slice implements:

- Go module targeting Go 1.27,
- API executable,
- migration executable,
- validated environment configuration,
- structured JSON logging,
- structured HTTP access logs with request IDs,
- HTTP security headers and request body limit,
- HTTP server timeouts and graceful shutdown,
- MySQL connection pool and bounded health checks,
- Redis client and bounded health checks,
- explicit SQL migration runner,
- first foundation migration,
- liveness/readiness endpoints,
- schema-aware readiness,
- Docker Compose for MySQL and Redis,
- Makefile commands,
- unit tests for configuration, health, and router behavior,
- GitHub Actions build/vet/unit/race/migration/API smoke workflow.

## Currently in progress

Phase 1 remains in progress.

Not yet implemented:

- `cmd/worker`,
- `cmd/scheduler`,
- tenant runtime,
- local users/authentication,
- groups,
- roles,
- permissions,
- authorization enforcement,
- audit runtime,
- OpenAPI contract,
- frontend foundation.

## Known problems

No known application defect is currently documented in the implemented foundation slice.

Local verification is constrained in the assistant execution environment because it cannot download the Go 1.27 toolchain or external modules. Repository CI is therefore the authoritative execution verification for this PR.

## Tests

Implemented automated checks:

- `gofmt` verification,
- `go vet ./...`,
- `go test ./...`,
- `go test -race ./...`,
- `go build ./...`,
- migration apply,
- API readiness smoke test against MySQL and Redis,
- migration rollback.

CI result must be green before merge.

## Database migrations

Implemented:

- `000001_foundation.up.sql`,
- `000001_foundation.down.sql`.

The migration creates the singleton `platform_metadata` record used by readiness validation to distinguish a reachable database from a correctly migrated database.

## Technical debt

Current implementation deliberately does not add empty worker/scheduler executables, fake identity endpoints, placeholder frontend screens, or speculative business-domain tables.

Items still to address in Phase 1:

- transaction manager,
- authentication/session design implementation,
- tenant isolation foundation and automated cross-tenant tests,
- RBAC model and middleware/service enforcement,
- audit persistence,
- OpenAPI,
- worker/scheduler runtime once real workloads exist,
- frontend and its test baseline.

## Next recommended tasks

1. Verify and merge the core foundation PR after CI passes.
2. Implement tenant, user, authentication, group, role, permission, and audit schema/services.
3. Add backend RBAC/tenant enforcement tests.
4. Add OpenAPI baseline.
5. Add worker/scheduler when the first real asynchronous/scheduled foundation workload is implemented.
6. Establish React + TypeScript frontend foundation.
