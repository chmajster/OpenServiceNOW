# Documentation

This directory contains durable technical and architectural documentation for OpenServiceNOW.

## Core project documents

At repository root:

- `ARCHITECTURE.md` — target system architecture and invariants,
- `ROADMAP.md` — phased implementation sequence and exit criteria,
- `TODO.md` — prioritized actionable backlog and in-progress register,
- `STATUS.md` — current factual repository state,
- `cmdb.md` — CMDB CI inventory permissions, import operation, migration and delivered boundaries,
- `docs/cmdb.md` — CMDB CI inventory permissions, import operation, migration and delivered boundaries,
- `CHANGELOG.md` — notable repository changes,
- `AGENTS.md` — repository working rules for coding agents.

## Architecture Decision Records

ADRs are stored under `docs/adr/`.

Current decisions:

- `ADR-0001-go-modular-monolith.md` — Go + modular monolith,
- `ADR-0002-mysql-persistence.md` — MySQL 8.x + database/sql + explicit migrations.

## ADR format

Each ADR should contain:

- Context,
- Decision,
- Alternatives,
- Consequences.

Accepted decisions remain immutable in intent. If a decision changes, add a superseding ADR rather than silently rewriting history.
