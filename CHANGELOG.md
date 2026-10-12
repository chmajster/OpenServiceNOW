# Changelog

All notable repository changes are documented here.

The project is pre-release and currently in architecture/foundation development.

## 2026-10-12 — Local Docker installer and updates

- Added `--local-docker` to install/update apt-managed Docker Engine/Compose/Buildx, build current sources with refreshed image bases, migrate and run the full application in containers without host Go/Node.
- Added health-checked lifecycle commands, loopback WebUI port fallback through 8100, safe mode switching with shared SQL data/credentials, explicit bootstrap and build/startup failure handling.
- Added isolated Ubuntu/Debian package installation/update verification and complete Docker lifecycle/browser CI coverage.

## 2026-10-12 — WSL startup and background frontend

- Documented ordinary-user Docker access, refreshing Linux/WSL group membership, and configuring free MySQL/Redis ports while preserving other applications and database data.
- Start local components in independent sessions so closing the installer's terminal cannot stop them; run Vite in non-interactive CI mode to disable stdin shutdown handling. Lifecycle verification removes the caller's CI setting and checks process session/terminal detachment.

## 2026-10-12 — Automatic local API port selection

- Local installation tries API ports 8080 through 8100 inclusive, skips occupied/reserved ports and reports exhaustion without stopping foreign listeners.
- Persists the selected API port for lifecycle commands and status, overrides the API's bind port without changing `.env`, and updates both Vite proxies when the effective port changes.
- Added full-runtime coverage for consecutive conflicts, port 8100, complete exhaustion, saved-port conflicts with a live frontend, and browser E2E on an alternate API port.

## 2026-10-12 — Complete local runtime installer

- Added explicit `--local` install/start/stop/restart/status modes using the current checkout, with strict argument/port validation and persisted frontend port.
- Built and managed API, worker, scheduler and Vite as ordinary-user background processes with separate logs, PID/start-time records, checkout ownership checks and a lifecycle lock.
- Reinstallation rebuilds local source and replaces old processes; failed startup rolls back only components started by the failed call.
- Infrastructure Compose remains separate from DevBox, supports local credentials/ports and validates container ownership while preserving existing SQL volumes.
- Added installer process/security tests and isolated full-runtime lifecycle verification in CI; documented Linux/WSL commands and operational state.

## 2026-10-11 — First-run WebUI installer

- Added a responsive organization/administrator setup wizard with password confirmation, validation, retry feedback and prefilled login after completion.
- Added a same-origin, CSRF-protected, Redis-rate-limited setup API with bounded password hashing and atomic account, membership, RBAC, audit and completion-state writes.
- Migration 25 permanently closes setup for existing installations without changing account credentials; row locking prevents concurrent first-admin creation.
- Local installer and container startup now defer account creation to the WebUI by default. Explicit `--bootstrap` / `BOOTSTRAP_ON_START=true` and the `admin` / `admin` option remain supported.
- Added API security/validation tests, real MySQL rollback/concurrency/upgrade coverage and desktop/mobile first-run browser verification.

## 2026-10-11 — Default administrator credentials

- Fresh installations default to tenant `default`, login `admin` and password `admin`; explicit bootstrap environment values remain supported.
- The administrator login alias resolves to `admin@example.test` and shares its authentication rate limit; existing email login remains compatible.
- Local installer and container startup use the same bootstrap defaults. Re-bootstrap verifies and preserves existing account passwords.
- Enforced LF line endings for `.env.example` so Bash can load fresh installer configuration from Windows checkouts.
- Login accepts the administrator alias and short default password; ordinary user creation retains the 12-byte password policy.
- Added bootstrap, password-policy, rate-limit, real MySQL persistence and desktop/mobile login regression coverage.

## 2026-10-11 — DevBox deployment

- Added a complete frontend/API/worker/scheduler/Redis Compose stack and non-root multi-stage Go/React images.
- Integrated runtime DevBox MySQL bindings, migrations and idempotent administrator bootstrap without storing credentials in source files or image layers.
- Added the same-origin Nginx proxy and dependency readiness checks; preserved the infrastructure-only local installer workflow.
- Quoted the MySQL 8 reserved `groups` table identifier in migrations and repository queries after real deployment exposed the failure.
- Normalized Go formatting and enforced Linux line endings; fixed shell lint findings and upgraded source-map-js to the patched 1.2.2 release.
- Corrected browser test selectors and isolated audit assertions; the suite respects the real login limiter and passes all six desktop/mobile journeys against MySQL/Redis.
- Verified the DevBox rebuild, login/CSRF/readiness, non-root source RW mounts, and SQL queries from API, worker and scheduler; documented the deployment and all 24 migrations.

