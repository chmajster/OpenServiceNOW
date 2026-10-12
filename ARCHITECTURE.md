# OpenServiceNOW Architecture

## Status

Phase 0 established the target architecture and module boundaries. The implemented stack contains API, migration/bootstrap, worker and scheduler executables, MySQL schema through migration 32, Redis-backed controls, identity/RBAC/audit services, the first-run installer, React administration and own-account UI.

Shared work-record, jobs, outbox, activity and attachment libraries are implemented and integration-tested. The scheduler enqueues durable session-cleanup jobs; the worker executes them and dispatches activity events. Incident Management consumes shared numbering, assignments and activity through domain-authorized API/UI. Generic work/job-management and attachment/communication API/UI remain future stages. Existing routes/screens cover setup, authentication, own accounts, tenant/user/RBAC/audit administration and incidents. The remaining sections distinguish implemented foundations from target business capabilities; see STATUS.md and TODO.md for verified progress.

## Architectural goals

The implemented `internal/incidents` domain owns incident fields, object authorization, lifecycle rules and business versioning. It calls shared `workrecords.Allocator.NextInTx` and `workrecords.Service.SetAssignmentInTx` using the same `database.Executor` as incident persistence, audit and outbox; no nested transaction commits independently. Creation idempotency is scoped to the backend actor and organization, while INC uniqueness is enforced per organization and numbering uses the shared row-locked sequence.

Nine incident event types have production handlers registered by `cmd/worker`. Payloads contain complete business before/after snapshots, decimal incident version and mutation-time `occurred_at`; shared activity uses that timestamp and source-event pagination, so delayed/out-of-order projection cannot redefine business time. History reports pending outbox work independently of durable synchronous writes. Future SLA must consume business occurrence/version, not delivery time. Incident communication/files and CMDB are later domain integrations, not fields or controls backed by absent providers.

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
  + create approvals
  + append audit records
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

Phase 1+ should introduce an internal event abstraction with stable event envelopes.

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

Initial delivery may be in-process. Event contracts must not assume the implementation is permanently in-process so a broker can be introduced later.

Events are not a substitute for transactional consistency. Where a database write and later asynchronous processing must be coupled reliably, use a transactional outbox pattern.

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

Workflow is versioned and node-based.

Initial target nodes:

```text
Start
End
Condition
Approval
CreateTask
UpdateRecord
Notification
Wait
Timer
Webhook
HTTPRequest
Automation
Parallel
Join
Subworkflow
```

Definitions and published versions are immutable relative to active executions. Running instances reference a specific version.

Approvals are generic and support user/group targets, sequential/parallel policies, delegation, timeout, reminder, and escalation.

## SLA subsystem

SLA processing is asynchronous and business-calendar aware.

The subsystem requires:

- definitions,
- start/pause/stop conditions,
- calendars,
- business hours,
- holidays,
- warnings,
- breaches,
- escalation.

SLA values cannot be calculated only when a record is opened. Scheduler/worker execution is part of the design.

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

Activity timeline is a user-facing projection that can combine comments, work notes, assignment/status transitions, approvals, SLA events, automation, attachments, and selected audit events.

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
