# CMDB Identification and Reconciliation (IRE)

Module 06 adds tenant-scoped source identity, shared CI reconciliation, field provenance, conflict resolution, merge preflight, and scheduled data-quality checks. The supported input paths are the CMDB CSV importer and the IRE observation API. There is no discovery agent, network scan, or external connection provider in this module.

## Identity and source setup

Every external record is keyed by `(tenant, source, scope, source_native_key)`. `source_native_key` is a stable identifier assigned by the producing source; the business identifier, host name, IP address, and MAC address are not suitable substitutes for it. A source's `connection_ref` is a reference label only. Credentials and connection strings are not stored by IRE.

An IRE manager can select an active tenant CSV source when creating a CI import. The import pins that source ID and uses its configured scope for identity mapping; imports without an explicit selection continue to use the system CSV source and its legacy `default` scope. Only users with `cmdb.ire.manage`, `cmdb.manage`, and `cmdb.read` can choose a configured source. Disabling a selected source prevents a queued preview or apply from writing through it.

Identity rules are versioned per CI class. A rule can contain up to eight alternative key sets; fields within one set must all match. Values are normalized before comparison. Supported normalizers are `TRIM`, `LOWER_TRIM`, `UPPER_TRIM`, and `UPPER_MAC`. Volatile names, status, ownership, environment, location, host names, and IP addresses cannot be identity keys. The default physical-device rule tries serial number plus manufacturer, asset tag, then the CMDB identifier. Rules can require identity attributes and, when needed, a typed relation to a parent CI.

The matcher returns one of `SINGLE_MATCH`, `NEW_CI`, `INSUFFICIENT_DATA`, `MULTIPLE_CANDIDATES`, or `CONFLICT`. Ambiguous matches are never resolved by choosing the first row. A reviewer can explicitly attach a source identity to a CI from the conflict queue; that decision is tenant-scoped and retained with the actor and reason.

## Reconciliation and provenance

An observation includes `source_native_key`, optional `scope_key`, optional `parent_ci_id`, `observed_at`, and a `values` object keyed by CI fields such as `identifier`, `class`, `owner_id`, or `attributes.serial_number`. Batches accept 1–500 observations and are checked for duplicate source keys, field shape, timestamp range, and a 5 MiB combined values limit.

Create a run in dry-run mode first. A run stores its input hash, selected rule versions, per-row state, accepted and rejected field changes, and a durable job checkpoint. Repeating the same tenant/source/idempotency key and payload returns the existing run; reusing that key with a different payload is rejected. Apply can be retried from `PARTIAL`; already applied observations are not re-applied. Full snapshots only mark absent source mappings stale after a complete full run reaches `COMPLETED`. IRE never deletes a CI because it disappears from a source snapshot.

Per-field policies can select `AUTHORITATIVE`, `PRIORITY`, `FRESHEST`, or `FILL_EMPTY`. A lower numeric source priority wins. For equal priority, the newer observation wins. A delayed observation cannot replace a newer accepted value. Manual changes and explicit temporary or permanent overrides hold a value against connector updates unless the field policy explicitly allows them. Override changes require a reason; temporary overrides also have an expiry. `last_seen_at` records the latest observation independently from the time a field value was edited.

The CI detail view has a provenance tab listing each current field source, run, identity-rule version, observation time, acceptance time, and active override. The IRE page includes source administration, rule editing, field policies, a match tester, dry-run/apply progress, conflicts, merge preflight, and quality controls.

## Conflict resolution and CI merge

Identity conflicts can be resolved against an active CI of the observed class, or dismissed with a reason. A merge requires a fresh preflight token and the current versions of both CI records. The preflight lists known references and blocks class mismatch, two linked Assets, missing Asset write permission, duplicate/self relations, conflicting portfolio links, overlapping overrides, unresolved quality findings, and any populated foreign-key reference for which IRE has no merge adapter.

The merge moves supported live references, preserves CI/Asset one-to-one integrity and provenance history, stores the plan and reason, and leaves the source CI archived with a redirect. Old redirects are flattened to the final canonical CI; CI reads and lists resolve to that record. Historical observations remain tied to their original source row for audit.

## Quality rules and metrics

Quality checks run on a tenant schedule or through the “run now” action. Findings are stored by rule, CI, and field and can be filtered by class, owner, rule, or state. Repairs through CMDB edits or reconciliation re-evaluate applicable open findings; scheduled evaluation refreshes all findings.

| Rule | Denominator | Numerator (`valid`) |
| --- | --- | --- |
| `required_fields` | Active, canonical CIs | CIs with every configured required field present and non-empty |
| `duplicate_identity` | Active, canonical CIs | CIs not in an ambiguous duplicate group for their class's active identity rule |
| `owner_missing` | Active, canonical CIs | CIs whose owner is an active tenant member |
| `source_missing` | Active, canonical CIs | CIs with at least one external identity or recorded field provenance |
| `stale_ci` | CIs with at least one external source identity | CIs without a stale active source mapping beyond the configured stale-days threshold |
| `invalid_reference` | Non-empty custom reference fields on active, canonical CIs | References resolving to an existing record of the configured tenant-scoped type |
| `invalid_relation` | Stored CI relation edges | Edges permitted by the relation type's configured class pairs |

`open` is the current number of affected CIs for CI-level rules, and the number of invalid custom reference values or invalid edges for `invalid_reference` and `invalid_relation`. A full snapshot is the only input that ages missing source mappings. Disabled sources are excluded from stale detection.

## API and permissions

The endpoints are under `/api/v1/cmdb/ire`; the OpenAPI document lists methods and schemas. Mutating requests use the authenticated session and CSRF protection. `cmdb.ire.manage` is required for source/rule/policy administration, matching, runs, conflicts, and merge operations; write actions also require `cmdb.manage` and `cmdb.read`. Quality results require `cmdb.quality.read`, `cmdb.quality.manage`, or `cmdb.ire.manage` together with the caller's CMDB record-read scope. Configuration and manual quality runs require `cmdb.quality.manage` or `cmdb.ire.manage`, plus `cmdb.manage` and `cmdb.read`.

Migrations `000105`–`000123` add source/rule/run storage, external identities, observation checkpoints, field provenance and history, override/policy state, conflict and merge records, and quality configuration/findings/metrics. Back up tenant data before rolling back these migrations because their down scripts remove IRE data.

## Local verification

Local verification passed: wsl.exe go test ./..., go vet ./..., go build ./..., npm.cmd test (23 files/50 tests), npm.cmd run typecheck, and npm.cmd run build. Migration application/rollback, isolated-MySQL worker processing, concurrency/restart scenarios, and real API/desktop/mobile Playwright flows still require a disposable local MySQL stack and running app; they were not available in this environment.
