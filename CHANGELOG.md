# Changelog

All notable repository changes are documented here.

The project is pre-release and currently in architecture/foundation development.

## 2026-09-22 — Phase 0 architecture

### Added

- target architecture for a Go modular-monolith backend,
- MySQL 8.x persistence strategy using database/sql and go-sql-driver/mysql,
- repository/service/transport layering rules,
- multi-tenant isolation architecture,
- backend-enforced RBAC direction,
- event, worker, scheduler, SLA, workflow, audit, and automation architecture,
- React + TypeScript frontend direction,
- security, concurrency, performance, testing, observability, and deployment principles,
- phased implementation roadmap,
- prioritized TODO register,
- factual current project status,
- ADR-0001 for Go modular monolith,
- ADR-0002 for MySQL persistence,
- documentation index.

### Runtime status

No runtime application was introduced in Phase 0. Go code, schema migrations, frontend code, Docker Compose, and tests are intentionally deferred to Phase 1 so the repository does not contain non-functional placeholders represented as complete.
