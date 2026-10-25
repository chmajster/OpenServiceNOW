# Documentation

- [Incident process and Major Incident](incidents.md): tenant policies, coordination, communication, shared actions, review, migration compatibility and local verification.

This directory contains durable technical and architectural documentation for OpenServiceNOW.

## Core project documents

At repository root:

- `ARCHITECTURE.md` — target system architecture and invariants,
- `ROADMAP.md` — phased implementation sequence and exit criteria,
- `TODO.md` — prioritized actionable backlog and in-progress register,
- `STATUS.md` — current factual repository state,
- `CHANGELOG.md` — notable repository changes,
- `AGENTS.md` — repository working rules for coding agents.

## Architecture Decision Records

ADRs are stored under `docs/adr/`.

Current decisions:

- `ADR-0001-go-modular-monolith.md` — Go + modular monolith,
- `ADR-0002-mysql-persistence.md` — MySQL 8.x + database/sql + explicit migrations,
- `ADR-0003-tenant-metadata-storage.md` ? tenant configuration snapshots and typed custom-value storage.
- `ADR-0004-service-portfolio-csdm.md` — portfolio records beside the CMDB, with explicit CI mapping and typed relations.

## Module guides

- [`metadata.md`](metadata.md) â€” tenant data dictionaries, configurable forms, permissions, API, migrations and current boundary,
- [`cmdb.md`](cmdb.md) â€” CI inventory and import behavior.
- [`service-portfolio.md`](service-portfolio.md) — CSDM concepts, permissions, API, migration and integration boundaries.
- [`workspace.md`](workspace.md) — operator queue, saved views, record editors, permissions, exports and current boundaries.

## ADR format

Each ADR should contain:

- Context,
- Decision,
- Alternatives,
- Consequences.

Accepted decisions remain immutable in intent. If a decision changes, add a superseding ADR rather than silently rewriting history.

- cmdb-ire.md - CMDB identity, reconciliation, provenance, merge and quality controls.
