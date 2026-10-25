# Incident process and Major Incident (Module 08)

The existing incident remains the source of truth. The modular Go monolith uses the same identity/RBAC, metadata, workrecords, MySQL transactions, outbox, activity, jobs, notifications and SLA services. This module needs no external infrastructure inventory, mail provider or conference API.

## Start and configure

Use the normal installer/update on a clean main checkout, or run `go run ./cmd/migrate up` before starting the API, worker and scheduler. Required schema: **151**. Keep the existing `.env`, database volume and attachment root. No production sample organizations, services, queues or users are seeded by this module.

1. Create local users and groups in the existing administration. Enter services in the service portfolio and CIs in CMDB as needed.
2. Open **Administration of incidents** (`/incidents/admin`) with `admin.metadata.manage`. Edit incident dictionaries, priority matrix, ordered assignment rules, templates, allowed transitions, reopening window and closure requirements. Save a draft, then publish it. Preview uses the currently published rules.
3. Set an optional default queue. A blank queue leaves unmatched incidents explicitly unassigned. Rules match service, category, location, calculated priority, title/description and an optional custom-field value; the first active match wins. Reapplication refuses manually assigned or owned work. Manual overrides require a reason.
4. Grant operators incident read/update/assignment/communication permissions as appropriate. `incident.major.nominate` permits nomination only. `incident.major.manage` approves/rejects/promotes/demotes and coordinates; `incident.major.read` reads private coordination. Every command still requires access to the underlying incident. Nomination is granted to the existing platform administrator role by migration; other roles require an explicit grant.
5. Use **Major incidents** (`/workspace/major`) or the incident coordination tab. Select the commander, deputy, communications owner, technical teams, affected services/CIs, estimated/confirmed impact and workaround. Selected owners must be active in the tenant and able to read the incident. A conference bridge is an HTTPS URL without embedded credentials or fragment; the application does not create a conference.
6. Use **My actions** or the common operator workspace for `incident_action` tasks. Tasks have immutable ACT numbers, current owner authorization, CAS state changes and private notes stored in shared workrecords. Closing an incident leaves unfinished tasks visible to their owners. Major read alone does not authorize task mutation.

Metadata publishes atomically per tenant. Existing dictionary keys remain stable; retirement prevents new selection while keeping original records readable/editable. Every active category needs an active subcategory; required dictionaries cannot lose all active values. Templates have an explicit stable key and version. Templates referencing services are offered only to actors with portfolio read/manage permission. Changing a published template body requires a higher version. Existing incidents pin their process snapshot/version and selected template identity; publishing does not silently rewrite them or historical SLA.

Automatic closure is disabled by default (`auto_close_hours=0`). With a positive value, the durable minute sweep closes eligible resolved incidents after their pinned waiting period. It reloads the current assigned user's permissions and checks all closure requirements; the job grants no privileges. Missing assignment or denied/invalid closure leaves the record resolved. Rejected attempts are deferred for one hour to avoid starving later records. Reopening clears the retry delay, requires a reason, current authorization and the pinned response window. A zero reopening window means no time limit; requester reopening additionally requires the explicit pinned policy.

## Coordination and recovery

P1 is a priority, not automatic Major Incident status. Nomination uses the incident version; subsequent decisions use the independent case version. Every decision has a reason. Versions and all BIGINT IDs, including arrays, are decimal JSON strings. The API uses session-derived tenant/actor, CSRF for writes, optimistic versions, and durable idempotency receipts for append/create commands. After a lost response repeat the identical body and Idempotency-Key. After a 409 compare the current state and deliberately reconcile the preserved draft.

The plan records audience, interval (5–1440 minutes) and next update deadline. Deadlines are persisted in MySQL. The scheduler enqueues `incident.maintenance`; the worker advances each deadline and writes its reminder outbox event in the same transaction. Restarting the worker does not depend on an open browser or create duplicate reminders for that deadline.

Public and operator updates are separate immutable publications. In-app notifications work without SMTP. Recipients are calculated using current membership, permissions, record access and the configured audience. A child requester sees allowed public updates without coordination participants, internal notes, review, private task data or delivery/author details. Watcher status alone never grants incident read access.

Publication starts PENDING. Dispatch creates actual shared notification/mail delivery rows. The visible delivery result is derived from those rows: available in-app, SMTP queued, confirmed delivery, partial/failed delivery, missing SMTP configuration or uncertain SMTP result. SMTP acceptance/queueing does not mean the recipient read the message. In-app availability takes precedence in the summary; detailed optional mail outcomes and retry controls remain in existing notification administration. Configure tenant SMTP there, using the existing encrypted secret mechanism. External SMTP cannot promise exactly-once delivery after an ambiguous network result.

