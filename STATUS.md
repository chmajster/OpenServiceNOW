# Current Project Status

Last updated: 2026-09-27

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
- tenant-scoped read models for group members, direct user-role assignments and group-role assignments,
- RBAC assignment UI for add/remove membership and user/group roles with server refetch after every mutation,
- optimistic locking for role permission replacement,
- append-oriented audit persistence,
- tenant-scoped lookahead cursor audit API,
- audit HTTP transport separated into auditapi to preserve acyclic package dependencies,
- OpenAPI 3.1 document for implemented endpoints,
- explicit OpenAPI permission metadata for every administrative operation, including dual permission requirements on user-bearing RBAC reads and writes,
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
- effective-platform-admin authorization for every system-role assignment/removal path and privileged-group membership mutation,
- dormant system-role binding protection during tenant-membership activation/deactivation,
- tenant-row serialization across privilege-sensitive RBAC and membership mutations,
- dynamic platform_admin inheritance of the live permission catalog,
- React + TypeScript frontend with login, overview, tenant, users, RBAC and audit screens,
- frontend API client with automatic CSRF handling,
- frontend permission-aware navigation and route guards,
- frontend responsive/loading/error/empty states,
- frontend API-client unit tests,
- Playwright browser E2E for the critical admin-to-least-privilege-user journey,
- E2E coverage for security-group membership, group-role assignment, audit visibility and real HTTP 403 enforcement,
- two-session E2E coverage for stale optimistic-lock conflicts and UI refetch,
- desktop Chromium and Pixel 7 Chromium projects with isolated test data,
- mobile-safe logout and permission-aware read-only tenant UI,
- server-side bounded search/pagination controls in user/RBAC screens,
- role permission editor using optimistic locking,
- CI workflow covering dependency manifest stability, backend format/vet/unit/race/build/migrations/integration/smoke/advisory-lock scheduler/rollback and frontend typecheck/test/build.

## Currently in progress

Phase 1 remains IN PROGRESS.

Later-phase work intentionally not required for Phase 1:

- `cmd/worker` is deferred to Phase 2B and will only be introduced with the first durable job workload,
- production reverse-proxy/static-frontend packaging belongs to deployment work,
- external identity providers such as LDAP/OIDC belong to later identity/integration work.

Remaining verification work:

- obtain a GitHub-hosted runner that actually executes workflow steps,
- generate and commit `go.sum` from a successful Go module resolution,
- generate and commit `web/package-lock.json` from successful npm dependency resolution,
- run `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, integration tests and backend build,
- execute migrations and complete API/auth/RBAC smoke flow,
- execute the authored full migration lifecycle test: latest → one-step rollback → latest → full rollback → latest → readiness,
- execute scheduler verification,
- run frontend typecheck, tests and production build,
- run full Playwright E2E on desktop and mobile Chromium against the real API/MySQL/Redis stack.

## Known problems

### BLOCKER — GitHub-hosted runner allocation fails before the first step

A workflow YAML parsing defect was found and fixed: an inline `run:` scalar contained `version: none dirty: false`, which made GitHub treat the workflow as invalid. After the fix, GitHub correctly recognizes the workflow name and creates the `test` and `frontend` jobs.

A temporary minimal diagnostic workflow with one `echo` step was also created and then removed. It showed the same remaining failure pattern as the real workflow:

- job object is created,
- job immediately finishes with `conclusion=failure`,
- `steps=null`,
- no job logs are available.

Therefore the YAML/parser problem is resolved. The remaining CI blocker occurs before the first runner step and is separate from application build/test results.

On 2026-09-26 the workflow was changed from `ubuntu-24.04` to `ubuntu-latest`; run #474 still failed with `runner_id=0`, empty runner name and no steps. The workflow was then changed to avoid duplicate `push` + `pull_request` runs for feature branches and to cancel superseded runs. Run #475 still failed identically before runner allocation.

The latest checked reproduction is run #541 (`36293288203`) on commit `edbb86575782286eccf483af5eb78c1117b2cde1`. Jobs `test`, `frontend` and `e2e` each completed with `conclusion=failure` and `steps=null`; no checkout or repository code executed. This remains a runner-allocation/platform blocker, not an application test failure.

The previous workflow triggered both `push` on `foundation/**` and `pull_request`, causing duplicate runs for each PR-branch commit. This has been corrected; feature branches now run through the PR trigger, while direct `push` CI is limited to `main`.

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
- full MySQL migration lifecycle source covering latest → one-step rollback → re-upgrade → full rollback → re-upgrade → schema readiness,
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
- quoted optimistic-lock version decoding in tenant/RBAC HTTP transports,
- Redis rate-limiter backend failure returning a stable 503 without entering password authentication,
- RBAC assignment reads and user-bearing writes requiring the additional user-management permission where user identities are exposed or changed,
- negative privilege-escalation coverage for direct system-role assignment, system-role group assignment, privileged-group membership changes and dormant administrator reactivation,
- group membership and user/group role mutation → repository readback cycles with cross-tenant rejection,
- OpenAPI permission metadata on every administrative operation.

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

These checks are authored but are not recorded as passing until a runner actually executes them. The current workflow still cannot generate the missing dependency lock files because no job reaches its first step.

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

1. Restore GitHub-hosted runner allocation and rerun `foundation-ci`.
2. Generate and commit `go.sum` and `web/package-lock.json` through real dependency resolution; do not synthesize either file.
3. Execute the complete backend, migration, frontend and Playwright matrix and fix every defect it exposes.
4. Repeat the full Phase 1 verification on the final commit.
5. Merge PR #3 only after all required checks have actually executed successfully and no blocker remains.
6. Start Phase 2 on a separate branch/Draft PR only after that merge.
