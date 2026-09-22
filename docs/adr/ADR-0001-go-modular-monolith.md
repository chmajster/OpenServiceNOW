# ADR-0001: Go modular monolith as the initial backend architecture

## Status

Accepted

## Context

OpenServiceNOW is intended to become a large ITSM / IT Operations platform with many domains: incidents, requests, catalog, changes, problems, CMDB, assets, SLA, workflow, approvals, knowledge, notifications, integrations, automation, reporting, dashboards, and administration.

The system needs enterprise-grade separation, but the repository starts without an existing runtime implementation. Introducing many independently deployed services at this stage would add network contracts, distributed transactions, deployment complexity, tracing requirements, service discovery, retries, and operational overhead before domain boundaries have been validated.

The backend requirement is Go.

## Decision

Use Go as the backend implementation language and start as a modular monolith.

Runtime executables are separated by responsibility:

- `cmd/api`,
- `cmd/worker`,
- `cmd/scheduler`.

Business domains live under `internal/` with explicit module boundaries.

Each significant domain separates:

- transport,
- service/application logic,
- domain behavior,
- repository interfaces,
- infrastructure implementations,
- events,
- tests.

HTTP handlers do not contain core business rules. Business services do not depend on HTTP-specific types. SQL is isolated behind repository implementations.

Cross-domain collaboration uses explicit interfaces or domain/application events. Packages do not reach into another domain's database implementation.

The architecture must remain extractable: stable domain contracts, event contracts, and infrastructure adapters should allow selected modules to become separate services later if concrete scaling, reliability, security, ownership, or deployment requirements justify it.

## Alternatives

### Microservices from the beginning

Rejected for the initial architecture because domain boundaries and operational requirements are not mature enough to justify distributed-system complexity.

### Single unstructured Go application

Rejected because it would create high coupling and make later ownership, testing, and selective extraction difficult.

### PHP monolith

Rejected because the project requirement is a Go backend and the desired worker/concurrency/runtime model is designed around Go.

### Java/.NET enterprise platform

Technically viable, but rejected because it conflicts with the selected project stack and would not provide a concrete advantage that justifies changing the backend requirement.

## Consequences

Positive:

- simpler deployment and local development,
- straightforward transactions across closely related domains,
- lower early operational overhead,
- fast Go runtime and tooling,
- strong compile-time package boundaries when designed carefully,
- future service extraction remains possible.

Negative:

- architectural discipline is required to prevent accidental coupling,
- a single API deployment initially contains many domains,
- poor package design could turn the modular monolith into a tightly coupled monolith.

Mitigations:

- enforce dependency rules through package structure and code review,
- document module contracts,
- add architecture tests/static checks where useful,
- require ADRs for major cross-domain coupling or service extraction.