Each child has at most one active parent; cycles and cross-tenant links are rejected. Multiple siblings may share one parent. The child list is paginated. Bulk commands accept at most 50 distinct child ID/version pairs. Public-message propagation selects an approved parent publication; the server derives its body. Resolution propagation appends a proposal, without closing or changing the child's lifecycle. Each item has a durable independent success/conflict/denial outcome. Replaying the same batch returns its recorded outcomes even after the parent changes; select failed items with current versions and a new key for a deliberate retry.

Resolve the root incident using the original state tab, then confirm actual detected/restored times and the required coordination fields. Restoration is separate from post-incident review completion. Review records impact, known causes, what worked and corrective actions with owners/due dates. Closure checks the pinned fields and blocks an active/candidate major case until it is restored or demoted. A completed review may still have open actions.

The original `incident.created/assigned/state.changed/resolved/closed/reopened/first_response` outbox meanings remain intact. The existing SLA engine pins its policy/calendar and starts the next resolution cycle on `incident.reopened`; response SLA is not reset. Major decisions, publications and review events do not recalculate SLA or replay old notifications. Optional Problem/Change/Alert modules have typed reference-provider contracts in `integration_contracts.go`; no unavailable integration buttons or fake creation endpoints are shown.

## Schema and migration compatibility

New migrations **139–151** add process snapshots/service references, shared action numbering/classification, nomination permission, single-parent uniqueness, deadline indexes, delivery preference outcomes, legacy task assignment backfill, automatic-closure retry timestamps and durable batch checkpoints. Existing legacy task IDs are retained and numbered ACTL; new tasks use the shared allocator. No historical migration SQL was edited.

The migration driver fixes three historical fresh-install incompatibilities while holding the same MySQL advisory-lock connection: migration 114's missing `(tenant_id,id)` candidate key on IRE observations, migration 134's oversized utf8mb4 VARCHAR (created as MEDIUMTEXT), and migration 135's oversized review columns (created as TEXT). The candidate key is also checked before process migration 139 on upgrades. The repair works with a single-connection pool. Existing accounts, credential hashes, sessions, incidents, attachments and metadata are preserved. If legacy data contains contradictory parent links, reconcile them explicitly before applying the unique index; the migration does not discard relations.

Destructive down migrations stop with SQLSTATE 45000 and an export/migration instruction. Back up and plan an explicit data conversion for a downgrade; do not force a failed migration version to hide a production failure. The lifecycle test uses a separate disposable SQL source with explicit test-only drops to exercise old upgrades without weakening production guards.

## Local verification

Run tests only on a disposable MySQL schema/Redis instance, never on the application database. The integration test creates separate controlled tenants and local identities. Stop unrelated test workers while executing deterministic integration assertions.

```bash
go test ./...
go vet ./...
go build ./cmd/...
go test -race ./internal/incidents ./internal/metadata ./internal/workspace ./internal/notifications
go test -tags=integration ./internal/integration -run 'Test(Module08|Incident|DurableSLA|Metadata|Workspace|IndependentNotifications|EventSplitRollbackAndLease)' -count=1 -timeout=300s
# Separate disposable schema, already migrated to the required version:
go test -tags=integration ./internal/integration -run '^TestMigrationLifecycle$' -count=1 -timeout=300s
# Dedicated module08_migrations* disposable schema, intentionally left dirty:
go test -tags=integration ./internal/integration -run '^TestModule08ProtectedDowngrade$' -count=1
npm --prefix web test
npm --prefix web run build
```

`web/e2e/module08-real.spec.ts` uses real browser → API → MySQL/outbox/worker flows with no intercepted responses. Start the local app/worker/scheduler, set `E2E_MODULE08_REAL=1`, `E2E_BASE_URL`, `E2E_TENANT`, `E2E_EMAIL`, `E2E_PASSWORD` and `E2E_DISPLAY_NAME` to an isolated local administrator fixture. Optionally set `E2E_CHROMIUM_EXECUTABLE` to an installed test Chromium. Then run `npm --prefix web run e2e -- module08-real.spec.ts`. Both desktop Chromium and mobile Chromium run. The suite writes real records and configuration in that test tenant; discard the fixture afterward.

Coverage includes assignment order/manual preservation, retired categories, tenant isolation, nomination vs approval, competing promotion, private/public child access, repeated requests, batch checkpoints, shared notes/workspace access, persisted reminders after service reconstruction, actual idempotent in-app delivery, independent restoration/review/tasks, actual workspace export through the worker and access revocation after task reassignment, pinned automatic closure, SLA reopening, CSRF denial, network-loss draft recovery, configuration conflict reconciliation and desktop/mobile layout. SMTP protocol/error/timeout/retry coverage remains in the shared notifications integration suite; no production SMTP provider or conference provider was contacted.
