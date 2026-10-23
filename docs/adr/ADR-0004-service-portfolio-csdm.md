# ADR-0004: Service portfolio records beside the CMDB

## Status

Accepted.

## Context

The existing CMDB has `APPLICATION` and `SERVICE` CI classes. Their current attributes do not distinguish an application planning object, an environment-specific deployment, a technology management service, a consumer-facing business service, and a service offering. Existing `SERVICE` records therefore cannot be classified safely from their class alone.

The CSDM conceptual model describes technology management services as provider-focused operational CIs, service instances as deployed application stacks, and business services as consumer-focused services published to users. Service offerings belong to the consumption side and are separate from the service itself. This module adopts those distinctions as product concepts without claiming complete CSDM conformance.

## Decision

- Keep existing `APPLICATION` and `SERVICE` CIs intact and keep their identifiers and history.
- Store business applications, service instances, technology management services, and business services as tenant-scoped portfolio records. A record can reference a CI, but its stable `service_key` and identity do not depend on CI name or owner.
- Store service offerings in a separate table. A catalog item may be mapped to one offer, but remains its own versioned order form.
- Require an explicit, version-checked classification when an existing `SERVICE` or `APPLICATION` CI is mapped into the portfolio. Never infer its meaning from the old class.
- Store typed relations with class checks and reject cycles for `DEPENDS_ON`.
- Treat availability observations as time-stamped facts separate from lifecycle and dependency edges. Calculations report the rule version and calculation time; unobserved dependencies remain unknown.
- Use the shared identity, RBAC, audit, transactional outbox, activity and catalog services. Do not create a second SLA timer or monitoring connector.

## Consequences

Service identity survives renames and owner changes. Portfolio users can model service relationships without changing historical CI records. The supported migration command checks each portfolio table before a rollback that would drop it and refuses while tenant data remains, without marking the schema dirty. Empty disposable installations can still roll back normally.
