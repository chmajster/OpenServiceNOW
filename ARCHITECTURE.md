# OpenServiceNOW Architecture

## Status

Phase 0 established the target architecture and module boundaries. The implemented stack contains API, migration/bootstrap, worker and scheduler executables, MySQL schema through migration 98, Redis-backed controls, identity/RBAC/audit services, the first-run installer, React administration and own-account UI.

Shared work-record, jobs, outbox, activity and attachment libraries are implemented and integration-tested. The scheduler enqueues durable session- and attachment-cleanup jobs; the worker executes them and dispatches activity events. Incident Management consumes shared numbering, assignments, messages, watchers, relations, attachments and activity through domain-authorized API/UI. Generic work/job-management and arbitrary-object attachment APIs/UI remain future stages. Existing routes/screens cover setup, authentication, own accounts, tenant/user/RBAC/audit administration, incidents, catalog/requests/fulfillment, workflow/approvals and notifications/delivery administration. The remaining sections distinguish implemented foundations from target business capabilities; see STATUS.md and TODO.md for verified progress.

## Connected ITSM domain boundaries

`internal/cmdb` owns typed CI classes, directed relation rules, bounded graph
traversal and archived history. Record adapters call each domain's current read
and mutation policy; links never grant access to either endpoint.
`internal/problems` owns PRB/analytical tasks, controlled transitions, documented
Known Error and explicit public incident transfer. `internal/changes` owns
CHG/execution tasks, independent immutable Standard-template approvals and
calendar rules/reservations. Both reuse work-record allocation/assignment,
communication/files, optimistic versions, atomic audit/outbox and retry receipts.

Workflow subjects explicitly distinguish REQUEST and CHANGE. The existing engine
uses a Change adapter with a frozen public snapshot/version/revision; publication
validates each domain's allowed steps. Material scope edits invalidate prior stamps
and pending executions while retaining decisions. Request catalog execution is
preserved. Scheduling and actual start use the common tenant transaction lock to
check current shared-CI overlap, service windows and blackouts; exceptional actions
have their own current permission, justification and audit.

`internal/workspace` reads minimal references from SQL scopes constructed by the
domains. Search, batched metadata, context targets, report aggregation and source
pagination apply those same current ACL predicates before returning text/counts.
Expressions are validated allowlisted ASTs; scopes never contain caller SQL.
Reports use bounded repeatable-read transactions, UTC half-open time ranges and
exact decimal count/identifier/version contracts. Private messages and catalog
form values are excluded from global references.

The existing AppShell hosts compact shared tables/forms, record tabs and a bounded
context panel. Drafts and uncertain commands remain in memory. Related-list editors
retain the exact command/key across tab changes, and navigation has one owning
record blocker. Tenant/session changes clear context; current batched metadata
removes inaccessible tab/history/favorite labels without loading private records.

## Architectural goals

The implemented `internal/catalog` owns catalogs/categories/items, draft CAS and immutable publications with declarative forms and explicit manual fulfillment. `internal/requests` owns Request/Requested Item/Task state, object access, request snapshots and closure rules. Both use backend session principals; mutations revalidate active membership/permissions under the organization lock in their domain transaction. Public DTOs exclude private catalog/task data. Group operators only read assigned-group records; organization-wide management is a separate permission. Catalog/request create retries use shared `workrecords.ReserveOperation`/`CompleteOperation` with actor/organization keys and body digests.

Manual submission creates the Request, snapshot items, numbered tasks, shared assignments, audit/outbox and retry receipt in one executor transaction. Workflow submission instead atomically creates a pinned execution and first durable job; its task steps use the same transactional task/assignment service after approval. `workrecords` supplies numbering, assignments and append-only messages; `attachments` supplies persistent storage, visibility filtering and orphan cleanup; `activity` projects source events idempotently. Request comment/note events have actual request-specific worker handlers, avoiding the incident-only notification/SLA consumer contract. Public history contains only safe state metadata/public communication; internal notes/file events remain INTERNAL. Finalization requires all required tasks COMPLETED and cancels remaining optional tasks; closed/cancelled requests are terminal. The limited workflow approval engine is delivered below; broader dependency automation remains future work.

The implemented `internal/incidents` domain owns incident fields, object authorization, lifecycle rules and business versioning. It calls shared `workrecords.Allocator.NextInTx` and `workrecords.Service.SetAssignmentInTx` using the same `database.Executor` as incident persistence, audit and outbox; no nested transaction commits independently. Creation idempotency is scoped to the backend actor and organization, while INC uniqueness is enforced per organization and numbering uses the shared row-locked sequence.