## 2026-09-29 — Local installer

### Added

- executable repository-level `install.sh` for the current Go/React/MySQL/Redis stack,
- preflight checks for Docker Compose v2, Go, Node.js, npm and curl,
- idempotent environment creation with generated bootstrap administrator password when missing,
- MySQL/Redis startup and health waiting, migrations, bootstrap, dependency installation and backend/frontend builds,
- start, stop, restart, status, no-start and uninstall modes with explicit data purge,
- PID/log state under ignored `.opensnow/` and CI syntax/help validation.

### Safety

- ordinary uninstall preserves `.env` and MySQL data,
- PID shutdown verifies the recorded process before sending signals,
- the installer rejects API ports incompatible with the current Vite proxy instead of producing a broken frontend/API path.

## 2026-09-27 — Phase 1 protected system-role hardening

### Security

- system-role assignment/removal now requires the caller to already be an effective tenant `platform_admin`,
- adding or removing members of a group carrying a system role requires effective platform-administrator authority,
- privilege-sensitive RBAC mutations serialize on the tenant row so group-role and group-membership changes cannot race around the authorization decision,
- activating or deactivating a tenant membership with a direct/group system-role binding requires effective platform-administrator authority, closing dormant-role reactivation paths,
- user-bearing RBAC writes now require both `admin.rbac.manage` and `admin.users.manage`,
- authorization failures use a stable HTTP 403 `PLATFORM_ADMIN_REQUIRED` response.

### Tests and contract

- added negative HTTP authorization coverage for user-bearing RBAC writes,
- added MySQL integration coverage for direct system-role escalation, system-role group assignment, privileged-group membership changes and dormant privileged-membership reactivation,
- OpenAPI now documents dual static permission requirements and conditional platform-administrator authorization,
- OpenAPI contract tests pin the dual-permission requirement for user-bearing RBAC writes.

### Verification status

- the change remains on a Draft PR; latest run #549 (`36293613157`) again failed before any job step executed,
- the upstream Phase 1 runner-allocation blocker and missing `go.sum` / `web/package-lock.json` remain unresolved,
- no build/test success is claimed from static review alone.

## 2026-09-27 — Local Phase 1 dependency and build verification

- Resolved and committed `go.sum`, updated `go.mod` with required indirect modules, and committed `web/package-lock.json` from npm resolution; CI now requires `npm ci`.
- Applied `gofmt` to 14 Go files; fixed an unused TypeScript import and restricted Vitest discovery to frontend unit tests instead of Playwright E2E.
- Local Go 1.27.1 module verification, vet, unit tests, race tests and VCS-stamping-disabled build pass; local frontend `npm ci`, typecheck, three unit tests and production build pass.
- MySQL/Redis integration, migrations, live API, browser E2E, PR #5 security fix and GitHub CI verification remain open. The last checked PR #3 workflow run #523 failed before runner allocation.

## 2026-09-26 — Phase 1 RBAC assignment and verification hardening

### Added

- tenant-scoped cursor reads for security-group members, direct user-role assignments and group-role assignments,
- end-to-end RBAC assignment UI for adding/removing group members and user/group roles with API refetch after each mutation,
- OpenAPI permission metadata for all administrative operations and a contract test that prevents undocumented permission requirements,
- reusable migration runner plus MySQL integration coverage for latest → one-step rollback → re-upgrade → full rollback → re-upgrade → schema readiness,
- Redis rate-limiter outage HTTP coverage,
- Playwright coverage for security-group membership, group-role assignment, audit events, stale optimistic-lock conflicts and mobile Chromium.

### Fixed

- tenant and role optimistic-lock conflict messages now survive the automatic data refetch instead of being cleared immediately,
- RBAC reads that expose user details additionally require `admin.users.manage`,
- E2E identities are isolated per browser project/retry to prevent false duplicate conflicts.

### Verification status

