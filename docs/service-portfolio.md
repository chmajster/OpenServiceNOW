# Service portfolio and CSDM subset

This guide describes the tenant-scoped service portfolio in OpenServiceNOW. It models a useful subset of CSDM concepts and does not claim full ServiceNow CSDM conformance.

## Concepts

- **Business application**: a stable planning record.
- **Service instance**: an environment-specific deployed application or service stack.
- **Technical service**: an operational service managed by a technology provider.
- **Business service**: a consumer-facing service that can have one or more service offers.
- **Service offer**: a separately managed consumption option. A catalog item remains its own versioned order form and can be explicitly linked to one offer.

Portfolio records have stable tenant-scoped keys and optional, explicit links to existing `APPLICATION` or `SERVICE` CIs. Existing CI records are never reclassified automatically. The classification screen creates a draft portfolio record only after an operator chooses its type.

## Access and setup

The module uses local accounts, tenant memberships, the existing RBAC service, CMDB, Assets, Catalog, SLA definitions, audit, activity and transactional outbox. No external provider or module-specific secret is required.

Migration 000097 through 000104 adds the organization references, portfolio records, typed relations, availability observations, offers, audience groups and permissions. Platform administrators receive the new permissions. Other roles need the relevant permission assigned by an administrator:

- `service_portfolio.read`: read the tenant portfolio and impact graph.
- `service_portfolio.read_own`: read records where the user is a business or technical owner.
- `service_portfolio.manage`: manage records, relations, lifecycle, offers and availability.
- `service_portfolio.offer.read`: read offers available to the signed-in user.
- CI mapping and CI impact additionally require `cmdb.read` or `cmdb.read_own`; the API enforces both permission sets.

Organization units, departments and cost centers are tenant-scoped references maintained from the portfolio quality screen. Locations are selected from the existing IT Asset Management location catalog rather than copied as free text. Owners must be active members of the tenant; support groups, audience groups, catalog entries and SLA policies are validated against their owning modules.

## API and behavior

All routes are under `/api/v1/service-portfolio` and are documented in OpenAPI. The API provides filtered, cursor-paginated item and offer lists; item/history/typed-relation reads and writes; explicit CI classification preview and save; quality findings; lookups; lifecycle transitions; availability; retirement impact; and bounded impact graphs from an item or CI.

Items move through `DRAFT → PUBLISHED → ACTIVE → RETIRED`. Publication requires business and technical owners, an active support group, criticality and environment. Relations are tenant checked, class checked and version checked. `DEPENDS_ON` graphs reject cycles. Changes use optimistic versions and write audit and outbox records in the same transaction. Item and offer creation accept idempotency keys.

Impact responses are limited to 100 nodes, 200 edges and depth 6. They include visible paths, relation rules, redundancy rule version and calculation time, plus a truncation flag. CI and portfolio permissions are checked independently; invisible CI details are omitted from portfolio reads and graphs.

Availability is a manual, timestamped observation and remains separate from lifecycle and dependency relationships. `ALL`, `ANY` and `QUORUM` rules reduce observed dependency states; `NOT_MODELED` and unknown observations are reported explicitly. There is no automated discovery, monitoring integration or claim that the observation is continuously current.

Offers can restrict access to active tenant group members. Catalog visibility and submission are denied when a linked offer or parent service is inactive or withdrawn; previously submitted request history remains intact. Retirement preview reports active offers, linked catalog entries and visible active work before confirmation; retiring a service withdraws its active offers without deleting open requests or incidents.

Incident and Request links use the existing CMDB work-record adapter. Problems and Changes are reported as unavailable because those domains do not currently expose a compatible record-link contract. SLA is a reference to an existing policy, not a second SLA timer.

## Migration rollback

The supported `go run ./cmd/migrate down` and `down-all` commands inspect portfolio-owned tables before any rollback that drops them. They stop with the table name and an export/removal instruction while records remain, before the schema is marked dirty. Empty disposable installations can still roll back. Stop application writers while performing any migration operation.

## Verification boundary

The current module has backend unit/contract coverage, frontend tests, a production build and desktop/mobile Playwright coverage for creating and opening a draft. The current environment has not run these migrations against an isolated MySQL instance or exercised the complete API/database/worker flow. Problems and Changes remain explicitly unavailable; availability observations are manual; automated Discovery and full CSDM domains are outside this module.
