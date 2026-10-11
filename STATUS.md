# Current Project Status

Last updated: 2026-10-11

## Current phase

Foundation and shared work-platform development.

The repository includes the administration foundation, durable MySQL jobs/worker, scheduler and work-record infrastructure. The full application is deployed through DevBox with real MySQL/Redis readiness. This deployment verification does not declare all roadmap phases complete.

## Last completed

Phase 0, the foundation implementation, shared work-platform infrastructure and the local installer are present in `main`. DevBox deployment is tracked in PR #7.

Implemented foundation capabilities:

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

Deployment verification covers the complete frontend/API/worker/scheduler/Redis stack, application-specific MySQL credentials and the original Windows/WSL source. Live readiness, administrator authentication, CSRF rejection, non-root RW mounts and SQL access from API/worker/scheduler pass after a DevBox rebuild.

The full desktop/mobile Playwright suite passed 6/6 against the real container stack. Final GitHub checks are tracked in [PR #7](https://github.com/chmajster/OpenServiceNOW/pull/7). Roadmap business capabilities and external identity providers remain separate development work.

## Known problems

The September GitHub runner-allocation failures are historical. Run #564 on the initial deployment commit executed repository code: frontend passed, while backend formatting and the reserved MySQL table identifier stopped the remaining jobs. Both code defects have been fixed; subsequent CI runs are tracked in PR #7.

No deployment blocker remains after the verified DevBox rebuild. The local endpoint uses development HTTP cookie settings; HTTPS ingress, secure cookies and production datastore policy are deployment choices for a production environment.

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

2026-10-11 local verification passed strict Go formatting, vet, unit tests, race tests and build in Go 1.27 Docker; real MySQL 8.4 integration tests covered migration lifecycle, tenant isolation/RBAC/optimistic locking and work-platform/jobs/outbox/attachments. Frontend installation, typecheck, unit tests (3/3), production build and npm audit passed (0 vulnerabilities). Shell syntax, ShellCheck, installer help and both Compose configurations passed. All six desktop/mobile Playwright tests passed against the real container stack. Final GitHub checks are recorded in PR #7.

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
- `000013_audit_events`,
- `000014_record_sequences`,
- `000015_jobs`,
- `000016_job_attempts`,
- `000017_outbox_events`,
- `000018_attachments`,
- `000019_activity_entries`,
- `000020_work_assignments`,
- `000021_work_messages`,
- `000022_work_watchers`,
- `000023_work_relations`,
- `000024_job_logs`.

Each up/down migration contains one SQL statement. Runtime readiness requires migration version 24, `dirty=false`, and the foundation metadata singleton. The real MySQL lifecycle test passed latest → one-step rollback → latest → full rollback → latest → readiness. The reserved `groups` identifier is quoted in schema and repository SQL.

## Technical debt

Known non-blocking design work:

- session cleanup scheduler uses a MySQL advisory lock so parallel replicas skip duplicate work; broader multi-job scheduler coordination will evolve with future workloads,
- durable job processing and retry/outbox behavior are implemented; broader workload coverage evolves with roadmap features,
- frontend/API container packaging is implemented; production TLS/ingress and operational deployment policy remain environment-specific,
- external authentication providers are not implemented yet.

No fake endpoint, placeholder UI or empty worker executable is used to hide these gaps.

## Next recommended tasks

1. Complete roadmap capabilities with their API/UI and tenant/RBAC guarantees.
2. Extend business-workload, retry/cancellation and external-provider verification as those features are introduced.
3. Configure TLS ingress, secure cookies, datastore policy, backups and monitoring for a production deployment.
4. Keep full backend/frontend/integration/browser CI passing for future changes.