Nine incident event types have production handlers registered by `cmd/worker`. Payloads contain complete business before/after snapshots, decimal incident version and mutation-time `occurred_at`; shared activity uses that timestamp and source-event pagination, so delayed/out-of-order projection cannot redefine business time. History reports pending outbox work independently of durable synchronous writes. The implemented SLA consumer reads business occurrence/version, not delivery time. Incident communication/file mutations emit the existing shared work events through the same transaction; production work handlers project them into activity without duplicating replayed events. CMDB is a separate authorized domain with deliberate manual inventory and links; Discovery remains outside the delivered scope.

OpenServiceNOW is an enterprise ITSM / IT Operations platform inspired by the functional class of products such as ServiceNow, while using its own implementation, UX, data model, naming, and architecture.

Primary design goals:

- modular monolith first,
- Go backend with explicit domain boundaries,
- MySQL 8.x as the system of record,
- Redis only for ephemeral coordination and caching,
- API-driven React + TypeScript frontend,
- strict multi-tenant isolation,
- backend-enforced RBAC,
- asynchronous execution for long-running work,
- auditability of business and administrative actions,
- architecture that can later extract selected domains into separate services without forcing microservices prematurely.

## Technology baseline

### Backend

- Go, current stable release,
- `net/http`,
- `github.com/go-chi/chi/v5`,
- `database/sql`,
- `github.com/go-sql-driver/mysql`,
- explicit SQL or generated SQL where it improves maintainability,
- OpenAPI for the public HTTP contract,
- structured logging,
- `context.Context` propagated from transport to infrastructure,
- Go-native worker and scheduler executables.

Heavy ORM usage is not part of the default architecture. If an ORM is introduced later, that decision requires an ADR.

### Database

Primary relational database:

- MySQL 8.x.

MariaDB compatibility may be maintained only where it does not constrain the design.

The application must not depend on PHP MySQLi. In Go, database access uses `database/sql` with the MySQL driver behind repository interfaces.

### Frontend

- React,
- TypeScript,
- client-side routing,
- API-only communication with the backend,
- responsive agent, administration, and self-service interfaces.

### Supporting infrastructure

- Redis for cache, locks, rate limits, temporary state, and job coordination,
- reverse proxy for production ingress,
- Docker Compose for local development,
- Kubernetes-compatible deployment model as a later operational target.

## System topology

```text
Browser
  |
  v
Reverse Proxy
  |
  +--------------------+
  |                    |
  v                    v
React frontend      Go API
                         |
             +-----------+-----------+
             |           |           |
             v           v           v
           MySQL       Redis      Storage
             ^
             |
      +------+------+
      |             |
      v             v
  Go Worker     Go Scheduler
```

MySQL is the authoritative source for persistent business state. Redis must never become the only durable source for incidents, requests, approvals, audit records, CMDB data, workflow definitions, or similar business entities.

## Repository layout

Target layout:

```text
cmd/
  api/
  worker/
  scheduler/

internal/
  platform/
    auth/
    cache/
    config/
    database/
    events/
    jobs/
    logging/
    observability/
    security/
    storage/

  identity/
  users/
  groups/
  tenants/
  rbac/

  tasks/
  incidents/
  requests/
  catalog/
  changes/
  problems/

  cmdb/
  assets/

  sla/
  workflow/
  approvals/

  knowledge/
  notifications/
  automation/
  integrations/

  reports/
  dashboards/
  audit/
  attachments/
  search/
  admin/

api/
  openapi/

migrations/
web/
docs/
  adr/
scripts/
docker/
```

This is the intended direction, not a requirement to create empty packages prematurely. Directories should appear when they contain meaningful code or documentation.

## Backend layering

A business module should normally follow this dependency direction:

```text
HTTP transport
    |
    v
Service / application layer
    |
    v
Domain model and policies
    |
    v
Repository abstractions
    |
    v
Infrastructure implementation
    |
    v
MySQL / Redis / external API
```

Typical module layout:

```text
internal/incidents/
  model.go
  errors.go
  repository.go
  service.go
  handlers.go
  dto.go
  events.go
  mysql_repository.go
  service_test.go
  repository_test.go
```

The exact file split may change as the code grows. The important rule is separation of responsibilities.

### Dependency rules

- HTTP handlers do not contain business rules.
- Services do not depend on HTTP-specific types.
- Business logic does not depend directly on `*sql.DB`.
- SQL remains inside repository/infrastructure implementations.
- Cross-domain calls should occur through explicit service interfaces or events, not by importing another module's persistence internals.
- Platform packages must not import business modules.
- Shared packages are kept small; a generic `common` dumping ground is prohibited.

