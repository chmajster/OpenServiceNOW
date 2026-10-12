# Current Project Status

Last updated: 2026-10-12

## Current phase

Foundation and shared work-platform development.

The repository includes the administration foundation, durable MySQL jobs/worker, scheduler and work-record infrastructure. The full application is deployed through DevBox with real MySQL/Redis readiness. This deployment verification does not declare all roadmap phases complete.

## Last completed

Phase 0, the administration foundation, shared work-platform libraries and both local installer modes are delivered through PR #14. DevBox deployment was delivered in PR #7. PR #15 rebuilds the existing administration UX while preserving the same backend permissions and architecture.

Availability boundaries:

- Setup, authentication, tenant/user/RBAC administration and audit have implemented services, OpenAPI routes and React screens.
- Jobs, attempts/logs, leases, retries/cancellation, session-cleanup scheduling and worker execution are implemented internally. There is no public job-management API or UI.
- Work records, outbox, attachments and activity have shared libraries, migrations, worker projections and integration tests. They have no public business-record/attachment routes or screens yet; object-specific business authorization must be supplied by their future domain consumers.
- Incident/SLA/request and later business capabilities remain roadmap work.

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
- graceful API, worker and scheduler shutdown,
- MySQL connection pool using `database/sql`,
- Redis connectivity,
- central transaction manager,
- explicit migrations,
- independent liveness and MySQL/schema/Redis readiness,
- local users and tenant memberships,
- Argon2id password hashing,
- opaque database-backed sessions,
- CSRF double-submit validation,
- Redis-backed login throttling,
- tenant bootstrap with initial platform administrator,
- first-run organization/administrator WebUI installer with durable completion state, CSRF/rate-limit protection and atomic RBAC/audit creation (migration 25),
- groups, roles, permission catalog and assignments,
- backend permission middleware,
- tenant-scoped user administration with cursor pagination/search, existing-global-account membership linking and membership-triggered session revocation,
- current-tenant administration with optimistic locking,
- role/group administration with cursor pagination/search,
- tenant-scoped read models for group members, direct user-role assignments and group-role assignments,
- RBAC assignment UI for add/remove membership and user/group roles with server refetch after every mutation,
- optimistic locking for role permission replacement,
- append-oriented audit persistence,
- tenant-scoped lookahead cursor audit API with validated date/actor/action/object/request filters,
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
- frontend API client with automatic CSRF handling, shared session expiry and rejection of responses from earlier logins,
- frontend permission-aware navigation and route guards,
- frontend responsive/loading/error/empty states,
- frontend API-client unit tests,
- Playwright browser E2E for the critical admin-to-least-privilege-user journey,
- E2E coverage for security-group membership, group-role assignment, audit visibility and real HTTP 403 enforcement,
- two-session E2E coverage for optimistic-lock conflicts, retained local proposals and explicit resolution,
- desktop Chromium and Pixel 7 Chromium projects with isolated test data,
- mobile-safe logout and permission-aware read-only tenant UI,
- URL-backed filters/tabs and bounded server search/pagination in user/RBAC/audit screens,
- separate role/group lists and details with paginated remote comboboxes, keyboard controls, response identity/cancellation and disabled stale assignment actions,
- administration diagnostics, Polish navigation/breadcrumbs and visible focus/mobile layouts,
- role permission editor using optimistic locking,
- CI workflow covering dependency manifest stability, backend format/vet/unit/race/build/migrations/integration/smoke/durable scheduler-worker/rollback, frontend typecheck/test/build/browser E2E, native lifecycle, Docker lifecycle and Engine installation/update tests.

## Currently in progress

Business-module APIs/UI, broader asynchronous operational monitoring and external identity providers remain roadmap work. Implemented shared libraries are available to domain consumers, rather than exposed as unfinished screens.

## Administration usability result

