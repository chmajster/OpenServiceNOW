# Tenant data dictionary and configurable forms

## Available capability

The `/metadata` administration screen edits an organization's draft configuration. It includes the organization name, locale and display formats, currency and basic brand settings; the Incident and CI dictionaries and Incident priority matrix; custom field definitions; form sections and tabs; list columns, sorting, search and filters; and presentation views keyed by effective RBAC permissions. The preview uses the same published-schema contract as the consuming forms. Saving and publishing require `admin.metadata.manage`; the backend checks this permission again inside the transaction.

The runtime supports the Incident and CI record types. Their existing domain tables remain authoritative for core fields. Custom values are added to Incident and CI API representations as `custom_fields` with `custom_fields_version`. Supported custom types are `TEXT`, `LONG_TEXT`, `BOOLEAN`, `INTEGER`, `DECIMAL`, `DATE`, `DATETIME`, `CHOICE`, `MULTI_CHOICE` and `REFERENCE`. Reference targets are a fixed allowlist (user, group, incident, request, CI and Asset); lookup and save validation apply tenant and existing RBAC visibility. A form presentation view cannot grant data access.

Published custom values are validated by the metadata package when records are created or edited. Hidden fields retain their existing values during edits; a caller cannot use a hidden field to bypass type, option, reference, required or rule validation. CI import pins the metadata version and validates through the same schema. The catalog form adapter delegates value validation while preserving catalog's existing immutable published snapshots. List filtering accepts only `cf.<field_key>` entries declared filterable in the published list configuration. Search and filters use typed MySQL columns and tenant/type/field indexes rather than loading all custom values into application memory.

## Setup and permissions

Apply pending migrations using the normal upgrade path:

```powershell
make migrate-up
```

The metadata configuration receives tenant-scoped neutral defaults the first time the authenticated service reads it. The `platform_admin` system role receives `admin.metadata.manage` through migration 000092. To let a different role edit metadata, assign the permission through the existing RBAC screen. Then sign in and open **Administration → Data dictionary and forms** (`/metadata`). No external service, integration secret or private environment configuration is required.

An administrator can save a draft, inspect its impact, test a rule against sample input, publish it, compare configuration versions, and restore an older version as a new draft. Publish validates references and field/layout rules, checks stored values and dictionary use, and applies supported small value migrations in the same transaction as publication. A stale version returns `409`; the editor retains the local draft and requires an explicit reload or reconciliation. Retiring a field hides it from new forms and retains historical values.

## API

- `GET /api/v1/metadata/schema/{incident|ci}` returns the published schema and tenant configuration to actors allowed to read the corresponding domain.
- `GET /api/v1/metadata/references/{type}` returns bounded, permission-filtered reference results.
- `GET /api/v1/metadata/configuration` and `/impact` read the admin draft and its impact.
- `PUT /api/v1/metadata/configuration/draft`, `POST .../publish`, and `POST .../test` save, publish and validate configuration.
- `GET /api/v1/metadata/configuration/versions`, `/versions/{version}`, `/versions/{from}/compare/{to}`, and `POST /versions/{version}/restore` expose tenant-scoped history.
- `GET` and version-checked `PUT /api/v1/metadata/preferences` read and save a user's locale and time-zone override; responses also contain the published organization display settings.

BIGINT IDs and versions remain decimal strings in JSON. Writes use the existing session, CSRF, RBAC, audit and transactional outbox infrastructure. Metadata routes do not replace domain authorization. See [`api/openapi/openapi.json`](../api/openapi/openapi.json) for request and response schemas.

## Persistence and migrations

Configuration snapshots and their tenant-scoped draft/published head are stored in `metadata_config_versions` and `metadata_config_heads`. Custom values use one row per tenant, record type, record ID and stable field key in `metadata_record_values`. Text, long text, integer, decimal, boolean, date, datetime, JSON choices and reference IDs have separate typed columns; unique values use a normalized hash. Indexes begin with tenant, record type and field key and cover supported typed filters. Core domain data is not converted to a universal EAV table.

This module uses migrations 000086–000089 for configuration, typed values and user preferences; 000091–000092 for permission registration and the platform administrator grant; 000093 for Incident dictionary/search indexes; 000094 for long-text search; and 000095 to allow tenant-managed CI status and environment values. Migration 000090 belongs to the preceding CMDB delivery. The required schema version is 95.

Migration 000095's down migration restores the original CI status and environment checks. If a tenant has saved a value outside the original built-in sets, MySQL rejects the constraint restoration; the migration does not delete or rewrite that value. Preserve the database and correct the configuration/data before retrying a downgrade.

## Supported boundary and remaining work

This delivery is a shared dictionary foundation consumed by Incident, CI, the existing catalog validation adapter and CI import. It is not a general App Engine, metadata-powered workflow engine or full ServiceNow implementation. Runtime record types are currently Incident and CI. Field ACLs, team-specific views, reporting/export over arbitrary custom fields, full localization of every existing screen, large asynchronous backfills and automated schema migration jobs are not provided. Configured views only change presentation; domain RBAC remains authoritative. Basic organization branding is shown in the authenticated application header. External image URLs must use HTTPS and are constrained by the browser content-security policy.

The current local verification state, including the unavailable Go/MySQL environment and exact frontend results, is maintained in [STATUS.md](../STATUS.md). Run applicable backend and database checks when Go and an isolated MySQL service are available:

```powershell
make test
make lint
make build
make migrate-up
cd web; npm.cmd test; npm.cmd run typecheck; npm.cmd run build; npm.cmd run e2e
```