- latest checked workflow run #511 (`36271136402`) still failed before checkout for all three jobs: `runner_id=0`, empty runner name/group and zero executed steps,
- `go.sum` and `web/package-lock.json` are still absent and must be produced by real dependency resolution,
- no backend, migration, frontend or Playwright success is claimed for the new commits until those commands actually execute.

## 2026-09-26 — Phase 1 CI allocation hardening and Phase 2 sequencing

### Changed

- switched GitHub-hosted jobs from a fixed `ubuntu-24.04` label to `ubuntu-latest` while isolating runner-allocation failures,
- removed duplicate feature-branch CI caused by simultaneous `push` and `pull_request` triggers,
- added workflow concurrency so superseded PR runs are cancelled instead of consuming runner capacity,
- expanded Phase 2 so durable jobs/worker, transactional outbox/events, attachments and the unified activity timeline are explicit prerequisites for Incident Management.

### Verification status

- run #474 on `ubuntu-latest` and run #475 after duplicate-trigger removal both failed before runner assignment with `runner_id=0`, an empty runner name and zero steps,
- no dependency-resolution, build, test, migration or browser step executed in those runs,
- GitHub Status reported Actions operational during the latest reproduction,
- executable verification remains blocked by GitHub-hosted runner allocation/account or repository Actions policy/quota/billing; application-code success is not claimed.

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

## 2026-09-23 — Phase 1 runtime, pagination and authentication hardening

### Changed

- separated audit HTTP transport into `internal/auditapi` to remove the `identity -> audit -> identity` import cycle,
- added cursor pagination and bounded search for tenant users, roles and groups,
- changed audit pagination to `limit+1` lookahead,
- added optimistic locking to role permission replacement,
- made `platform_admin` dynamically inherit every permission in the live permission catalog,
- made tenant/user no-op updates and idempotent RBAC assignments avoid false audit churn,
- normalized request client addresses to valid canonical IPv4/IPv6 only,
- forced MySQL session timezone to UTC,
- added optional MySQL and Redis TLS configuration,
- bound development API/MySQL/Redis listeners to loopback by default,
- added full OpenAPI local-reference and operation-ID validation.

### Security

- added bounded concurrent Argon2 verification to cap authentication memory pressure,
- bounded stored Argon2 work factors before expensive verification,
- rejected trust-all proxy CIDRs and attacker-controlled all-trusted X-Forwarded-For chains,
- sanitized client-provided request IDs before logging/audit persistence,
- added controlled JSON panic recovery and panic-safe transaction rollback,
- required an existing user's current password before bootstrap can grant that identity administrator membership,
- reserved the `platform_admin` role name,
- returned explicit `LAST_PLATFORM_ADMIN` conflicts for operations that would remove the final effective administrator,
- enforced production secure cookies and bounded HTTP header size,
- added `Cache-Control: no-store` and restrictive response security headers.

### Verification status

Static checks performed through the GitHub branch confirm zero local package import cycles, no stale pre-pagination list signatures, no TODO/FIXME implementation markers, and an OpenAPI document with 16 paths / 23 unique operations / zero unresolved local references. Executable build/test verification remains blocked by GitHub Actions runner allocation and missing dependency lock files.

## 2026-09-23 — Phase 1 dependency, session and scheduler hardening

### Fixed

- corrected unpublished Go dependency pins to released versions: `go-chi/chi v5.3.1` and `go-sql-driver/mysql v1.10.0`,
- replaced invalid TypeScript project-reference build mode with explicit no-emit typechecks suitable for Vite,
- aligned `.env.example` development MySQL credentials with Docker Compose and made spaced values shell-source-safe,
- added bounded cursor pagination/search to tenant user lists and RBAC user lookup,
- made JSON request endpoints return explicit HTTP 415 for unsupported content types and 413 for oversized bodies.

### Security and reliability

- request correlation IDs are generated exclusively by the server; client-supplied request IDs are ignored,
- disabling tenant membership explicitly revokes all active sessions for that tenant/user in the same transaction,
- login throttle responses report the configured retry window,
- session TTL and authentication throttle values have upper bounds,
- scheduler cleanup is coordinated through a MySQL advisory lock so multiple replicas do not duplicate work,
- session cleanup indexes now match expired/revoked batch queries,
- API defaults and development datastore ports remain loopback-bound,
- CI preserves generated dependency lock artifacts and validates manifest stability.