[PR #15](https://github.com/chmajster/OpenServiceNOW/pull/15) preserves the existing user creation/linking, membership enable/disable, role/group creation, permission editing and all assignment add/remove operations. Roles expose information, permissions and assignments; groups expose information, members and role assignments. Creation forms are explicit actions. Remote choices are independent of the current list page and retain their selection through searches/pagination.

Organization/role conflicts preserve the draft, retrieve the current version by object identity and require explicit resolution before saving. Failure to refetch a conflict or permission catalog is retryable without discarding the form. Mutations refetch durable assignments and refresh session context; protected 401 clears the whole application and older session responses cannot restore it. Readiness diagnostics are under administration.

Audit uses session tenant scope with parameterized inclusive date/exact actor/action/object/request filters and bounded cursors. New permission changes store before/after snapshots under the same transaction/role lock. Historical audit rows without old values display only the data they originally recorded. No migration was added; schema version remains 25.

Local Go unit/HTTP/OpenAPI tests, nine frontend unit tests, TypeScript/build and the disposable MySQL integration/migration lifecycle passed. Browser verification passed 20/20 scenarios on desktop Chromium and Pixel 7, including real users beyond the first 50 results, delayed user/group responses, conflict-refetch failure, catalog retry, duplicate-name draft preservation, keyboard navigation, backend logout and context changes. Regression coverage also verifies that a post-write session refresh supersedes a pending earlier read and that removing one's RBAC-management permission refreshes navigation even when object refetch is forbidden. The final revision, all six CI job results and the end-to-end evidence are recorded in [PR #15](https://github.com/chmajster/OpenServiceNOW/pull/15).

## Native local installer result

[PR #14](https://github.com/chmajster/OpenServiceNOW/pull/14) extends the existing four-process runtime with read-only status during Docker outages, individual PID/log details, before/after startup-rollback diagnostics and safe PID reads when processes exit. Makefile includes native lifecycle and continuous scheduler commands.

On 2026-10-12, the expanded native suite passed against executable revision `0a49ac574a749a1584fb51cf9324310340aa05b2` in a disposable Docker-in-Docker environment with its own network, checkout, SQL volume and credentials. It verified all four detached processes; known scheduler job IDs correlated with a completed worker attempt, result and execution logs; actual expired-session deletion; duplicate prevention; dead background process visibility with successful synchronous authentication; status leaving SQL/runtime state unchanged; API fallback/proxy updates through port 8100; frontend port persistence; startup rollback; safe bootstrap with an incorrect old password; and preservation of configuration, tenant data, password hash and active session across ordinary uninstall/reinstall. The host's existing application was left running.

`--status` reports worker/scheduler process liveness, not processing throughput or queue lag. API/schema/Redis readiness remains independent of those processes. Processing proof belongs to the isolated test; status never creates a job. Current-branch CI execution and final verification are recorded in PR #14.

## Known problems

The September GitHub runner-allocation failures and early formatting/reserved-identifier failures are historical, fixed issues. [foundation-ci #600](https://github.com/chmajster/OpenServiceNOW/actions/runs/38189545233) on `70c6a319a964d6f120449a981d52367e6e50fa35` completed successfully with all six jobs, including native and Docker lifecycle verification. They are not current blockers.

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
- role permission optimistic-lock conflicts, no-op version stability and transactional before/after audit snapshots,
- explicit linking of an existing global account to a second tenant without password mutation,
- audit cursor pagination, combined date/actor/action/object/request filters, invalid-filter rejection and tenant isolation,
- safe bootstrap re-entry for existing users,
- bounded password-verifier saturation behavior,
- panic-safe transaction behavior,
- precision-safe BIGINT JSON serialization above JavaScript `Number.MAX_SAFE_INTEGER`,
- quoted optimistic-lock version decoding in tenant/RBAC HTTP transports,
- Redis rate-limiter backend failure returning a stable 503 without entering password authentication,
- RBAC assignment reads and user-bearing writes requiring the additional user-management permission where user identities are exposed or changed,
- negative privilege-escalation coverage for direct system-role assignment, system-role group assignment, privileged-group membership changes and dormant administrator reactivation,
- group membership and user/group role mutation → repository readback cycles with cross-tenant rejection,
- single-role/group HTTP reads with authentication, permissions, not-found/invalid-ID handling, session tenant scope and decimal-string serialization,
- late object/session response rejection, backend logout, relogin and current-organization/own-permission context updates,
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

[foundation-ci #606](https://github.com/chmajster/OpenServiceNOW/actions/runs/38237188306) passed all six jobs on executable revision `0a49ac574a749a1584fb51cf9324310340aa05b2`: backend format/vet/unit/race/build, all 25 migrations/integration/rollback and auth/RBAC smoke flow; frontend typecheck/unit/build; desktop/mobile E2E; expanded native lifecycle with known-ID scheduler/worker proof; Docker lifecycle/browser workflow; and Engine installation/update. Local shell syntax, ShellCheck, installer help/unit tests and both Compose configurations also passed. That native-installer stage required no migration or API/UI change; the administration changes and their separate verification are described above.

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
- `000024_job_logs`,
- `000025_installation_state`.

Each up/down migration contains one SQL statement. Runtime readiness requires migration version 25, `dirty=false`, and the foundation metadata singleton. The real MySQL lifecycle test passed latest → one-step rollback → latest → full rollback → latest → readiness. Migration 25 permanently closes first-run setup for existing accounts/organizations without changing credentials. The reserved `groups` identifier is quoted in schema and repository SQL.

## Technical debt

Known non-blocking design work:

- session cleanup scheduler deduplicates through time-bucket job keys and a unique MySQL index; broader multi-job scheduler coordination will evolve with future workloads,
- installer background status reports verified process liveness; durable heartbeats, queue-lag monitoring and public job administration are future operational work,
- durable job processing and retry/outbox behavior are implemented; broader workload coverage evolves with roadmap features,
- frontend/API container packaging is implemented; production TLS/ingress and operational deployment policy remain environment-specific,
- external authentication providers are not implemented yet.

No fake endpoint, placeholder UI or empty worker executable is used to hide these gaps.

## Next recommended tasks

1. Complete roadmap capabilities with their API/UI and tenant/RBAC guarantees.
2. Extend business-workload, retry/cancellation and external-provider verification as those features are introduced.
3. Configure TLS ingress, secure cookies, datastore policy, backups and monitoring for a production deployment.
4. Keep full backend/frontend/integration/browser CI passing for future changes.