## HTTP API

Base prefix:

```text
/api/v1
```

API behavior must be consistent across modules:

- authentication,
- tenant context,
- backend RBAC checks,
- request ID,
- structured error responses,
- input validation,
- pagination,
- filtering,
- sorting,
- search,
- idempotency where duplicate execution would be harmful,
- request size limits.

Error shape:

```json
{
  "error": {
    "code": "INCIDENT_NOT_FOUND",
    "message": "Incident not found",
    "request_id": "..."
  }
}
```

Raw MySQL errors and internal stack details must not be returned to clients.

## Request lifecycle

Expected request path:

```text
HTTP request
  -> request ID
  -> security headers / limits
  -> authentication
  -> tenant resolution
  -> authorization
  -> transport validation
  -> service
  -> transaction/repository
  -> audit/event emission
  -> response mapping
  -> structured access log
```

The request `context.Context` must propagate to MySQL, Redis, outbound HTTP, storage, and other cancellable operations.

## Configuration

Runtime configuration should be explicit and validated on startup.

Expected environment variables include:

```text
APP_ENV
HTTP_ADDR

MYSQL_HOST
MYSQL_PORT
MYSQL_DATABASE
MYSQL_USER
MYSQL_PASSWORD

MYSQL_MAX_OPEN_CONNS
MYSQL_MAX_IDLE_CONNS
MYSQL_CONN_MAX_LIFETIME
MYSQL_CONN_MAX_IDLE_TIME

REDIS_ADDR

SESSION_SECRET
ENCRYPTION_KEY
```

Secrets must not be committed to the repository. Development examples use placeholders only.

## MySQL architecture

### Repository access

Business code depends on repository interfaces. MySQL-specific implementations use `database/sql`.

All queries must:

- use parameters,
- use context-aware methods,
- have bounded execution through request or operation timeouts,
- close rows,
- handle `sql.ErrNoRows` explicitly,
- return typed/domain errors rather than raw driver errors where appropriate.

### Transaction management

Atomic workflows use a centralized transaction abstraction rather than scattered direct calls to `db.Begin`.

Example atomic operation:

```text
create request
  + create requested items
  + create fulfillment tasks and shared assignments
  + append audit and outbox records
  + complete idempotency receipt
```

The transaction manager should expose an executor that repositories can use with either the base DB handle or a transaction.

### Connection pool

Pool settings are configuration-driven:

- `MaxOpenConns`,
- `MaxIdleConns`,
- `ConnMaxLifetime`,
- `ConnMaxIdleTime`.

Health checks use `PingContext` with a short timeout.

### Schema management

All schema changes use versioned migrations.

Initial convention:

```text
migrations/
  000001_initial.up.sql
  000001_initial.down.sql
```

Production schema evolution must not rely on runtime AutoMigrate behavior.

### Identifiers

Recommended default:

- internal technical primary keys: `BIGINT UNSIGNED`,
- public API IDs may later use opaque identifiers where exposure of sequential IDs is undesirable,
- human-readable record numbers are separate attributes, for example `INC0000001`, `REQ0000001`, `CHG0000001`.

Number generation must be concurrency-safe and tenant-aware where business requirements demand tenant-local sequences.

### Time

- Go: `time.Time`,
- persistent timestamps: UTC,
- display conversion: frontend/user presentation layer,
- precision: normally `DATETIME(6)` for business timestamps.

### JSON

MySQL JSON is reserved for genuinely flexible structures such as:

- workflow node configuration,
- custom field metadata or values where relational modeling would be excessively rigid,
- integration configuration,
- execution metadata.

Core business relationships remain relational.

## Multi-tenancy

Tenant isolation is a mandatory cross-cutting invariant.

Tenant-owned tables include `tenant_id`, and data access must constrain by tenant.

Unsafe:

```sql
SELECT * FROM incidents WHERE id = ?
```

Expected:

```sql
SELECT *
FROM incidents
WHERE tenant_id = ?
  AND id = ?
```

Tenant scope must not rely only on frontend filtering.

Required controls:

- tenant is resolved from authenticated server-side context,
- clients cannot freely choose another tenant identifier,
- repository queries include tenant predicates,
- unique constraints include `tenant_id` where uniqueness is tenant-scoped,
- authorization checks occur after tenant resolution,
- automated tests must verify cross-tenant access denial.

## Authentication and identity

Backend-owned authentication foundation must support an incremental path to:

- local users,
- secure session or token-based authentication,
- LDAP,
- Active Directory,
- OIDC,
- OAuth2,
- MFA,
- SAML later.

Password storage uses Argon2id or bcrypt. Plaintext passwords are forbidden.

