# Current Project Status

Last updated: 2026-10-13

## Current phase

Durable workflow and approvals for catalog requests, following service catalog fulfillment and incident SLA.

The repository includes the administration foundation, durable MySQL jobs/worker, scheduler, work-record infrastructure and the core Incident Management API/UI. The full application is deployed through DevBox with real MySQL/Redis readiness. This delivery does not declare every Phase 3 extension or later roadmap phase complete.

## Workflow and approvals delivery (PR #22)

Schema 64–71 implements immutable published definitions, nine validated steps, MySQL executions/decisions/attempts, user/group ANY/ALL approval with explicit self policy and frozen/current membership checks, real scheduler/worker jobs, transactional request commands and idempotent task creation. Workflow catalog submission creates an execution without direct tasks and pins its version. Direct state changes cannot bypass approval. The form editor, inbox, visual state, diagnostics/resume/cancel and authorized in-app/SMTP integration are implemented. Parallel/join, subprocesses, scripts, delegation and multi-item workflow submissions remain outside this scope; manual multi-item requests remain supported.

Go/OpenAPI/unit/vet/race/build, disposable MySQL integration and migration rollback/reupgrade through 71, TypeScript/build and 25 frontend tests passed. Integration covers concurrent decisions/jobs, deadline races, ANY/ALL, self DENY/ALLOW, frozen/current membership, tenant/object isolation, atomic rollback, late-worker task completion, timers/conditions, repair and independent projections. All 34 existing desktop/mobile regression scenarios passed; the latest four scoped setup/workflow scenarios also passed on both viewports, including keyboard identity selection, publication/decision conflicts, pinned historical version, one real task, fulfillment, in-app and acknowledged SMTP delivery, access-revocation diagnostics/resume/cancel. The earlier local combined run used a superseded repair fixture in one desktop case; the fresh scoped run passed that corrected fixture. An actual API/worker/scheduler restart during approval retained the saved session, number, version and deadline; repeating the decision created exactly one task. Screenshots were reviewed for desktop/mobile layout. Current full six-job CI and final review evidence are maintained in [PR #22](https://github.com/chmajster/OpenServiceNOW/pull/22).

No historical requests are enrolled; upgrades/restarts preserve credentials. Migration 71 downgrade requires explicit export/removal of incompatible workflow notifications. Approval decisions at/after their deadline time out; task completion at/before its deadline counts as timely even when processing is delayed. Process cancellation does not automatically cancel the separate request or tasks.

## Historical service catalog and request fulfillment delivery

[PR #21](https://github.com/chmajster/OpenServiceNOW/pull/21) adds `internal/catalog` and `internal/requests`, schema 55–63, separate catalog/category/item/publication and Request/Requested Item/Task models, immutable declarative forms and snapshots, explicit active-group manual fulfillment, server visibility/required validation, atomic shared numbering/assignment/audit/outbox/idempotency and CAS transitions. Requesters can browse authorized publications, submit and track their own requests, communicate, attach files and cancel before work starts. Scoped group operators assign/start/hold/resume/complete tasks and finalize/close requests only after required work. The portal does not expose drafts, internal instructions/results or private communication/files/history. Templates require explicit administrative creation/configuration/publication; migrations seed no services or requests.

Local Go/OpenAPI unit/race/vet/build, TypeScript/build and 23 frontend unit tests passed. Disposable MySQL integration and migration rollback/reupgrade through 63 passed; coverage includes controlled outbox failure rollback, concurrent replay/digest conflicts, historical schema retention, required-task closure, optional-task cancellation, version conflicts, organization/object isolation, active group access, shared messages/files and idempotent real worker projections. Six scoped desktop/mobile setup/catalog/request/conflict scenarios passed with real MySQL/API/frontend/worker/scheduler, all six field types, conditions, IDs above 2^53−1, lost committed response/retry, changed publication/history, scoped assignment/fulfillment and private note/file exclusion. Actual API/worker restart preserved request numbers/versions, historical forms, activity, file bytes and the saved session. Full regression and final CI/review evidence are recorded in PR #21. The expanded desktop navigation scrolls to keep every action, including logout, reachable.

Boundaries: API submission has 1–10 items; portal submits one service per request. Definitions have at most 50 fields/20 independent manual tasks. There are no approval/dependency engines, external provisioning actions, request SLA or request notification channels in this stage. Closed/cancelled requests are terminal. Publication/fulfillment data remain immutable; optional unfinished tasks are explicitly cancelled on finalization. Prior stage CI evidence below describes its historical revision.

## Incident SLA delivery

[PR #20](https://github.com/chmajster/OpenServiceNOW/pull/20) implements `internal/sla`, migrations 49–54, immutable audited policy/calendar versions, deterministic organization/priority/category selection, first-response events, durable current and historical resolution cycles, exact-reason pauses, microsecond accrual and business-time deadlines. Worker jobs consume bounded source batches in incident-version/time order and the scheduler autonomously checks due work. Warning/breach events have actual activity/notification consumers with atomic per-threshold idempotency. Authenticated administration, filtered incident batch summaries/history and React policy/calendar/list/detail views are implemented together.

Local verification passed calendar weekend/holiday/night, Warsaw spring/autumn DST, repeated pauses, delayed resolution, precision/restart and deterministic selection tests; Go unit/race/vet/build; disposable MySQL integration and complete migration rollback/reupgrade through 54; frontend TypeScript/build and 22 unit tests. The real MySQL test covers reversed source IDs/versions and handler arrival, retained policy history, idempotent thresholds plus four actual in-app effects, definition/request conflicts, object RBAC/tenant isolation, a fresh service checkpoint and authenticated read-only SLA routes. Four scoped desktop/mobile setup/SLA scenarios passed with real API/frontend/worker/scheduler, policy conflict resolution, private/reporter exclusion from first response, exact-reason pause/resume, pinned calendar versions, BIGINT IDs, and a real autonomous deadline crossing while both incident browsers were closed. Actual API/worker restart preserved instance IDs/deadlines/thresholds, definitions/calendar versions, SLA history and the saved session. The full 30-scenario regression, current six-job CI, final review and merge evidence are recorded in PR #20; earlier CI results below describe their historical revisions.

Boundaries: no historical enrollment, no rebinding active policy/calendar, no generic escalation workflow. Policy priority/category freeze at creation; reopening resets only resolution with the original versions. Response without a qualifying public operator message is UNANSWERED. The scheduler checks at most 100 candidates every at most 30 seconds; a job applies 200 transitions and durably continues. Definitions are limited to 100 per kind and calendar search to ten years. Snapshots expose last calculation time; reads never execute timers. Warning/breach notifications follow state-change preferences and current incident permissions; SMTP retains its existing ambiguity. The schema upgrade requires stopped old runtimes, and does not alter credentials.

## Earlier deliveries

Phase 0, the administration foundation, shared work-platform libraries and both local installer modes are delivered through PR #14. DevBox deployment was delivered in PR #7. PR #15 delivered the administration UX rebuild; all six jobs in [foundation-ci #616](https://github.com/chmajster/OpenServiceNOW/actions/runs/38246614044) passed. Own-account implementation and local verification are recorded in [PR #16](https://github.com/chmajster/OpenServiceNOW/pull/16), which also tracks final CI and merge evidence.

Availability boundaries:

- Setup, authentication, own-account profile/password/sessions, tenant/user/RBAC administration and audit have implemented services, OpenAPI routes and React screens.
- Jobs, attempts/logs, leases, retries/cancellation, session-cleanup scheduling and worker execution are implemented internally. There is no public job-management API or UI.
- Work records, outbox, attachments and activity have shared libraries, migrations, worker projections and integration tests. Incidents now consume numbering, assignments and activity with domain object authorization; generic work/job administration remains unavailable. Incident-bound communication/file/observer/relation APIs and UI are implemented with concrete object authorization.
- Incident Management and incident SLA have persistence, production workers, OpenAPI and functioning React screens. Catalog/request fulfillment now has domain persistence, actual task/assignment/activity processing, authenticated OpenAPI routes and administration/portal/operator UI. Limited workflow approvals now have persistence, production jobs, authenticated OpenAPI and functioning React screens; broader business capabilities remain roadmap work.

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

CMDB, other business modules, editable notification templates/webhooks/inbound mail, broader operational monitoring and external identity providers remain roadmap work. Independent consumers and the incident notification subset are implemented and locally verified in PR #19; final CI/review/merge evidence is maintained in that PR. Shared libraries are consumed through domain APIs rather than exposed as unfinished screens.

## Notifications result

[PR #19](https://github.com/chmajster/OpenServiceNOW/pull/19) implements unique event/consumer deliveries, atomic split, independent leases/retries and preserved activity idempotency. Migrations 38–48 explicitly exclude legacy events from notifications. In-app lists/counts/read actions and SMTP attempts recheck current organization/object/private-note access. Notification materialization and SMTP jobs share a MySQL transaction; SMTP failure does not block activity or in-app. SMTP uses optional configuration, tenant-bound AES-256-GCM credentials with an external key, safe DTOs/errors, organization-specific user preferences, real delivery/attempt history and controlled administrative tests/retries. React exposes all these capabilities with decimal IDs, URL pagination/tabs, retained conflict proposals and no browser secret persistence.

Verification passed Go unit/race/vet/build, OpenAPI, frontend typecheck/build and 19 frontend unit tests, disposable MySQL integration and migrations 1–48 with legacy cutoff and full rollback/re-upgrade, real SMTP DATA acceptance and lost-response handling. The existing 26 desktop/mobile regression scenarios passed; after fixing notification tab freshness and table overflow, an additional 6/6 scoped setup/incident/notification browser scenarios passed on fresh data with real Mailpit and IDs above 2^53−1. Actual API/worker restart preserved notification IDs/read states, preferences, SMTP configuration, delivery outcomes/attempt counts, incident history and saved login. Additional desktop/mobile inbox/configuration/queue checks after restart passed without page overflow. The latest six-job CI, full 28-scenario regression, final review and merge evidence are recorded in PR #19; earlier CI results apply only to their historical revisions. SMTP external delivery permits duplicates after accepted DATA and lost acknowledgement. Editable templates, webhooks/inbound mail and secret rotation tooling are not delivered. Local Mailpit needs no external account; in-app needs no SMTP configuration/key. Upgrade stops old API/workers during migration; mixed dispatcher versions are unsupported.

## Incident collaboration result

[PR #18](https://github.com/chmajster/OpenServiceNOW/pull/18) adds separate public replies/internal notes, private filtering in shared message/activity/file repositories before pagination, incident-bound file authorization and safe metadata DTOs. File visibility is inherited from a linked message or explicit; note bodies and private file names are excluded from requester lists, history payloads and pending indicators. Optional assignment, actor and source-event BIGINT identifiers remain decimal strings.

API/file metadata/audit/outbox writes share a transaction. Message/upload keys reconcile lost responses without duplicates; different content conflicts. Existing work-event production handlers project author/business-time/version metadata idempotently. History can be pending after a confirmed durable write. Observers do not grant access; related/parent-child records authorize both ends, cap lists at 100 and reject cycles/multiple parents. Operator batches cap at 50 and provide per-record permission/version/results for assign-to-self and start-work actions.

Native storage uses `ATTACHMENT_ROOT`; Docker/DevBox services share an external persistent named attachment volume. Uploads enforce 10 MiB per file and 11 MiB multipart in API/proxy, sniff MIME, validate names and require authorized download. Deletion blocks download immediately; hourly durable worker cleanup sweeps managed orphan/temp/deleted files after a 24-hour grace period. Ordinary uninstall preserves storage; native/Docker locations need explicit file migration when switching modes. The Docker lifecycle suite verifies real authenticated bytes after API replacement, image rebuild and uninstall/reinstall, plus both upload size limits.

Local verification passed Go unit/race/vet/build, OpenAPI, ShellCheck/installer checks, 17 frontend unit tests/typecheck/build and disposable MySQL with all 37 migrations and rollback/re-upgrade. MySQL verifies cross-organization/private resource isolation, atomic SQL-failure rollback, one-connection transaction reuse, idempotency, worker replay, absent activity authors, removal of inactive observers, storage reconciliation and per-record batch outcomes. A full 26/26 desktop/mobile browser regression used actual SQL identifiers above 2^53−1 across users/groups/sessions/incidents/files/events and preserved records/history/login after restarting real API/worker processes. Additional 8/8 scoped desktop/mobile scenarios verified independent reply/note drafts and private file defaults. The isolated Docker lifecycle passed authenticated 3.25 MB upload/download, 10/11 MiB rejection, API replacement, image rebuild and ordinary uninstall/reinstall with unchanged file bytes and saved session. Final six-job CI, review and merge evidence are tracked in PR #18. That PR did not deliver an external notification provider; PR #19 adds optional SMTP. No object-store adapter, SLA/CMDB source or message editing is claimed; communication is append-only and drafts are memory-only.

## Incident Management result

[PR #17](https://github.com/chmajster/OpenServiceNOW/pull/17) delivers the core incident model, fixed category/subcategory catalog, explicit 3×3 priority matrix, separate audited priority override and backend-controlled NEW/IN_PROGRESS/ON_HOLD/RESOLVED/CLOSED/CANCELLED lifecycle. Holding/cancelling/reopening reasons, resolution code/description and active assignments are enforced in the API. Reopening emits a distinct event and preserves prior resolution/closure values in history.

Session-derived organization/actor, object read/read-own checks, separate action permissions and active member/group validation apply on the backend. Lists and lookups are bounded/server-filtered; newest/oldest/priority sorting uses stable keyset order. Summaries omit large descriptions. Identifiers, cursors and versions are decimal strings; pagination does not promise a frozen snapshot during concurrent edits.

Shared numbering/assignment services accept the domain transaction without duplicated SQL. Writes persist incident, required assignment row, audit and outbox atomically. Actor/organization-scoped creation keys and body digests deduplicate concurrent retries and reject changed bodies; versioned updates prevent lost changes. Production worker handlers project all nine incident event types idempotently, preserving mutation-time before/after values, version and business occurrence time. History follows source-event order and reports pending projection independently of synchronous record persistence/readiness.

React exposes list/create/details, **Moje**, **Mojej grupy**, **Nieprzypisane**, assignment, lifecycle, priority and history. Paginated choices require domain access rather than user-administration permission. Responses are object-bound/cancelled; dirty forms retain their base version across refresh and require explicit conflict decisions. Lost creation responses reuse their in-memory body/key; unfinished drafts are not persisted across closing/reloading the form.

Final UI review also retains dirty forms when concurrent closure removes write eligibility; the local proposal stays visible and saving is disabled until the operation is permitted. Additional desktop/mobile scenarios passed for that decision, priority override permission/reason and matrix restoration. A real API/worker process restart preserved both UI-created closed incidents and their complete numbers/versions/assignments/history; the saved login continued to work.

Go unit/race/vet/build, disposable MySQL integration and migrations 1–32, 15 frontend unit tests and typecheck/build passed. Full browser regression passed 24/24 desktop/mobile scenarios, including lost response after a real committed incident write, agent beyond the first directory page, keyboard actions, own-record restrictions, rejected API transition, retained conflict draft, reopen, real worker history and refresh persistence. MySQL tests verify all nine event types, source-order pagination after out-of-order delivery and retry deduplication. The native lifecycle CI suite checks incident/number/assignment/history persistence across restarts, port changes and ordinary uninstall/reinstall. Final CI, review and merge evidence are tracked in PR #17.

At PR #17 completion, collaboration was a subsequent stage; PR #18 delivered replies/notes/files, related/parent-child records and two operator batch actions. At that revision SLA was future work; PR #20 implements incident SLA. CMDB/service relations, configurable catalogs, major workflows and broader automation remain subsequent stages.

## Administration usability result

[PR #15](https://github.com/chmajster/OpenServiceNOW/pull/15) preserves the existing user creation/linking, membership enable/disable, role/group creation, permission editing and all assignment add/remove operations. Roles expose information, permissions and assignments; groups expose information, members and role assignments. Creation forms are explicit actions. Remote choices are independent of the current list page and retain their selection through searches/pagination.

Organization/role conflicts preserve the draft, retrieve the current version by object identity and require explicit resolution before saving. Failure to refetch a conflict or permission catalog is retryable without discarding the form. Mutations refetch durable assignments and refresh session context; protected 401 clears the whole application and older session responses cannot restore it. Readiness diagnostics are under administration.

Audit uses session tenant scope with parameterized inclusive date/exact actor/action/object/request filters and bounded cursors. New permission changes store before/after snapshots under the same transaction/role lock. Historical audit rows without old values display only the data they originally recorded. PR #15 added no migration and was verified at schema version 25; own-account changes extend the current schema to 27.

Local Go unit/HTTP/OpenAPI tests, nine frontend unit tests, TypeScript/build and the disposable MySQL integration/migration lifecycle passed. Browser verification passed 20/20 scenarios on desktop Chromium and Pixel 7, including real users beyond the first 50 results, delayed user/group responses, conflict-refetch failure, catalog retry, duplicate-name draft preservation, keyboard navigation, backend logout and context changes. Regression coverage also verifies that a post-write session refresh supersedes a pending earlier read and that removing one's RBAC-management permission refreshes navigation even when object refetch is forbidden. The final revision, all six CI job results and the end-to-end evidence are recorded in [PR #15](https://github.com/chmajster/OpenServiceNOW/pull/15).

## Own-account result

**Moje konto** exposes versioned global display-name editing, password change with the current password, and bounded cursor-paginated active-session metadata across the user's organizations. Current sessions are marked; users can revoke one session or all others. HTTP handlers derive the owner from the backend session, require CSRF for writes, reject foreign session IDs and expose no tokens in JSON. Organization administrators retain organization-scoped membership/session controls without gaining global password/session authority.

Password changes share the bounded Argon2 pool and global-account/IP attempt limits, rotate the current session/CSRF hashes in place, revoke other sessions, increment credential/profile versions and audit in one transaction. Login rechecks the credential version under the global user lock, so an old password verified before a committed change cannot create a new session. Competing changes with one snapshot commit at most once. A stale logout cannot revoke the rotated session. SQL COMMIT acknowledgement failure returns `ACCOUNT_RESULT_UNKNOWN`; the UI explains reconciliation and never blindly retries the write. A confirmed change followed by a failed context read is reported as a confirmed password change.

New administrator-created accounts and new default-password installation accounts require a password change. Until then authenticated API access is limited to minimal `/me`, password change and logout. Custom first-run passwords are permanent; headless bootstrap can explicitly mark a new custom account temporary. Existing hashes and flags survive migrations, re-bootstrap and restart, including Unicode passwords accepted under the earlier byte-based policy. New passwords use the same 12-code-point/1024-UTF-8-byte policy in API and UI. Email changes, MFA, LDAP and OIDC remain outside the delivered scope.

Local verification passed Go unit/HTTP/OpenAPI tests, race tests, vet/build, all 27 migrations and full rollback/re-upgrade on disposable MySQL 8.4, account/RBAC/tenant integration, 13 Vitest tests, TypeScript/build and 22/22 Playwright scenarios on desktop Chromium and Pixel 7. Coverage includes failed current password, transport failure with retained input, profile conflict decisions, real token rotation/revocation, forced-change API rejection, foreign-session rejection, tenant-scoped membership revocation, transactional audit rollback, old verified credential fencing, two concurrent password changes, session restoration with a new service/SQL pool, and preservation of legacy bootstrap credentials. Final CI results and merge evidence are maintained in PR #16; historical runner failures are not current implementation findings.

## Native local installer result

[PR #14](https://github.com/chmajster/OpenServiceNOW/pull/14) extends the existing four-process runtime with read-only status during Docker outages, individual PID/log details, before/after startup-rollback diagnostics and safe PID reads when processes exit. Makefile includes native lifecycle and continuous scheduler commands.

On 2026-10-12, the expanded native suite passed against executable revision `0a49ac574a749a1584fb51cf9324310340aa05b2` in a disposable Docker-in-Docker environment with its own network, checkout, SQL volume and credentials. It verified all four detached processes; known scheduler job IDs correlated with a completed worker attempt, result and execution logs; actual expired-session deletion; duplicate prevention; dead background process visibility with successful synchronous authentication; status leaving SQL/runtime state unchanged; API fallback/proxy updates through port 8100; frontend port persistence; startup rollback; safe bootstrap with an incorrect old password; and preservation of configuration, tenant data, password hash and active session across ordinary uninstall/reinstall. The host's existing application was left running.

`--status` reports worker/scheduler process liveness, not processing throughput or queue lag. API/schema/Redis readiness remains independent of those processes. Processing proof belongs to the isolated test; status never creates a job. Current-branch CI execution and final verification are recorded in PR #14.

## One-line remote installer result

[PR #25](https://github.com/chmajster/OpenServiceNOW/pull/25) adds an HTTPS bootstrap for fresh installation and subsequent updates from the expected GitHub `main` branch. It installs into `/opt/OpenServiceNow`, defaults to the complete containerized runtime, and delegates Docker package management, builds, migrations, startup rollback and readiness verification to the existing `--local-docker` lifecycle.

The source synchronizer runs as the ordinary installation user, requests `sudo` only for missing apt packages and initial `/opt` directory creation, and accepts updates only when the target is a clean checkout on the expected branch and origin. It acquires the application's lifecycle lock before fetch plus an ancestor check and fast-forward merge, then hands that lock to the installed script; concurrent lifecycle/build operations, local edits, untracked source files, divergent commits, changed origins, non-empty foreign directories and symbolic-link targets are rejected without replacement. Ignored `.env`, runtime state, attachments and named volumes remain outside Git updates and are preserved by ordinary application updates.

Local shell verification covers piped execution/help, real Git clone and fast-forward update, installation paths containing spaces, expected argument forwarding and all rejection cases above. The foundation workflow validates syntax and ShellCheck before running the bootstrap regression suite and the existing native/Docker lifecycle suites.

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
- `000025_installation_state`,
- `000026_user_credentials`,
- `000027_account_sessions`,
- `000028_group_availability`,
- `000029_incidents`,
- `000030_incident_requests`,
- `000031_incident_permissions`,
- `000032_activity_event_order`,
- `000033_attachment_access`,
- `000034_activity_access`,
- `000035_activity_access_backfill`,
- `000036_incident_collaboration_requests`,
- `000037_incident_collaboration_permissions`,
- `000038_event_notification_policy`,
- `000039_event_notification_default`,
- `000040_event_deliveries`,
- `000041_event_delivery_backfill`,
- `000042_event_legacy_distribution`,
- `000043_notification_preferences`,
- `000044_notification_smtp_config`,
- `000045_notifications`,
- `000046_notification_mail_deliveries`,
- `000047_notification_mail_attempts`,
- `000048_notification_permission`,
- `000049_sla_definitions`,
- `000050_sla_definition_versions`,
- `000051_sla_instances`,
- `000052_sla_work`,
- `000053_sla_history`,
- `000054_sla_permission`,
- `000055_service_catalogs`,
- `000056_catalog_categories`,
- `000057_catalog_items`,
- `000058_catalog_item_versions`,
- `000059_service_requests`,
- `000060_requested_items`,
- `000061_fulfillment_tasks`,
- `000062_work_operation_keys`,
- `000063_request_permissions`,
- `000064_workflow_definitions`,
- `000065_workflow_versions`,
- `000066_workflow_executions`,
- `000067_workflow_steps`,
- `000068_workflow_decisions`,
- `000069_workflow_attempts`,
- `000070_workflow_permissions`,
- `000071_workflow_notifications`.

Each up/down migration contains one SQL statement. Runtime readiness requires migration version 71, `dirty=false`, and the foundation metadata singleton. The real MySQL lifecycle test passed latest → one-step rollback → latest → legacy schema 24 → latest → full rollback → latest → readiness. Migration 25 permanently closes first-run setup for existing accounts/organizations without changing credentials; migration 26 preserves existing hashes and defaults the required-change flag to false. The reserved `groups` identifier is quoted in schema and repository SQL.

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
