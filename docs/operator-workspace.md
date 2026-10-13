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

## Problem Management

Migrations 85–89 add Problems, analytical tasks, explicit incident/CI link tables
and domain permissions. PRB and PRBTASK use the existing transactional allocator.
The Problem module has server-filtered/shared-view lists, typed forms, analytical
narratives, documented Known Error, controlled assessment/analysis/fix/resolution/
closure/cancellation/reopening, current tenant/object RBAC and optimistic versions.
Resolution/closure requires verification and completed required analytical tasks.
Assignments reuse work_assignments and validate active tenant users/groups.

Incidents provide an explicit authorized public-field transfer preview. Creation
and the incident link/touch commit atomically; private messages/files are never
copied. Related lists and CMDB links apply each domain's access independently.
A resolved/closed Problem never changes any incident's state. The remedial Change
creation action opens an explicit authorized public-field preview; both related
lists retain independent object access checks.

Messages and files reuse shared storage/repositories/reservations. problem.notes
is checked after object access and before INTERNAL messages, files or source-event
history are returned. Audit excludes message bodies; the activity worker projects
privacy-tagged events with author/time and actual field differences. Files have
message-derived visibility, safe downloads and durable cleanup. Retry keys,
business changes, parent/child versions, audit and outbox are transactional.

Forms preserve drafts on refresh and compare current records on version conflict.
The shared editor now retries an uncertain mutation using the exact original body,
path, method and key. New Problem forms and analytical tasks hold their drafts in
memory; no private communication is persisted in browser storage. Navigation,
workspace tabs and access revalidation include Problems.

Problem validation: Go unit/OpenAPI tests, vet/build/race passed; frontend 26 tests,
typecheck and production build passed. Full isolated MySQL integration through
schema 89 passed (80.051s), including fresh installation, rollback/re-upgrade,
existing workflows, concurrent edits, permission revocation, foreign-tenant access,
private messages/files, Known Error, required-task gates and unchanged incidents.
An initial run had an invalid incident fixture and a migration test timeout; the
fixture was fixed, and the unchanged migration test subsequently passed in 39/36s.
Desktop/mobile browser flows passed through creation-response loss, public incident
preview, bidirectional incident/CI links, analytical work, private note, verification,
closure/reload, history and multi-page list counts. Repeated with PRB/task identifiers
above the JavaScript safe integer range. Screenshots inspected at 1366/1920/mobile.

## Change backend checkpoint

Migrations 90–98 add versioned Standard templates, CHG records/CHGTASK tasks,
Problem/CI links, explicit workflow Change subjects, calendar rules and RBAC.
Historical migrations and Request execution identities are preserved. Change
uses the existing durable engine through a typed adapter; no generic domain
write permission or second automation engine was introduced. Every evaluation
pins an immutable public snapshot, record version and assessment revision.
Material edits invalidate stamps and pending executions without deleting their
decisions. Stale jobs and decisions cannot authorize the replacement revision.

Normal requires assessment before authorization. Standard uses an independently
approved immutable template with exact plans/scope/group/workflow/CI constraints,
risk ceiling and duration ceiling, plus a real approval for each instance.
Emergency requires urgency justification and its own typed definition, whose
decision deadline cannot exceed 24 hours. Self approval is denied for Change.
Tasks start/complete only during Implement; required completion, actual execution
instants and the recorded review outcome gate closure. Problem/incident states
are never closed as a side effect.

Scheduling checks shared-CI overlap and service-window/blackout rules atomically
under the existing tenant lock. Half-open boundaries permit adjacent changes.
Protected records expose no identifiers, titles or dates in collision details;
an explicit override requires separate permission, reason and audit. Dates are
stored as UTC instants with a separately validated IANA display zone. Calendar
queries are bounded to 93 days/1000 records. Template/rule administration is
versioned, permission checked and idempotent.

Backend validation: Go unit/OpenAPI, vet and build passed. Full isolated MySQL
integration through schema 98 passed (64.094s), including migration lifecycle,
installer, retained catalog workflow, duplicate decisions/creation/tasks, scope
invalidation, durable waits, concurrent reservations and edits, protected
collision details, boundaries/windows/blackouts, task execution/review/closure,
Standard/Emergency rules, private notes and revoked permissions. A prior run
encountered leftover fixtures from a failed test; the rollback correctly refused
to discard Change executions, and the successful full run used a fresh disposable
test schema. Actual API, worker and scheduler restart was verified during a pending approval;
the execution identity and deadline survived and one task was executed.

## Change operator screens

