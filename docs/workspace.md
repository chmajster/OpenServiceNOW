# Operator workspace

The operator workspace combines the implemented incident, fulfillment-task and incident coordination-action queues while keeping record data and commands in their existing domains. A work reference is scoped by tenant, object type, and decimal-string object ID; IDs from different tables therefore do not collide.

## Available workflows

- Query a shared, server-filtered queue for incidents and fulfillment tasks using mine, group, unassigned, watched, and all-visible views.
- Search and sort with bounded cursor pagination; group and select visible columns; construct nested AND/OR filters with typed operators and references.
- Save private or shared server views with optimistic versions. Sharing requires `admin.rbac.manage`; a shared definition never grants access to its records.
- Open several work records in tabs. The workspace embeds the existing incident and task forms, including their domain actions, metadata fields, communications, attachments, history, SLA/process sections, and conflict handling. Draft contents stay in component memory and are not written to local storage. Closing a dirty tab requires an explicit discard action; leaving the route uses the existing unsaved-work guard.
- Use registered assignment and transition commands for per-record and bulk actions. The API checks each record and version, returning an individual result. Domain workflow rules continue to decide which transitions are valid.
- Request an asynchronous CSV export. The worker rechecks the requester's active membership, current permissions, filters, and row access; it stores the result as an internal attachment and records exported references for access revalidation at download time. Exports are bounded to 10,000 rows and the configured attachment size.

Favorites, recents, open-tab references, and list preferences are stored under a tenant-and-user-specific local-storage key. Only reference metadata and list state are stored there, not editable record contents. The server remains authoritative when a stored reference is reopened.

## Permissions

- Incidents are available to users with `incident.read`; users with `incident.read_own` are restricted to records they reported or are affected by.
- Fulfillment tasks are available to users with `request.manage` or `request.fulfill`; fulfillment access is limited to active assignment groups where the domain API requires it.
- Filter and column metadata is limited to active fields visible in the current tenant's metadata list configuration and the actor's role view.
- Shared views require `admin.rbac.manage`. The workspace does not expose permission editing through a view.
- Each endpoint derives the tenant, user, and permissions from the authenticated session. The worker reloads the user's current membership and permissions before processing each export page.

## API and runtime

The authenticated API is under `/api/v1/workspace`:

- `GET /fields` and `GET /references/{kind}` provide actor-scoped field and reference choices.
- `POST /queue` runs a bounded queue query; `GET /items/{type}/{id}` reloads one authorized record summary.
- `GET/POST /views`, `PUT/DELETE /views/{id}` manage saved definitions.
- `POST /actions` runs registered assignment or transition commands per record.
- `POST /exports`, `GET /exports/{id}`, and `GET /exports/{id}/file` create, inspect, and download an export.

Use the normal tenant-local MySQL/Redis configuration; the workspace requires no external provider. Run `make migrate-up`, start the API with `go run ./cmd/api`, and start the worker with `make worker`. The workspace export handler is registered by `cmd/worker`. Migrations 000124-000129 add saved views, export state/reference records, and indexes for the queue queries. The down migrations for saved views and exports intentionally stop with a SQL error and require an explicitly reviewed recovery plan before downgrade; they do not silently delete data. The integration lifecycle test substitutes drop statements only in a temporary migration copy for its disposable test schema.

## Current boundary

The queue currently registers the incident and fulfillment-task types. Adding a further domain requires a workspace adapter and corresponding authorization, query, detail, action, and export support; no Change, Problem, or arbitrary record type is implied. The right-hand preview is a summary of the selected work item. Related records can be opened through existing domain links, which invoke the unsaved-work guard, but a separate in-place preview for every reference type is not yet provided. Workspace-specific labels are currently Polish; full PL/EN workspace resource coverage remains open.

The repository's current Go toolchain and isolated MySQL/API/worker environment were unavailable during this implementation. Backend compilation, Go tests, migration execution, worker restart recovery, and real desktop/mobile API-backed Playwright flows therefore remain unverified. See [STATUS.md](../STATUS.md) for the exact local checks and remaining acceptance work.

Coordination actions (`incident_action`) use their domain CAS/owner checks, shared work_assignments and private workrecords notes. They remain visible after incident closure. Their root incident ACL is rechecked for query, preview, commands and export download. See [Major Incident configuration](incidents.md).
