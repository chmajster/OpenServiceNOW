# CMDB CI inventory

## Available screens and behavior

The application exposes `/cmdb` for the CI list, `/cmdb/new` for creation, `/cmdb/:id` for direct-link details, and `/cmdb/imports` for CSV imports. List filters, search, sorting cursor and return URL are preserved in browser navigation. The class catalog, required attributes, types and enum values come from `GET /api/v1/cmdb/classes`; the browser does not maintain a second validation schema.

Built-in CI classes cover physical servers, virtual machines, network devices, user workstations, printers, storage devices, mobile devices, peripherals, UPS units, applications and services. Hardware classes expose typed manufacturer or vendor, model, serial and asset identifiers, plus category-specific details such as CPU/RAM, IP/MAC addresses, printer or storage type, firmware, rack position and UPS capacity. `CONNECTS_TO` records links between networked hardware and VMs. Link a CI to an Asset when purchase cost, warranty or financial lifecycle data is needed; those Asset fields remain the source for that information.

CI and Asset detail workspaces both show the linked record and let authorized users create or remove the one-to-one association. CI details also provide basic/class data, optimistic-lock edits, typed relations, bounded dependency-graph navigation, incident/request references, archive/restore and shared activity history. A relation or record link checks permissions at both ends. A link does not grant the viewer permission to open the other record. Asset financial or lifecycle state is not copied into the CI's operational status. Graph views represent manually recorded relations; they do not run Discovery or Service Mapping.

The import screen uses the persistent `inventory_imports` records and the existing `cmdb.import` worker job. Upload creates a preview and does not mutate CI or Asset records. An IRE manager can choose an active CSV source for CI imports; that source's scope separates its local identifiers from other sources. Users without source-management permission use the system CSV source. Apply uses the approved preview/version; users can review a changed plan, cancel an eligible import, and inspect progress and durable per-row outcomes after navigating away or refreshing. The backend contract publishes the active byte, row and column limits. Row-level conflicts are reported as outcomes rather than silently overwriting newer versions.

## Permissions

Grant permissions through the existing organization roles and RBAC administration. The API enforces these permissions; hiding a screen or action is only a usability aid.

| Capability | Required access |
| --- | --- |
| Read CI list/details, relations, graph and history | `cmdb.read` or `cmdb.read_own`; the server applies the corresponding record filter |
| Create, edit, archive or restore a CI; edit CI relations | `cmdb.manage` and `cmdb.read` |
| Read an Asset | `assets.read` or `assets.read_own`, with the Asset service's own object filtering |
| Create or change an Asset | `assets.manage` and `assets.read` |
| Link/unlink a CI and Asset from either detail workspace | `cmdb.manage` + `cmdb.read` and `assets.manage` + `assets.read`; both record versions and archive states are checked |
| Link/unlink a CI and an incident/request | `cmdb.link`, plus the incident/request domain's current authorization for that exact record |
| Import CI or Asset CSV | `cmdb.import` plus write access for the selected target (`cmdb.manage` + `cmdb.read` for CI; `assets.manage` + `assets.read` for Asset) |
| Select a configured CSV source for a CI import | `cmdb.ire.manage`, `cmdb.manage` and `cmdb.read`; otherwise the system CSV source is used |

The imports navigation is shown to users with the appropriate import and target read/manage permission pair. A durable job carries the tenant and executor identity established by the authenticated API. The worker reconstructs that principal through the shared work-record authorization check, then uses the same tenant-scoped domain commands; it does not trust tenant or actor data from a browser payload.

## Setup and migration

Manual CMDB use requires the normal application installation, an active local account, organization membership and the relevant permissions. No CMDB-specific environment variable, external connection or secret is required. Classes and allowed values are the built-in catalog returned by the API. The existing CSV importer is the supported bulk-entry path.

Local development applies pending schema changes through the existing migration runner:

```bash
make migrate-up
```

The native and Docker installers apply pending migrations during installation/upgrade. Migration `000090_cmdb_search_indexes` adds indexes to the existing `cmdb_cis` table for tenant-scoped name search and status/environment filters. Migration `000096_cmdb_hardware_classes` expands the allowed CI class values so physical device types can be stored. Neither migration adds records or renames identifiers. Apply the repository's pending migrations with the normal upgrade procedure; do not manually edit migration history.

The full API contract is in [`api/openapi/openapi.json`](../api/openapi/openapi.json), under the CMDB and Asset tag. Registered API routes include class metadata, CIs, relations/graph/history, Asset and incident/request links, lookups and durable imports. Source reconciliation, field provenance, conflict handling and quality controls are described in [`cmdb-ire.md`](cmdb-ire.md).

## Delivered boundary

This delivery makes the existing CI/Asset and import backend available through a complete CMDB user flow. It does not complete Phase 8. Software/license inventory, purchasing, warranty/vendor/cost-center processes, broader Asset lifecycle, dynamic class administration, automated Discovery, reconciliation of multiple data sources and Service Mapping remain future work. The current relation graph is bounded by the API's node, edge and depth limits.

## Local verification state

The current worktree's frontend unit tests, TypeScript checks, production build and scoped desktop/mobile Playwright flow are recorded in `STATUS.md`. The Playwright test stubs API responses. A real-MySQL migration lifecycle and browser flow against the API/worker still need local execution in an environment with Go, a running disposable MySQL/Docker Engine, and configured E2E credentials.
