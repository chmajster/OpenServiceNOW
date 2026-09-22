# ADR-0002: MySQL 8.x with database/sql and explicit migrations

## Status

Accepted

## Context

OpenServiceNOW requires a relational system of record for transactional ITSM data, tenant isolation, workflow state, approvals, audit, CMDB data, and other strongly related business entities.

The project requires MySQL. Go does not use PHP MySQLi; the standard database abstraction is `database/sql`.

The data layer must support explicit SQL, transactions, connection-pool management, context cancellation, migrations, indexes, constraints, and testable repository boundaries.

## Decision

Use MySQL 8.x as the primary relational database.

Go access uses:

- `database/sql`,
- `github.com/go-sql-driver/mysql`.

Business services depend on repository interfaces rather than MySQL driver APIs.

Default persistence strategy:

- explicit parameterized SQL,
- context-aware query methods,
- repository implementations per domain,
- centralized transaction abstraction,
- versioned up/down SQL migrations,
- configuration-driven connection pool.

Heavy ORM usage is not part of the baseline. Tools such as `sqlc` or a lightweight query builder may be introduced if they provide measurable maintainability or type-safety benefits without hiding critical SQL behavior.

Schema changes are never applied only through application startup logic. Production schema state is controlled through migration files.

Redis may support cache, locks, rate limiting, temporary coordination, and jobs, but it is not a durable business system of record.

## Alternatives

### PostgreSQL

Technically suitable, but rejected because the project explicitly selects MySQL as its primary database.

### GORM with AutoMigrate

Rejected as the default because the project requires explicit control over production schema evolution and query behavior.

### Raw driver usage throughout services

Rejected because it couples business logic to persistence and makes testing and later data-layer changes harder.

### Redis as primary storage

Rejected because ITSM business state requires durable relational consistency, constraints, transactions, and queryability.

## Consequences

Positive:

- explicit control over SQL and indexes,
- mature MySQL operational ecosystem,
- predictable transaction semantics,
- compatibility with MySQL-native tooling,
- easier query-plan analysis,
- database/sql keeps business code separate from a concrete driver.

Negative:

- more SQL and repository code must be maintained,
- developers must actively manage scanning, nullability, transactions, and schema migrations,
- careless SQL can still create N+1 patterns or missing tenant predicates.

Mitigations:

- repository tests against MySQL,
- tenant isolation tests,
- migration tests,
- query review and EXPLAIN for hot paths,
- reusable transaction manager,
- static/lint checks where practical,
- consistent error mapping around `sql.ErrNoRows` and database conflicts.