The shared workspace exposes Changes, CAB, the bounded month/week calendar,
service windows/blackouts, versioned Standard templates and typed approval
configuration. Forms use real domain commands, frozen retry keys, preserved
conflicting drafts and human-readable comparisons. Problem-to-Change transfer
shows public fields before creation; CI and Problem links are bidirectional.
Calendar forms resolve IANA gaps/ambiguities explicitly rather than guessing UTC.
CAB loads actual decisions/collisions/history when a row is expanded.

Validation: frontend 31 tests, typecheck and production build passed. Targeted
Change MySQL integration passed (6.195s). Normal desktop and mobile browser flows
passed through public Problem preview, lost creation/link responses, assessment,
independent approval, scheduling, required task execution, review, closure,
worker history, related links and real calendar-rule create/deactivation. The
mobile flow also verified whole-set counts and pagination on 26 additional rows.
Standard/Emergency desktop and mobile flows passed: independently approved
immutable template, exact CI scope, real instance approval and emergency rejection.
Identifiers exceed JavaScript's safe integer range. A pending Normal approval was
held while all three actual processes restarted; the same execution/deadline
continued without duplicate tasks. A later history assertion in that run was
corrected and the complete browser flow subsequently passed.

## Authorized search, context and reports checkpoint

Stage F provides permission-scoped metadata for eleven object types, exact-number
search, stable parent/task/item URLs and batched access revalidation. Search does
not index private notes or confidential catalog form values. Record context reads
source and targets independently, limits each section and links to complete lists.
Reference selectors retrieve an exact selected identifier under the same policy
used by their search; public previews load only when opened.

Operational reports aggregate the entire authorized set in a repeatable-read
snapshot. They cover active states/groups, disjoint backlog ages, actual durable
incident SLA risk, Problems ranked by independently readable linked incidents,
and reserved/closed Changes. Definitions distinguish snapshot and UTC half-open
time ranges. Counts drill into freshly authorized, cursor-paginated source lists;
no SLA is invented for requests, Problems or Changes. Aggregate groups are bounded,
as are search, context, metadata, report periods and source page sizes.

Validation: Go unit/OpenAPI tests, vet/build and the three relevant MySQL domain
integration tests passed (7.388s). Frontend 31 tests, typecheck and production build
passed. Desktop (9.1s) and touch mobile (8.3s) browser flows verified 31 Problems
across pages 25+6, refresh, exact BIGINT task search, public selected-reference
preview, independently readable CI/Change context, report source drilldown and
absence of invented Change SLA. A mobile aggregate-table width issue was found
and fixed; the repeat mobile flow passed and its screenshot was inspected.

## Combined acceptance and policy review

The shared review corrected retained uncertain related-list commands, readable
Problem/task/Request/workflow comparisons and the Request CI tab. The owner has
one navigation blocker; exact retry bodies/keys survive another record tab. My
workspace now includes current-assignee and active-own-group Problem/Change queues
whose counts match domain-filtered lists. Implementation rechecks current CI,
workflow/template and calendar policy under the tenant lock. A new blackout after
reservation blocks without changing state/version/timestamps; a separate current
permission and audited reason can override it. MySQL verifies denied and allowed
exceptions plus replay.

Full fresh isolated MySQL integration passed through schema 98 (63.231s), including
installer/migration lifecycle, retained Request workflow, current queues, all domain
privacy/concurrency/retry gates and the new execution policy. Go unit/OpenAPI, race,
vet/build and frontend 31 tests/typecheck/build passed.

The combined desktop acceptance passed (2.7m): administrator used real forms to
create CI and Business Service and a dependency; operators received scoped roles;
an operator created an incident, opened it from its queue, linked CI and added a
private note; public preview created a Problem with recorded analysis; a remedial
Change pinned another person's actual decision; a required task was executed,
reviewed and closed; the Problem was verified and closed; the incident stayed NEW.
API, worker and scheduler actually restarted while approval waited. The same
execution/deadline survived and no duplicate task/effect appeared. Five CMDB,
Problem, Change, Standard/Emergency and report touch-mobile flows passed (1.9m).

An early combined test expected incident creation to redirect and looked for a
message in the history selector. It was corrected to open the actual confirmation
link and assert the real message. Screenshot review then found a queue definition
that did not wrap and a Change capture before lazy loading completed; wrapping and
capture readiness were corrected. Final screenshot inspection remains pending.

## Remaining implementation

1. Finish the 38-scenario existing desktop/mobile browser regression.
2. Complete isolated native/Docker installer update/lifecycle checks, final screenshots
   at 1366/1920/mobile, review final diffs and update all verification summaries.
3. Push the final accurate PR/documentation state. No automatic merge is authorized.
