# OpenServiceNOW

OpenServiceNOW is a planned enterprise ITSM / IT Operations platform with its own implementation and interface, targeting a functional class similar to large service-management platforms without copying ServiceNow code, branding, or UI.

## Current state

The project is in **Phase 0 — Architecture**.

There is not yet a runnable application. The repository currently defines the architecture, implementation roadmap, project status, backlog, and core technical decisions.

Do not treat missing runtime commands as defects in Phase 0 and do not add fake startup instructions before the corresponding runtime exists.

## Target stack

Backend:

- Go,
- net/http + chi,
- database/sql,
- go-sql-driver/mysql,
- explicit SQL migrations,
- Go worker,
- Go scheduler.

Data:

- MySQL 8.x as the primary durable system of record,
- Redis for cache, locks, rate limits, temporary coordination, and job support.

Frontend:

- React,
- TypeScript,
- API-driven architecture.

Development/operations direction:

- Docker Compose for local dependencies,
- reverse proxy for production,
- Kubernetes-compatible deployment model later.

## Architecture

See:

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [ROADMAP.md](ROADMAP.md)
- [TODO.md](TODO.md)
- [STATUS.md](STATUS.md)
- [docs/adr](docs/adr)

## Intended product scope

The roadmap includes:

- Incident Management,
- Request Management,
- Service Catalog,
- Change Management,
- Problem Management,
- CMDB,
- Asset Management,
- Knowledge Base,
- SLA,
- Workflow,
- Approvals,
- Automation,
- Notifications,
- Integrations,
- Reporting,
- Dashboards,
- Self-Service Portal,
- Agent Workspace,
- Administration,
- Audit,
- RBAC,
- multi-tenancy,
- webhooks,
- background jobs,
- dynamic forms.

The project is intentionally delivered in phases. A module is not considered complete until its applicable code, database migrations, API, RBAC, tenant isolation, UI, error handling, tests, and documentation are complete.

## Contributing / agent workflow

Repository working rules are defined in [AGENTS.md](AGENTS.md).

Every implementation task should use a dedicated branch and Draft Pull Request, with the PR description kept current throughout the work.
