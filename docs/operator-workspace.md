# Integrated operator workspace

The implementation is developed on `feat/operator-workspace`, draft PR #24.
It must not be merged automatically. The original `feat/cmdb-assets` working
directory contains unfinished CMDB work, which is preserved and may be reused
in this isolated worktree with the user's authorization.

## Verified architecture and baseline

The application is a Go/MySQL modular monolith with Redis-backed sessions and
ephemeral signals, React/TypeScript, durable outbox deliveries, jobs, a worker
and scheduler. Work records provide numbering, assignments, communication,
attachments, activity and operation reservations. Domain commands use
transactions and optimistic versions; identifiers and versions are JSON strings.

Incidents have domain transitions, assignment, public replies, private notes,
files, history, watchers, relations, batch commands and incident SLA. SLA has
calendars and durable recalculation. Notification delivery and optional SMTP
are persisted. Catalog requests have REQ/RITM/SCTASK and manual fulfillment.
Published workflow definitions are versioned and executions pinned; participant
membership and decisions are checked by the server. User, tenant, RBAC and
session protections are existing foundations to retain.

Main at the start of this task has migrations 1–71. It has no production CMDB,
Problem or Change module. PR #23 contains a separate unfinished CMDB/asset
implementation. CMDB requires typed database and business-service classes,
support groups, lifecycle, additional relations and a complete user interface.
Assets and CI are separate concepts. Existing workflow is coupled to catalog
requests; Change requires an explicit adapter, not a second approval engine.

Existing record lists use independent markup and simple domain-specific filters.
There is no shared column configuration, validated nested filter editor or
group-shared saved view. Existing incident batch commands already check each
object and version and should be reused. Forms already retain drafts on version
conflicts and have navigation guards. Communication keeps independent public
and private drafts. These protections must survive workspace integration.

## Workspace behavior

The existing AppShell now contains compact global controls, permission-scoped
module navigation, record tabs, breadcrumbs and a collapsible context panel.
Tabs are deduplicated by domain URL and registered only after an authorized read.
Session metadata is bounded, held in memory, and discarded on logout or a change
of tenant/user. Known record access is rechecked when the session refreshes.
Internal notes and form drafts are never stored in workspace metadata or browser
storage. Scroll restoration is scoped to the tenant/user and cleared on logout.

Queue counts and rows share each domain's access predicate before pagination.
Counts are decimal strings and links use corresponding filters. Incident SLA risk
means an active incident with a recorded warning/breach on an active or paused
SLA instance; this is the latest persisted worker state, not a prediction.
Approvals count only current, pending decisions before their deadline.

Global search applies object access before returning metadata. It searches only
record numbers and permitted titles, excludes communication and submitted form
values, prioritizes exact numbers, and returns at most ten results of each type.
Input is bounded and wildcard characters are escaped for SQL LIKE.

## Shared record lists and forms

Incidents, requests and fulfillment tasks use a common record table and toolbar.
Columns can be hidden, reordered and resized; URL state includes presentation,
filters and sorting. A recursive AND/OR editor sends a strictly validated filter
structure with whitelisted fields/operators, bound SQL values, and depth/node
limits. Counts and grouping apply object access to the complete filtered dataset
before pagination. Keyset cursors support the selected sort.

Private views belong to their creator. Group views require current membership
and workspace.views.share to publish. Sharing only grants view configuration,
never record access. Creating a view uses operation reservations; updates and
removal require the owner's current version. Conflicts preserve the proposal and
require an explicit choice. Migrations 72–73 add the views and sharing permission.

Exports use current filters/access, cap at 10000 rows, omit private communication
and submitted form values, protect spreadsheet formulas, and preserve BIGINT as
text. Incident batch commands retain their domain validation. Task batches call
the existing assignment/transition command per selected version and report each
result. Requests and incidents share a record header and authorized row preview.
Keyboard focus stays in the preview and returns to its trigger. Task form refresh
keeps the draft's original version until a conflict is explicitly resolved.

Validation at this checkpoint: Go unit/OpenAPI tests, vet, build and race tests
passed; frontend 25 tests, typecheck and production build passed. New desktop
and mobile browser coverage passed for full-dataset filtering, grouping,
sort pagination, column order, saved views, authorized preview, formula-safe
CSV and group access revocation. Full integration passed on a separate database (47.369s), including rollback
and re-upgrade. Browser regression passed: 24 foundation/incident/workspace/list
cases plus four request/conflict cases across desktop and mobile. An initial combined run was
invalid because its rollback test reset browser fixtures and the running worker
consumed test events; this result is not counted as a passing regression.

## CMDB integration

The original unfinished CMDB/asset backend was copied into this worktree; its
source directory remains untouched. Its unpublished migrations are renumbered
74–82 after shared views. Migrations 83–84 add database class, support group,
criticality, lifecycle and containment. Historical migrations 1–73 are unchanged.
Every migration contains one SQL statement, matching the existing migration
runner. DATABASE and PART_OF data block a downgrade that would reject those
records; there is no automatic deletion to make a downgrade succeed.

The CMDB screen has a real list, server filter/sort/group/count, shared views,
CSV, authorized previews, a typed six-class form, version conflict comparison,
archive/restore reason and projected history. CI is distinct from business-service
class, catalog offering and financial Asset. The copied asset/import backend is
retained and tested; no incomplete standalone asset/import screen is published.

Directed edges validate class pairs. PART_OF is acyclic containment under the
existing tenant write lock; DEPENDS_ON can contain cycles. Both endpoints require
read access before lists/graphs expose metadata. Graphs cap depth at 5, nodes at
100 and edges at 200. Reverse relation names are shown explicitly. Creation and
link/relation retries use the shared operation reservation mechanism, audit and
outbox in one transaction. Archive preserves existing links and history.

Incident and request details have CI linking with a fresh authorized target
preview. CI related records apply each domain's own access predicate before
pagination; linking grants no read permission. CI references are added to
workspace tabs and permission revalidation.

CMDB validation: Go unit/OpenAPI tests, vet, build and race passed. Full MySQL
integration through schema 84 passed (52.403s), including migration rollback and
re-upgrade, existing catalog workflow, imports, tenant/owner isolation, graph
cycles/limits, typed database fields, support group, retry and containment.
Desktop/mobile browser flow passed for lost creation response, retained draft
on refresh/conflict, service dependency, incident link in both directions,
archive/reload preserving relations, paginated list and keyboard tabs. The flow
was repeated with CI/relation IDs above JavaScript's safe integer range. CI forms
and graphs were visually inspected at 1366, 1920 and mobile widths.

## Remaining implementation

1. Deliver Problem lifecycle, tasks, communication, links and Known Error.
2. Deliver Change domain adapter, version-bound approval, tasks, controlled
   lifecycle, templates, conflict-safe scheduling and time-window policies.
3. Extend authorized search/context, add operational reports and execute the
   complete ITSM scenario and installer/runtime/regression checks.

This checkpoint does not claim that stages D–F or the complete ITSM acceptance
scenario are done. Reference selectors will be extended for the new domains.
