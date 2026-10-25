# ADR-0003: Tenant configuration snapshots and typed custom values

## Status

Accepted for the Incident and CI metadata delivery.

## Context

Organizations need tenant-specific dictionaries, field definitions and form/list layouts, with draft/published history. Incident and CI already have domain tables and indexes that serve core queries. Replacing these tables with a universal EAV model would weaken the existing domain constraints and make common list queries harder to reason about. Custom values still need typed validation, filtering, uniqueness and tenant isolation.

## Decision

Store a complete validated configuration snapshot per tenant and version in MySQL JSON, with a tenant-scoped head pointing to the draft and published versions. Publish takes a row lock, checks the expected configuration version, validates references/layouts, checks existing values, applies supported safe conversions/backfills, and advances the published head transactionally.

Store each custom value in a separate row keyed by `(tenant_id, record_type, record_id, field_key)`. Keep text, long text, integer, decimal, boolean, date, datetime, JSON multi-choice and reference IDs in type-specific columns. DECIMAL(26,6) supports the configured maximum of 20 total field digits and six decimal places. Persist the field type and the configuration version used to validate the value. A normalized SHA-256 hash supports tenant/type/field-scoped uniqueness. Index typed filters by tenant, type, field and value; use prefix indexes for searchable text. Domain-owned Incident and CI fields stay in their existing columns and retain their domain constraints and authorization.

Only fixed record types and reference targets are accepted. Table names, SQL expressions and arbitrary code never come from tenant configuration. Searchable and filterable custom-field keys are checked against the published list layout before building parameterized queries. Database index plans should be checked against disposable MySQL data before increasing query complexity or adding new searchable types.

## Alternatives

### Move all record fields into a universal EAV store

Rejected because it would discard useful domain columns, complicate core constraints and make common record access depend on metadata joins.

### Keep custom values in an untyped JSON blob on each domain row

Rejected because type-specific indexes, unique-value enforcement and controlled conversion would be substantially weaker.

### Create physical database columns for every tenant-defined field

Rejected because tenant configuration would require DDL per publication, risk lock-heavy updates, and complicate downgrade and operational recovery.

## Consequences

Positive:

- Existing domain tables remain stable and authoritative for core fields.
- Custom values retain real numeric, date and reference types and can be indexed.
- Published configuration is immutable and pinned values can be interpreted with their schema version.
- Tenant and field predicates lead the custom-value key and filter indexes.
- Unique and safe small migrations can be checked before the publication transaction commits.

Negative and constraints:

- A fixed set of typed indexes cannot serve every arbitrary future report; new types and query shapes need deliberate schema and plan review.
- Large migrations are refused synchronously and need a future durable-job design before being supported.
- The current runtime adapter supports Incident and CI only; adding a record type requires a domain adapter and tests.
- JSON configuration snapshots are convenient for immutable reads but need full-document validation and optimistic concurrency on every write.