## RBAC

Core model:

```text
users
groups
roles
permissions
role assignments
scopes
```

Authorization is always enforced by the backend. Frontend visibility rules are only UX optimization.

Permissions should use stable names, for example:

```text
incident.read
incident.create
incident.update
incident.assign
incident.resolve
incident.delete
admin.users.manage
```

Future scoped authorization can include tenant, group, service, location, or resource constraints.

## Common task foundation

Incident, request fulfillment, change, problem, and other work records share common concepts but should not be forced into one oversized universal table before requirements are validated.

Reusable capabilities should include:

- record numbering,
- assignment,
- comments,
- work notes,
- watchers,
- attachments,
- activity timeline,
- history,
- audit,
- SLA bindings,
- related records.

The design should favor composable shared services/tables over inheritance patterns that make every record type depend on one fragile schema.

## Events

The implemented outbox uses stable event envelopes and independent durable consumer deliveries. Activity and notifications are production consumers; future modules register their own stable versioned consumer ID and explicit replay policy.

Example events:

```text
incident.created
incident.updated
incident.assigned
incident.resolved
request.created
sla.warning
sla.breached
workflow.started
workflow.completed
workflow.failed
ci.updated
```

Delivery currently runs in cmd/worker using MySQL claims and leases. No in-process-only acknowledgement or Redis-only persistence represents successful delivery. Event contracts remain independent of transport so a broker can be introduced later.

Events are not a substitute for transactional consistency. Where a database write and later asynchronous processing must be coupled reliably, use a transactional outbox pattern.

## Notifications and independent event consumers

MySQL stores a unique delivery per `(event_id, consumer_id)` with its own attempts, lease and outcome. The dispatcher freezes every required consumer and commits the split atomically. Outbox DELIVERED means distribution committed; each delivery independently reports PENDING, DELIVERING, DELIVERED, FAILED or SKIPPED. `activity.v1` and `notifications.v1` are separate consumers. History readiness depends on activity alone. Future modules register stable versioned IDs and define explicit replay policy before worker startup.

Migrations 38–48 mark existing events as notification-ineligible, preserve completed activity, migrate failed activity into retryable deliveries and allow pending legacy activity to finish without sending historical notifications. Already split events never acquire consumers implicitly. Stop the old API/worker/scheduler while upgrading; mixed dispatcher versions are unsupported.

`internal/notifications` consumes assignment, communication, state and resolution events. It verifies current membership and incident/private-note access before materializing a unique event/user effect. In-app effects and SMTP jobs commit together using shared jobs transactions. Lists, counters, read actions and own mail history filter access before pagination. SMTP rechecks recipient access and preferences before each attempt. Observing a record grants no access.

Own notification/preferences/history APIs and administration configuration/queue/retry APIs are exposed through working React screens. `admin.notifications.manage` gates administration. Tenant-bound AES-256-GCM encrypts SMTP passwords with an external 32-byte key; API and logs contain only safe metadata/error codes. SMTP is optional and in-app requires no key or mail account. Versioned configuration and retries preserve concurrent-write safety; the administrator test targets only their own login address.

