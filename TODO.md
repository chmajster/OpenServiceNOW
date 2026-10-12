# OpenServiceNOW TODO

Last reconciled: 2026-10-12

Legend: `[ ]` not delivered, `[-]` in progress, `[x]` implemented and verified for the stated scope. Completion of a shared library does not imply a public API or UI. Detailed business scope and exit criteria remain in ROADMAP.md; verification evidence is in STATUS.md.

## Architecture and administration foundation

[P2] [x] Define the Go/chi/database/sql/MySQL modular monolith and ADRs.

[P2] [x] Implement API, migration/bootstrap executables, validated configuration, structured logs, request/security controls and graceful shutdown.

[P2] [x] Implement MySQL/Redis connectivity, transactions and explicit migrations 1-25 with clean-schema readiness. CI #600 passed migration apply, MySQL integration and full rollback.

[P2] [x] Implement first-run WebUI setup with durable completion, atomic organization/admin/RBAC/audit creation, concurrency protection and safe upgrades.

[P2] [x] Implement local users/authentication, opaque SQL sessions, CSRF, rate limiting, bounded Argon2 checks and session-derived organization/actor.

[P2] [x] Implement tenant and user administration, bounded search/cursor pagination, optimistic locking and safe linking of existing global identities.

[P2] [x] Implement groups, roles, permissions and assignments, system-role safeguards and last-effective-administrator protection with transactional serialization.

[P2] [x] Implement tenant-scoped audit persistence/API and precision-safe decimal-string BIGINT contracts.

[P2] [x] Deliver setup/login/overview/tenant/users/RBAC/audit React screens, permission-aware navigation, errors/loading/empty states and desktop/mobile browser coverage.

[P2] [x] Generate and commit real Go/npm dependency lock files; CI #600 verified manifest stability and backend/frontend builds.

[P2] [x] Deliver the DevBox API/frontend/worker/scheduler stack with same-origin proxy, private Redis and application-scoped MySQL bindings.

## Shared work platform: implemented libraries and background runtime

[P2] [x] Implement work-record numbering, assignment, comments/work notes, watchers, relations, optimistic locking and bounded list contracts as shared services/repositories.

[P2] [x] Implement durable MySQL jobs, attempts/logs, claim leases, retry/cancellation primitives and bounded worker execution.

[P2] [x] Implement transactional outbox delivery and idempotent activity projections in the worker.

[P2] [x] Implement attachment metadata/storage, validation and tenant-safe persistence, plus shared activity pagination.

[P2] [x] Run the real session-cleanup workload through scheduler -> durable MySQL job -> worker. Scheduler buckets use unique job idempotency keys; this is no longer a direct advisory-lock cleanup runner.

[P1] [ ] Expose shared work capabilities through the relevant business-module APIs/UI with object-specific authorization. No public work-record/job-management/attachment routes or screens currently exist.

[P2] [ ] Add durable background health/heartbeat/queue-lag monitoring as operational requirements develop. A verified live process is not proof of active processing.

## Local installer lifecycle

[P2] [x] Native installer builds/starts/stops API, Vite, worker and scheduler with separate logs, detached sessions, PID ownership/start-time checks and lifecycle locking.

[P2] [x] Select/save an available API port in 8080-8100, update both Vite proxies and preserve the selected frontend port across restart/status.

[P2] [x] Preserve configuration, accounts and SQL data during restarts and ordinary uninstall; explicit re-bootstrap requires the existing password and never resets it.

[P2] [x] Deliver Docker installer lifecycle/update/rollback and Engine installation/update tests without changing DevBox orchestration.

[P2] [x] Strengthen native lifecycle verification and diagnostics in PR #14.

Implemented and verified in the isolated native lifecycle suite:

- read-only status without directory/lock creation; local process states still reported during Docker outage,
- per-process PID/log details and state before/after partial-start rollback,
- safe identity reads when a process exits during startup/status,
- known scheduler ID correlated with durable result, attempt owner and execution logs,
- an expired session removed by the actual worker handler,
- dead worker/scheduler visibility while API/authentication remain available,
- status leaves SQL and saved runtime state unchanged,
- custom administrator session/password/configuration survive port changes, restart, ordinary uninstall and reinstall,
- CI lifecycle output and per-process failure artifacts,
- Makefile lifecycle and continuous scheduler commands.

The suite passed on executable revision `0a49ac574a749a1584fb51cf9324310340aa05b2`; results and capability limits are recorded in STATUS.md. Current-branch CI and final review are recorded in PR #14.

## CI and historical issues

[P2] [x] Execute the six-job foundation workflow successfully. CI #606 passed backend, frontend, desktop/mobile E2E, expanded native lifecycle, Docker lifecycle and Docker Engine jobs on executable revision `0a49ac5`; CI #600 passed on the preceding installer revision. Run links and verification scope are recorded in STATUS.md.

September runner-allocation failures (including run #523) and early deployment formatting/reserved-table failures are historical. Later runs executed successfully; they must not be listed as active application blockers. Every new revision still requires its own checks.

## Remaining business roadmap

[P2] [ ] Incident Management.
[P2] [ ] SLA engine.
[P2] [ ] Requests and Service Catalog.
[P2] [ ] Workflow and Approval engines.
[P2] [ ] Change and Problem Management.
[P2] [ ] CMDB and Asset Management.
[P2] [ ] Knowledge Base.
[P2] [ ] Notifications and Integrations, including external identity providers.
[P2] [ ] Automation.
[P2] [ ] Reporting and Dashboards.
[P2] [ ] Self-Service Portal.
[P2] [ ] Advanced Administration beyond the delivered foundation.
[P2] [ ] Production hardening: threat model, broader load/recovery tests, TLS/secure cookies, secret rotation, backups and operational monitoring.