SMTP uses durable existing worker jobs with bounded network timeouts and records real attempt outcomes. DELIVERED requires acknowledged DATA acceptance, rather than enqueue success. Lost leases may leave UNKNOWN attempts; durable job exhaustion is reflected as FAILED. External SMTP is at-least-once: acceptance followed by lost acknowledgement can cause a duplicate on retry. Templates contain generic summaries and incident identifiers, never private bodies, titles or filenames. Editable templates, inbound mail and webhooks remain future modules. See [README notification operations](README.md#notifications-and-independent-event-consumers) for external-key setup, safe upgrade/replay policy, optional local Mailpit and runtime limits.

## Jobs and workers

Long-running operations must not block HTTP requests.

Persistent job metadata should include:

- tenant,
- type,
- state,
- progress,
- payload,
- result,
- error,
- retry count,
- timestamps.

Implemented job states:

```text
QUEUED
RUNNING
RETRYING
COMPLETED
FAILED
CANCELLED
```

Workers have explicit lifecycle management, concurrency limits, cancellation, retry policy, and graceful shutdown. Arbitrary unmanaged goroutines are not the job system.

## Scheduler

The scheduler is a separate executable using shared internal packages.

Scheduled responsibilities may include:

- SLA evaluation,
- scheduled automation,
- notification retries,
- integration retries,
- maintenance jobs,
- cleanup.

Current session-cleanup scheduling uses a UTC time bucket and a unique MySQL job idempotency key, so multiple scheduler iterations/replicas share one durable job per bucket. Workers claim jobs with transactional `FOR UPDATE SKIP LOCKED`, leases and attempt history. Additional scheduler workloads must define their own deduplication/coordination policy.

## Workflow and approvals

`internal/workflow` implements the bounded catalog process engine through MySQL, authenticated OpenAPI and React UI. Definitions use draft CAS; published versions are immutable and executions reference the exact tenant/definition/version. Submission atomically persists the Request/Requested Item, execution, job, audit and outbox. Workflow mode creates no direct tasks; the owning `internal/requests` adapter creates real shared assignments/tasks in the step transaction. A workflow submission contains one item; manual submissions retain their multi-item support. Coordinating multiple process instances requires the future join policy.

The nine delivered nodes are START, CONDITION, APPROVAL, CREATE_TASK, WAIT_TASK, WAIT_TIMER, UPDATE_RECORD, NOTIFY and END. Publication validates all references/configuration, DAG reachability, approval on every task/fulfillment path, task dominance/completion, closure after fulfillment and no further approval/task creation after fulfillment. Conditions compare immutable submitted scalar values; no scripts execute. Definitions are bounded to 100 per tenant, nodes to 50 and path variants to 10,000.

Authors (`workflow.author`) and publishers (`workflow.publish`) do not grant execution privileges. The pinned execution identity must currently hold `workflow.execute`, `request.fulfill`, active organization membership and membership in the configured fulfillment group. The request adapter verifies durable tenant/request/item/executor scope and calls shared domain commands with their state/version/required-work rules. HTTP transitions cannot bypass a pending/error/cancelled process. No synthetic administrator permission is introduced.

Worker jobs advance one step in a transaction under organization/request/execution locks. Step result, task/domain mutation, audit/outbox and next job commit together. Stable job keys and persisted step results prevent duplicate task creation after retry. Waiting deadlines/participants live in MySQL; no long-lived sleeping goroutine exists. Scheduler selects at most 100 due rows at most every 30 seconds; waiting polls update only next-check time, without decision-version churn. Timely task completion uses its persisted business timestamp even when the worker is late. Reads do not advance execution.

Approvals target a user or bounded group, with ANY or ALL. At step entry, eligible participants are frozen; later members do not join. Every decision rechecks active identity, organization, workflow.approve, current group membership and explicit self-approval DENY/ALLOW. Rejection or deadline follows the configured branch. ALL rechecks prior signers; lost membership does not shrink quorum. Decision CAS and the same locks serialize concurrent decisions/timeout, and an identical committed retry is harmless. Reasons are audited without exposing other participants' private reasons to the portal.

Object-filtered inbox/state/notifications enforce current access before pagination/counts. Execution UI presents step labels, state and deadlines without private task instructions. `workflow.manage` exposes safe error codes, last 100 business attempts and reason/version-protected RESUME after error or CANCEL. Cancellation stops execution; the separate domain cancellation command controls request/tasks. Resume uses the same immutable version and current identity, without granting privileges. Activity and notification consumers independently handle workflow.changed, workflow.approval.requested and workflow.notification; the latter two have real in-app and optional SMTP effects with fresh read/delivery access checks.

Parallel/join, subprocesses, delegation, arbitrary scripts, external automation and reminders/escalation are future work. Migration 71 downgrade to incident-only notifications refuses incompatible persisted workflow notifications until deliberately exported/removed. Normal upgrade/restart preserves workflow state and credentials.

## SLA subsystem

`internal/sla` implements incident response/resolution policies and organization calendars through persistence, API and React administration. Definition roots use optimistic locking; published JSON versions are immutable and audited. The selected policy is the latest version effective at incident creation, with lower administrative order, greater priority/category specificity and lower stable ID breaking ties. A policy always references an explicit immutable calendar version. New definitions do not enroll historical incidents; disabling a policy affects future selection only.

Weekly windows use local IANA time; holidays/exceptions use local ISO dates. Exceptions override holidays and weekday hours. Business/event/deadline timestamps are UTC. Calendar calculations intersect windows with the real timeline: spring missing minutes do not count and autumn repeated minutes count twice. Civil-date iteration also handles skipped/repeated local midnight without shifting working windows. Intervals do not cross local midnight; overnight schedules use two days. The calculation horizon is bounded to ten years. Definitions are bounded to 100 of each kind per organization, 1000 holidays/exceptions and eight windows per day. Timers preserve microseconds internally and expose decimal-string microsecond values alongside second summaries.

The independent `sla.v1` consumer enqueues durable incident reconciliation, while a scheduler sweep every at most 30 seconds selects at most 100 due/new incidents. Jobs fetch at most 200 source transitions, persist their checkpoint and enqueue continuation when required. They read committed outbox events in incident-version/business-time/source-ID order under the same organization/incident lock used by domain writes, rather than relying on delivery order. The replay cursor, policy/calendar snapshot, current cycles, every finished prior cycle, transitions, accrued time, pauses, deadlines and thresholds survive restart in MySQL. Timer advancement occurs only after the committed transition backlog is exhausted; delayed resolution cannot become a false breach at worker receipt time.

Creation starts both metrics. `incident.first_response` is emitted atomically with the first appended public reply from an operator with organization-wide incident read who is not the reporter; private notes and reporter replies cannot stop response SLA. Only exact configured ON_HOLD reasons pause. Resolution/closure stops both metrics; a missing public response is explicitly UNANSWERED, never treated as answered. Cancellation cancels. Priority/category changes preserve the initial choice and record that decision; reopening starts a new resolution cycle with the original definition/calendar and leaves the response metric unchanged. Calendar edits require a new policy version to affect future incidents; no silent historical rebinding exists.

Warning and breach outbox events, history and audit commit with the instance, keyed by instance/provided threshold. `activity.v1` and `notifications.v1` have real handlers; the latter reuses current object authorization, in-app/SMTP delivery and state-change preferences. Retry does not duplicate MySQL escalation effects; external SMTP retains its documented ambiguity. SLA GET routes only read authorized snapshots/history, never run timers. A stopped worker delays displayed calculations and notifications, while synchronous incident operations remain available. Queue delivery administration exposes failed SLA consumers. Scheduler backlog and work duration can extend processing beyond the 30-second cadence; this is not a delivery-time guarantee.

## CMDB and assets

CMDB configuration items and assets are separate domains.

CMDB target:

- class definitions and inheritance,
- configuration items,
- typed/custom attributes,
- relationships,
- service/application/infrastructure representations,
- dependency graph.

Asset target:

- ownership,
- lifecycle,
- procurement,
- warranty,
- vendor,
- serial/licensing data,
- cost information.

An item may have links between asset and CI representations without treating them as the same concept.

## Audit and activity

Audit records are append-only from the normal application path and capture:

- tenant,
- actor,
- action,
- object type and ID,
- old/new values where appropriate,
- request ID,
- source,
- IP metadata where legally and operationally appropriate,
- timestamp.

Incident activity is a user-facing projection combining incident business changes and shared communication/file/observer/relation events. Visibility is durable and defaults to INTERNAL; only known public business events and explicit public communication are exposed. Repository SQL filters private payloads before pagination/cursor/pending checks. SLA warning/breach producers are implemented; approvals and automation remain future producers.

Incident-bound routes first resolve the current session actor/organization and readable domain record; clients cannot select an arbitrary object reference. Internal note/file/activity access additionally requires organization-wide incident read and internal-note read. File references inherit linked message visibility and authorize the linked record. Public file DTOs exclude storage keys/paths; download rechecks access and uses safe attachment/MIME headers. Shared mutation methods accept the domain transaction executor, including permission checks with a single SQL connection.

File bytes are written atomically under the configured local storage root; only metadata is stored in MySQL. Docker/DevBox share a persistent attachment volume across API/worker/scheduler, outside writable container layers. Native storage is a persistent configurable directory. Backups and installer-mode migrations must include both SQL and bytes. Prepared files are removed on confirmed rollback; uncertain commits retain bytes. A durable hourly cleanup job deletes managed orphan/temp/tombstoned bytes after 24 hours using bounded reference queries. Removing a file commits its tombstone/audit/outbox first and immediately denies new download requests.

Incident detail tabs and list filters/cursors are URL state. Private draft contents stay in memory, with navigation/refresh guards; HTTP idempotency keys reconcile uncertain replies/uploads. Operator selections capture decimal identifiers and versions, and each bounded batch record is authorized/version-checked in its own transaction with an explicit result.

## Security baseline

Mandatory controls:

- strong password hashing,
- parameterized SQL,
- strict tenant isolation,
- server-side RBAC,
- rate limiting,
- secure cookies or token handling,
- CSRF protection where relevant,
- explicit CORS policy,
- secure HTTP headers,
- attachment validation,
- request body limits,
- encrypted sensitive integration secrets,
- structured audit,
- no secret logging,
- SSRF protections for configurable outbound integrations,
- safe file path handling,
- authorization on every object access to prevent IDOR.

Security-critical state changes should use idempotency and/or optimistic locking where duplicate requests could cause inconsistent state.

## Concurrency and locking

Go concurrency is used deliberately rather than automatically.

Required practices:

- bounded worker pools,
- cancellation via context,
- no goroutine leaks,
- safe shared state,
- race testing on concurrency-sensitive code.

Important mutable records should support optimistic locking, typically with a version column:

```sql
UPDATE incidents
SET title = ?,
    version = version + 1
WHERE id = ?
  AND tenant_id = ?
  AND version = ?
```

Zero affected rows indicate a conflict.

## Performance

Baseline requirements:

- pagination on all potentially large collections,
- cursor pagination path for very large data sets,
- no unbounded list loads,
- avoid N+1 queries,
- indexes designed from query patterns,
- large exports executed as jobs,
- connection pool metrics,
- rate limits and bounded concurrency,
- cache only where consistency semantics are explicit.

Likely composite indexes will involve combinations of:

- `tenant_id`,
- `status`,
- `assignee_id`,
- `assignment_group_id`,
- `priority`,
- `created_at`,
- `updated_at`,
- `number`.

Indexes will be finalized against concrete queries and measured plans rather than added blindly.

## Observability

The platform should expose:

- structured logs,
- health and readiness endpoints,
- request latency metrics,
- HTTP status metrics,
- DB pool metrics,
- worker/job metrics,
- scheduler metrics,
- failure/retry metrics.

Log fields should include where available:

```text
timestamp
level
request_id
user_id
tenant_id
module
operation
duration
error
```

Passwords, tokens, session secrets, encryption keys, and full credentials must never be logged.

## Global identity and account security

`users` holds a global identity and credential version; `tenant_memberships` holds organization access. Self-service profile/password/session APIs derive the global user and current organization from the backend session. Organization-administration permissions grant no authority to reset another user's global credentials or manage their sessions elsewhere. Membership deactivation retains organization-scoped session revocation.

Login verifies Argon2id outside SQL locks, then locks the current tenant and global user, validates the credential version and active membership, and inserts the session in the same transaction. Account writes use the same tenant-before-user lock order and revalidate the current session and CSRF hash under lock. Password rotation increments the credential version, clears the required-change flag, revokes other global sessions and replaces the current token/CSRF hashes without creating a duplicate session. Audit shares the transaction and contains no password, hash or token. Redis attempt limits and a shared bounded Argon2 pool cover password operations and administrative account creation.

Migration 26 defaults the required-change flag to false and credential version to one, preserving existing credentials. New administrator-created accounts and the default installation password are temporary; custom first-run passwords are permanent unless headless bootstrap explicitly requests a temporary password. Restricted accounts can only access minimal `/me`, password change and logout. Identity middleware rejects all other authenticated routes before module handlers. Migration 27 indexes bounded global account-session reads. The React account page, public API and persistence are implemented together; MFA and external identity providers remain future stages.

Password transactions either commit fully or roll back fully; a lost COMMIT acknowledgement is explicitly reported as an uncertain result. Session rotation makes old-cookie retries unauthenticated, while credential-version preconditions reject stale writes. Uncertain commit/transport outcomes are reconciled through `/me` or a fresh login with the new password, without an automatic write retry. The API client ignores late unauthorized responses belonging to a previous CSRF generation so they cannot clear a newly rotated session. Bootstrap applies new-password validation only when creating an identity; existing matching legacy credentials and required-change flags are preserved.

## Graceful shutdown

API, worker, and scheduler processes handle SIGTERM/SIGINT.

Shutdown sequence:

1. stop accepting new work,
2. cancel root context,
3. allow active operations to finish within a deadline,
4. stop worker intake and scheduler ticks,
5. close external clients/pools,
6. flush log/telemetry buffers where required,
7. exit.

## Frontend architecture

The frontend is API-driven and organized by product capability rather than direct database concepts.

Implemented administration primitives include Polish permission-aware navigation and breadcrumbs, bounded server-side list/search contracts, paginated remote user/group/role comboboxes, role/group detail tabs and explicit creation forms. Query filters and tabs are URL state. Each response is guarded by its object/query identity and cancellation; assignment editors cannot mutate an old page while another object is loading.

Organization and role-permission editors retain local proposals after transport errors and HTTP 409. Conflict resolution compares a fresh tenant-scoped server object with the draft, then explicitly selects either current values or a proposal for a subsequent version-checked save. Failure to read the current version keeps saving disabled.

The shared API client sends CSRF cookies/headers and invalidates the AuthProvider on protected 401 responses. Session generations and aborted-request guards reject stale responses; refresh cancellation prevents a late `auth/me` from resurrecting logout. Each context refresh supersedes an earlier pending read, so a post-write refresh cannot reuse pre-write permissions. Context refresh follows login, current-organization changes and RBAC/membership mutations, including successful writes that remove permission to refetch the object, plus window return and server expiry. Backend object scope, dual permissions, system-role protections and last-administrator checks remain authoritative.

Audit reads combine parameterized equality/date filters with tenant scope from the session and descending BIGINT cursor pagination. The React view presents field differences plus optional raw JSON and request metadata. Role-permission audit snapshots share the write transaction and role lock. Existing audit indexes support tenant, date, actor and object reads; this stage adds no schema migration or separate audit store.

Future general UI primitives (beyond implemented administration):

- global search,
- filter builder,
- saved views,
- activity timeline,
- bulk actions,
- dashboards.

All user-visible actions require loading, success, empty, disabled, and error states where applicable.

The UI may hide actions based on permissions returned by the API, but hidden controls do not replace backend authorization.

## Testing strategy

Backend:

- unit tests for domain/service rules,
- `httptest` for transport behavior,
- repository integration tests against MySQL,
- transaction tests,
- tenant isolation tests,
- RBAC tests,
- race tests where concurrency is involved.

Frontend:

- unit/component tests,
- API-contract-aware tests,
- Playwright for critical user journeys.

Cross-cutting end-to-end coverage should eventually validate:

```text
UI
  -> API
  -> auth/RBAC/tenant
  -> service
  -> MySQL / worker
  -> state update
  -> API reload
  -> UI state
```

## Deployment direction

Development:

- `docker-compose.yml` starts the infrastructure-only MySQL/Redis stack for `make dev` and the local installer,
- root `compose.yaml` builds API/frontend/worker/scheduler and runs private Redis for DevBox,
- DevBox supplies the shared MySQL database/account and DB_* secrets through its private runtime override; the entrypoint maps them to MYSQL_*,
- API applies versioned migrations and idempotent bootstrap before readiness; frontend/worker/scheduler wait for API health,
- the non-root frontend serves built React assets and proxies API/health on the same origin; DevBox publishes its selected port and mounts original sources RW,
- application SQL state and durable jobs remain in the shared MySQL volume; Redis cache is disposable.

The native `install.sh --local` path builds API/worker/scheduler and React from the current checkout, provisions only MySQL/Redis through `docker-compose.yml`, and runs the four application processes in detached sessions. Each process has its own log and PID record bound to user, checkout, executable, kernel boot ID and process start time. Lifecycle commands serialize through a checkout lock; repeated start reuses verified processes. Startup rollback removes only processes/dependencies started by that attempt and reports the remaining process state.

The native API selects/saves an available loopback port in 8080–8100; Vite proxies both `/api` and `/health` to that port through `OPENSNOW_API_PORT`. API readiness checks MySQL, migrated schema and Redis independently of background process liveness. Read-only installer status reports background exits separately, including when Docker is unavailable. Processing is proven in an isolated lifecycle test by correlating known scheduler job IDs with durable worker attempts/results and the session-cleanup side effect. Status never creates a probe job. Continuous background heartbeats/queue-lag monitoring are not currently exposed by API or UI.

Production direction:

- stateless API replicas,
- independently scalable worker replicas,
- scheduler with single-leader/distributed coordination,
- managed or highly available MySQL,
- managed/high-availability Redis where used,
- external object storage for attachments,
- reverse proxy / ingress,
- Kubernetes compatibility without making Kubernetes a Phase 1 requirement.

## Extraction strategy

A modular monolith remains the default. A domain becomes a service candidate only when there is a concrete operational reason, for example:

- independent scaling pressure,
- deployment cadence isolation,
- security boundary,
- high-volume integration workload,
- ownership boundary,
- reliability isolation.

Potential future extraction candidates include notifications, automation execution, search indexing, reporting/export, and integration workers.

## Initial architectural invariants

The following rules should be treated as non-negotiable unless replaced by an ADR:

1. Go backend.
2. MySQL 8.x primary relational datastore.
3. Modular monolith before microservices.
4. Business logic outside HTTP handlers.
5. SQL behind repositories.
6. Multi-tenant filtering in backend data access.
7. Backend-enforced RBAC.
8. Explicit schema migrations.
9. Redis is not the business system of record.
10. Long-running work executes asynchronously.
11. Audit is append-oriented.
12. No secret material in source control or logs.
13. Public behavior is documented through OpenAPI.
14. Incomplete functionality is tracked explicitly and never represented as complete.

## Phase 0 result

Phase 0 supplied the architectural direction and ADRs. Runtime foundations and the shared work platform have since been implemented; the target business modules above remain roadmap work. A library, HTTP route and UI capability are separate delivery milestones and must not be marked complete interchangeably.
