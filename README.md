# OpenServiceNOW

OpenServiceNOW is an enterprise ITSM / IT Operations platform implemented independently from ServiceNow code, branding, and UI.

## Current state

The administration foundation, shared work platform and core Incident Management process are implemented. Further business modules continue according to the roadmap; an internal library does not imply an available HTTP endpoint or UI screen.

Implemented foundation capabilities:

- Go API runtime with graceful shutdown,
- validated environment configuration,
- structured JSON application/access logs and request IDs,
- MySQL 8.x through `database/sql`,
- Redis,
- explicit versioned migrations,
- liveness and schema-aware readiness,
- transaction manager,
- tenant/user/group/RBAC/session/audit schema,
- local password authentication with Argon2id,
- opaque database-backed sessions,
- CSRF double-submit validation,
- login rate limiting in Redis,
- backend permission evaluation,
- tenant/admin bootstrap command,
- first-run WebUI installer for atomic organization/administrator creation,
- cursor-paginated/searchable tenant user administration, including linking an existing active global identity to another tenant without changing its credentials/profile,
- tenant administration with optimistic locking,
- cursor-paginated/searchable RBAC administration for groups, roles, permissions and assignments,
- immutable system-role permissions, optimistic-lock role edits, dynamic superadmin permission inheritance, self-removal protection and a transactional guarantee that each tenant retains at least one active effective platform administrator,
- tenant-scoped paginated audit API,
- trusted-proxy-aware client IP resolution,
- optional MySQL/Redis TLS and forced UTC MySQL sessions,
- bounded Argon2 password verification concurrency,
- panic-safe transactions and JSON panic recovery,
- bounded session cleanup scheduled as durable MySQL jobs and executed by the worker,
- OpenAPI 3.1 contract,
- React + TypeScript administration frontend,
- Docker Compose development services,
- unit/integration/race/build/migration/authentication/RBAC/frontend CI definitions.

Durable MySQL-backed jobs, worker/scheduler execution and shared work-record infrastructure are implemented. Both the native local installer and the root DevBox Compose stack run API, frontend, worker and scheduler.

| Capability | Existing implementation | HTTP API | UI |
| --- | --- | --- | --- |
| First-run setup, authentication, tenants, users, RBAC and audit | Services, repositories and migrations | Documented in OpenAPI | Setup, login and administration screens |
| Jobs and scheduler | Durable jobs/attempts/logs, leases, retry/cancellation primitives; scheduler → worker session cleanup | No public job-management endpoints | No job-management screen |
| Work records, outbox, activity and attachments | Shared libraries, SQL persistence, worker projections and integration tests | Numbering, assignment and history through incidents; no generic work/file endpoints | Incident assignment/history; communication/files remain future work |
| Incident Management | Transactional lifecycle, numbering, RBAC, audit and worker history | Documented `/api/v1/incidents` routes | List, creation, details and lifecycle actions |
| SLA and other roadmap business modules | Remaining roadmap work | Not available | Not available |

## Incident Management

Open **Incydenty** to search/filter the server-paginated list or **Zgłoś incydent** to report a problem. **Moje**, **Mojej grupy** and **Nieprzypisane** apply the same object authorization as the full list. Filters, sort cursors and detail tabs are stored in the URL. An agent selects an active organization member/group in **Przypisanie**, then uses **Stan** to start work, hold, resolve and close. Choices search independently of the first 50 results and support keyboard selection/pagination.

The organization and reporter come from the backend session. Numbers such as `INC0000001` are unique per organization. Creation requires `Idempotency-Key`: the same normalized body/key returns the original incident; changing the body returns 409. Updates use the decimal-string incident version. Conflicts retain the draft and require an explicit decision against the current state; delayed responses for another incident are ignored. **Sprawdź wynik poprzedniej próby** reconciles a lost creation response with its original body/key. Pending form drafts/keys survive errors within the mounted form, not closing/reloading it.

Impact and urgency use 1 (high), 2 (medium), 3 (low); P1 is the highest priority:

| Impact × urgency | 1 | 2 | 3 |
| --- | --- | --- | --- |
| 1 | P1 | P2 | P3 |
| 2 | P2 | P3 | P4 |
| 3 | P3 | P4 | P5 |

An override requires `incident.priority.override`, a reason and an audited separate action. Changing impact/urgency restores the matrix priority. Category/subcategory pairs and resolution codes come from the implemented domain catalog returned by metadata.

| Current state | Allowed next states |
| --- | --- |
| NEW | IN_PROGRESS, ON_HOLD, CANCELLED |
| IN_PROGRESS | ON_HOLD, RESOLVED, CANCELLED |
| ON_HOLD | IN_PROGRESS, RESOLVED, CANCELLED |
| RESOLVED | CLOSED, IN_PROGRESS |
| CLOSED | IN_PROGRESS |
| CANCELLED | Terminal |

Holding/cancelling requires a reason; resolving requires a code and description. Starting/resolving requires an active assigned user, active organization membership and membership in the selected active group. Reopening requires a reason and emits `incident.reopened`; it clears the current resolution/closure fields and preserves their historical values. Closed/cancelled records cannot otherwise be edited or assigned.

| Permission | Access |
| --- | --- |
| `incident.create` | Create, together with `incident.read` or `incident.read_own` |
| `incident.read` | Read incidents in the current organization |
| `incident.read_own` | Read incidents reported by or affecting the actor |
| `incident.update` | Edit fields and perform other allowed lifecycle transitions |
| `incident.assign` | Change assignment |
| `incident.resolve` | Resolve and close |
| `incident.priority.override` | Set/reset an audited priority override |

Writes also require read access to the concrete incident. Create requester/agent roles in **Role** and assign them to users/groups; the protected `platform_admin` inherits the live catalog. Read-own reporters can select themselves as the affected person; agents with organization-wide read can select other active organization members.

Incident, required shared assignment row, audit, request digest and outbox event are persisted atomically in MySQL. Every emitted incident event has a production worker handler. **Historia** shows before/after changes and business occurrence time/version, ordered by source event rather than projection completion. While the worker is unavailable, records remain saved and history reports pending processing; **Odśwież historię** reads the latest state. Communication, files, CMDB/service relations, SLA, templates, bulk actions and major/parent-child workflows are subsequent stages with no unfinished controls in this release.

## Administration panel

The Polish administration navigation separates Users, Roles, Groups, Organization, Audit and Diagnostics. Roles and groups have searchable, paginated tables and deep-linked details: role information/permissions/assignments and group information/members/role assignments. Creation forms open through an explicit action. User creation shows the created account even when it falls beyond the first list page; linking an existing global identity preserves its profile and password.

User, group and role comboboxes search the real tenant-scoped API and have their own `after_id` pagination. Use arrows, Home/End and Enter to select, Escape to close and Tab to reach pagination/clear controls. Selections survive searches and page loading. User identities require `admin.users.manage` in addition to `admin.rbac.manage` on the relevant RBAC operations.

List filters and detail tabs are stored in the URL. Reads are bound to the current object/query, cancelled on changes and guarded against late responses. Assignment mutations are disabled while the current state is loading or failed. Organization and role-permission version conflicts retain the local proposal, display the current server state and require an explicit choice before another save. A failed conflict refetch leaves the proposal available and saving blocked until the current state can be read.

Protected HTTP 401 responses clear the shared session and navigation. Login, organization updates and permission/membership mutations refresh the backend context; returning to the window and the server-provided expiry also trigger a session check. A late response from a previous login cannot restore a revoked session. Diagnostics lives in administration and reports existing API/MySQL/Redis readiness; background process status remains an installer capability.

Audit filters run on the server: inclusive RFC3339 `from`/`to`, decimal `actor_user_id`, exact `action`, `object_type`, `object_id` and `request_id`, combined with bounded `before_id` pagination. The UI accepts dates in the browser's local timezone, shows field differences and retains raw JSON/request metadata in technical details. New role-permission replacements record both sides in their write transaction; historical events without a prior snapshot cannot reconstruct missing values. No new database migration is required.

## Stack

Backend:

- Go 1.27,
- `net/http` + chi,
- `database/sql` + `go-sql-driver/mysql`,
- MySQL 8.x,
- Redis.

Frontend:

- React,
- TypeScript,
- React Router,
- Vite,
- same-origin cookie session + CSRF model.

## One-command local installer

OpenServiceNOW includes an executable `install.sh` for the repository's current local runtime. It does not change or depend on DevBox orchestration rules.

Requirements:

- Docker Engine/Desktop with Docker Compose v2,
- Go 1.27 or newer,
- Node.js 22.12 or newer with npm,
- Bash on Linux or WSL, curl, `flock`/`setsid` (util-linux), `ss` (iproute2) and standard Unix tools,
- access to a running Docker Engine as your ordinary user; no systemd or application service registration is needed.

Install, migrate, build and start the current stack:

```bash
bash ./install.sh --local
```

The installer uses the exact checkout containing `install.sh`, including uncommitted source changes. It creates local `.env` from `.env.example` when missing, explicitly starts MySQL/Redis with `docker-compose.yml`, installs Go/npm dependencies, applies migrations, builds API/worker/scheduler into local `bin/`, builds the frontend and runs all four application processes in independent background sessions that survive closing the installer's terminal. Vite runs directly from `web/` in non-interactive mode and proxies `/api` and `/health` to the selected API port. It verifies process ownership, database/cache readiness and the actual frontend proxy before reporting success. It does not move or clone sources, modify Git, or use DevBox configuration.

Open the displayed WebUI address (normally `http://127.0.0.1:5173`). On a fresh database, the first-run wizard creates the organization and administrator. You can use `admin` / `admin`, or choose your own email and password. Existing configuration, administrator credentials and SQL data are preserved. Re-running `--local` stops this checkout's old processes, rebuilds current source and starts the new runtime. `--no-start` prepares/builds everything and leaves application processes stopped.

For unattended installation, use `bash ./install.sh --local --bootstrap` or set `BOOTSTRAP_ON_START=true` and supply `BOOTSTRAP_*` values in `.env`. Headless bootstrap closes the WebUI installer too. The default is `BOOTSTRAP_ON_START=false`; ordinary start/restart never recreates an administrator.

Operational modes:

```bash
bash ./install.sh --local --status
bash ./install.sh --local --stop
bash ./install.sh --local --start
bash ./install.sh --local --restart
bash ./install.sh --local --no-start
bash ./install.sh --local --uninstall
bash ./install.sh --local --uninstall --purge-data
```

Choose another frontend port on install or restart:

```bash
OPENSNOW_WEB_PORT=8090 bash ./install.sh --local
bash ./install.sh --local --status
bash ./install.sh --local --restart
```

The effective frontend port is stored in `.opensnow/run/web-port`, so later commands use the same address without repeating the variable. On first start, the API tries 8080, then 8081, 8082 and so on, through **8100 inclusive**. Ports reserved for this instance's frontend/MySQL/Redis are skipped. If no eligible port is available, the command fails with a concrete error. The selected API port is saved in `.opensnow/run/api-port` and displayed by startup/status; later commands reuse it when available, or search 8080–8100 again when it has been occupied by another process. A running API is reused by `--start`. Both Vite proxies follow the effective API port, including when an existing Vite process needs restarting after the API port changes. Keep `HTTP_ADDR` in `.env` on base port 8080; the installer overrides only its port for the API process without rewriting `.env`.

Frontend/database/cache conflicts still report an error. The installer never stops foreign listeners. Set `MYSQL_PORT` and `REDIS_ADDR` in `.env` if the infrastructure's default loopback ports 3306/6379 are occupied. Configure matching database credentials there before first provisioning; changing a password in `.env` does not reset accounts in an existing MySQL volume. For checkouts with the same directory name, use a distinct `COMPOSE_PROJECT_NAME` in `.env`; existing Compose project names/volumes remain compatible and container ownership is checked against the checkout path.

The script also accepts an absolute path from another directory, including paths with spaces, e.g. `bash "/mnt/c/Projects/OpenServiceNOW local/install.sh" --local`. Logs are `.opensnow/logs/api.log`, `web.log`, `worker.log` and `scheduler.log`; separate PID/start-time records live in `.opensnow/run/`. Start is idempotent; stop preserves data and affects only this checkout's verified processes and infrastructure. Failed startup removes only processes/dependencies started by that invocation and reports log locations. Status returns a nonzero exit code for an incomplete/unready runtime. Ordinary uninstall preserves `.env`, SQL volume, source files, logs and unrelated `bin/` files; `--purge-data` explicitly removes this instance's database volume and `.env`. Previous commands without `--local` remain aliases for the same local mode. Conflicting lifecycle actions/options are rejected.

`--status` only reads saved configuration, process identity and readiness; it creates no runtime directories, changes no accounts and enqueues no jobs. It reports every local process with its PID and log even if Docker is unavailable. API/frontend `ready` means the synchronous API, schema and Redis checks passed; worker/scheduler `running` means the verified process is alive. It does not prove processing or measure queue lag. A stopped worker makes overall status fail while synchronous API requests and login can remain available. Failed startup prints process state before and after rollback, including components preserved from an earlier start.

Run the real lifecycle verification with `make local-test` (Linux/WSL, Docker, Go, Node.js, Python 3 and build utilities). It copies tracked working-tree files into a temporary checkout with spaces and uses a separate Compose project and disposable MySQL volume. It correlates scheduler JSON job IDs with worker attempts/results/logs, proves expired-session deletion, read-only status, duplicate prevention, API/proxy port changes, restart/session persistence, partial-start rollback, bootstrap password protection and ordinary uninstall/reinstall preservation. The `local-runtime` CI job runs this suite; failure artifacts include lifecycle and per-process logs.

### Local startup troubleshooting

If preflight cannot access Docker, run `docker info` as your ordinary user. With a local Linux Docker Engine, a socket owned by `root:docker` requires membership in that group:

```bash
sudo usermod -aG docker "$(id -un)"
newgrp docker
docker info
bash ./install.sh --local
```

Opening a new WSL terminal also reloads group membership. Keep the installer and application processes under your ordinary user. For Docker Desktop, enable the engine and WSL integration for your distribution first.

If another MySQL service owns port 3306, choose a free port in this checkout's `.env`, for example `MYSQL_PORT=3307`; the installer uses that value for both the container mapping and the Go database connection. For a Redis conflict, set e.g. `REDIS_ADDR=127.0.0.1:6380`. Before editing, check that the proposed ports are free with `ss -ltn '( sport = :3307 or sport = :6380 )'`. Preserve the existing database credentials and run the installer again. A port change does not require stopping another application's database or deleting volumes. Local API conflicts are handled automatically within 8080–8100.

## Local Docker installation and updates

Build and run the entire application in containers:

```bash
bash ./install.sh --local-docker
```

This mode first installs or updates the local Docker Engine, Compose and Buildx packages on Ubuntu/Debian. It uses `sudo` only for package/service management, adds the ordinary user to the Docker group and refreshes group access when necessary. Existing apt installations keep their package family (`docker.io` or `docker-ce`); a fresh system uses available distribution Compose v2 packages or Docker's signed stable apt repository. It installs the newest versions available in the configured repositories and refuses package removal. Package upgrades can restart the Engine. Engine data, images, volumes and configuration are preserved. See the [Ubuntu packages documentation](https://ubuntu.com/server/docs/how-to/containers/docker-for-system-admins/) and official [Docker Ubuntu](https://docs.docker.com/engine/install/ubuntu/) / [Debian](https://docs.docker.com/engine/install/debian/) installation instructions.

Engine service startup requires systemd or a supported existing service manager. In WSL, enable systemd for a new native Engine installation and keep the distribution running while using the application; [systemd services alone do not keep WSL alive](https://learn.microsoft.com/en-us/windows/wsl/systemd). For Docker Desktop, a remote engine or another OS/package manager, manage the Engine there and run `bash ./install.sh --local-docker --skip-docker-update`. Other lifecycle commands reuse the installed Engine without updating system packages.

The installer pulls MySQL/Redis images and builds the backend/frontend images from this exact checkout with refreshed base images. Go and Node/npm run inside image builds; they are not required on the host. A build failure leaves the running application available. After successful builds it stops this checkout's previous application, starts infrastructure, applies migrations, optionally performs explicit `--bootstrap`, and starts API, Nginx frontend, worker and scheduler with readiness checks. Fresh databases show the account creation wizard; updates preserve users, passwords, organizations and SQL data.

The WebUI automatically selects an available loopback port in **8080–8100**, saves it in `.opensnow/run/docker-port`, and displays its address. `OPENSNOW_DOCKER_PORT=8090` selects a specific port on installation/restart; an occupied explicit port reports an error. The API is available only through the frontend's `/api` and `/health` proxy. MySQL/Redis retain the loopback mappings configured in `.env`, allowing the same named SQL volume and credentials to be reused when switching between `--local` and `--local-docker`. Each mode's application processes are stopped before the other mode starts, avoiding duplicate workers. Switch modes by installing or restarting with the desired flag; start/status/stop/uninstall require the recorded mode.

```bash
bash ./install.sh --local-docker --status
bash ./install.sh --local-docker --restart
bash ./install.sh --local-docker --stop
bash ./install.sh --local-docker --start
bash ./install.sh --local-docker --no-start
bash ./install.sh --local-docker --uninstall
```

Re-run `--local-docker` after source changes to rebuild and update the application. `--no-start` prepares images and schema while leaving application containers stopped. Restart reuses existing images. Logs are saved as `.opensnow/logs/docker-*.log`. Failed startup stops only components started by that invocation. Uninstall removes this checkout's containers/network and preserves `.env`, SQL volume, Engine and image cache; adding `--purge-data` explicitly removes its SQL volume and `.env`. DevBox's `compose.yaml` remains independent; the local Docker installer explicitly combines `docker-compose.yml` with `docker/compose.local.yaml`.

## DevBox deployment

The root `compose.yaml` packages the complete application: React assets served by a non-root Nginx reverse proxy, Go API, worker, scheduler and a private Redis cache. It uses a shared DevBox MySQL server with an application-specific database/account. The existing `docker-compose.yml` remains the infrastructure-only development stack selected explicitly by `make dev` and `install.sh`.

In DevBox, add this local source and choose Docker Compose. Select `frontend` as the HTTP service, container port `8080`, a free host port and readiness path `/health/ready`. Leave the start command empty. Create/bind a MySQL database with a separate application account and schema migration privileges. Deploy and open the application's address to create the first administrator in the WebUI. For unattended setup, set `BOOTSTRAP_ON_START=true`, `BOOTSTRAP_ADMIN_PASSWORD` in SecretStore, and the public `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_NAME` and tenant variables as needed.

DevBox injects `DB_*` credentials at runtime; the backend entrypoint maps them to `MYSQL_*`. API startup applies the explicit migrations and accepts traffic for the WebUI installer; administrator bootstrap runs only when explicitly enabled. Redis/API readiness gates frontend, worker and scheduler startup. The frontend proxies `/api/` and `/health/` to the API on the same origin, preserving session cookies and CSRF checks. Only the frontend port is published; SQL data stays in DevBox's persistent MySQL volume.

Go 1.27 and Node build tooling run inside multi-stage Docker builds, so host interpreters are unnecessary for this deployment. Original sources remain visible through DevBox's RW mounts; rebuild the application after changing Go or frontend code. Secrets, Git metadata, local dependencies and build/test artifacts are excluded from the image build context. The HTTP example uses development cookie settings; configure TLS and secure cookies when deploying HTTPS.

## Local development commands

Start MySQL and Redis:

```bash
make dev
```

Create local configuration:

```bash
cp .env.example .env
```

Load the environment:

```bash
set -a
source .env
set +a
```

Apply migrations:

```bash
make migrate-up
```

Create the administrator through the WebUI after starting both services below. Alternatively, set the desired `BOOTSTRAP_*` values and run `make bootstrap` for headless setup. Bootstrap is idempotent for tenant membership, role and permission assignment. Re-running it does not reset the password of an existing user.

Start the backend:

```bash
go run ./cmd/api
```

Start the frontend in a second shell:

```bash
cd web
npm ci
npm run dev
```

For the complete stack, run `make worker` and `make scheduler` in separate shells too, loading `.env` in each shell. Alternatively, `make local-install` builds and starts all four processes; `make local-start`, `make local-stop`, `make local-restart` and `make local-status` manage the installed native runtime.

The Vite development server listens on `127.0.0.1:5173` and proxies `/api` and `/health` to `127.0.0.1:8080` by default. `OPENSNOW_API_PORT` changes the proxy's loopback target port; the local installer sets it automatically from the selected/saved API port.

### Initial login

On an empty installation, `/login` and protected pages automatically open `/setup`. The wizard creates the organization, administrator, membership, platform-admin permissions, audit event and permanent completion marker in one database transaction. Invalid input or a failed write leaves setup available for retry. Only one concurrent setup can succeed; subsequent attempts return `409 SETUP_COMPLETED`. Completion survives application restarts and account deletion. Migration 25 marks deployments with existing users or organizations as already installed without changing their credentials.

`GET /api/v1/setup` reports `required` and, while setup is available, issues an HttpOnly, SameSite=Strict installer cookie plus a response token. `POST /api/v1/setup` requires same-origin JSON and that token in `X-Setup-Token`, is rate limited, and shares the login password-hashing concurrency limit. No administrator/session API is exposed through the wizard after installation.

For the default account, choose tenant `default`, login `admin` and password `admin`. The login alias `admin` resolves to the stored account email `admin@example.test`; either identifier works. The API retains the `email` field for compatibility:

```json
{
  "tenant": "default",
  "email": "admin",
  "password": "admin"
}
```

The default password is accepted only when installing or bootstrapping the default administrator account and requires a password change on first login. New passwords require at least 12 Unicode code points and at most 1024 UTF-8 bytes in both API and UI; their Unicode representation is not normalized. Custom passwords selected by the first-run administrator are permanent. Headless bootstrap can explicitly mark a newly created custom account as temporary with `BOOTSTRAP_REQUIRE_PASSWORD_CHANGE=true`. Every account created through user administration receives a temporary password. These policies apply to new accounts only: migrations, restart and re-bootstrap preserve existing passwords and flags. Explicit re-bootstrap requires the current password.

### My account

Every authenticated user can open **Moje konto** to edit their global display name, change the password with the current password, and inspect/revoke their own active sessions across organizations. Login email changes, MFA, LDAP and OIDC are outside this stage. Session lists expose organization, creation/expiry, IP and browser metadata, never tokens. Lists use bounded cursor pagination. Profile writes use the decimal-string user version and retain the local draft on errors or conflicts.

Password changes atomically increment the credential version, revoke all other sessions, clear the temporary-password flag and rotate the current session token and CSRF token in place. Concurrent login checks the credential version again under the global user lock before inserting a session. Old-password verification cannot create a session after a committed password change. A repeated request carrying the old session or credential version cannot change the password twice. Failures before commit preserve credentials and sessions. A lost response or SQL commit acknowledgement has an uncertain outcome (`ACCOUNT_RESULT_UNKNOWN` for a commit error). Reload `/me` to reconcile, or log in with the new password if the old session was rejected. Do not retry password changes automatically.

An account requiring a password change can only use authenticated `GET /api/v1/auth/me` (empty permissions), `POST /api/v1/auth/password` and `POST /api/v1/auth/logout`. Backend middleware enforces this for every authenticated module, including direct API calls. Organization administrators can manage membership and revoke sessions within their organization when disabling membership; membership authority cannot reset global credentials or revoke sessions in another organization.

The server sets:

- `opensnow_session` — HttpOnly session cookie,
- `opensnow_csrf` — readable CSRF cookie.

Unsafe authenticated requests require the same CSRF value in the `X-CSRF-Token` header. Cookie names are protocol constants shared by backend, frontend and OpenAPI.

### Reverse proxy client IP

The backend ignores forwarded IP headers unless the direct peer belongs to a CIDR configured in:

```text
TRUSTED_PROXY_CIDRS
```

Example for an internal reverse-proxy network:

```text
TRUSTED_PROXY_CIDRS=10.20.0.0/16,fd00:20::/64
```

When the direct peer is not trusted, rate limiting and audit use the canonical direct peer IP, preventing a client from spoofing `X-Forwarded-For`. Trust-all CIDRs are rejected.

### Datastore TLS

Development defaults to loopback plaintext connections. Production deployments can enable verified TLS with:

```text
MYSQL_TLS_MODE=true
REDIS_TLS_ENABLED=true
REDIS_TLS_SERVER_NAME=redis.internal.example
```

Compatibility modes such as MySQL `skip-verify` or Redis `REDIS_TLS_INSECURE_SKIP_VERIFY=true` are explicit operator choices. MySQL sessions always force UTC for consistent `DATETIME` handling.

## Endpoints

Health:

```text
GET /health/live
GET /health/ready
```

Authentication:

```text
POST /api/v1/auth/login
GET  /api/v1/auth/me
POST /api/v1/auth/logout
```

Administration currently covers:

- current tenant,
- local tenant users,
- RBAC permissions/roles/groups/assignments,
- tenant audit history.

Tenant scope is derived from the authenticated session, never from a client-supplied tenant identifier.

The OpenAPI document is stored at:

```text
api/openapi/openapi.json
```

## Development commands

```bash
make build
make test
make lint
make migrate-up
make migrate-down
make migrate-down-all
make migrate-version
make bootstrap
make worker
make worker-once
make scheduler
make scheduler-once
make frontend
make frontend-test
make e2e
```

`make migrate-down` rolls back one migration. `make migrate-down-all` rolls the schema back to no applied version. Readiness requires the exact current migration version and rejects dirty or stale schemas.

### Browser E2E

`make e2e` runs the Playwright suite against an already running local API/frontend. Before using it locally:

1. start MySQL/Redis and apply migrations,
2. leave the disposable database empty to test first-run account creation (an existing E2E account is also supported),
3. start the API and worker (`make worker`, needed for incident history),
4. start Vite on `127.0.0.1:5173`,
5. export `E2E_TENANT`, `E2E_ADMIN_EMAIL`, `E2E_ADMIN_PASSWORD`, `E2E_OPERATOR_EMAIL`, and `E2E_OPERATOR_PASSWORD`,
6. install Chromium once with `cd web && npx playwright install chromium`,
7. run `make e2e`.

CI performs infrastructure/migration/startup steps automatically in its dedicated `e2e` job. The first browser test creates the E2E tenant/admin through the real WebUI; subsequent tests use that account. Run the suite only against a disposable test database.

When testing a fresh default `admin` password, the setup scenario completes its required change before the remaining tests. Suite-created accounts also complete the temporary-password flow. Test login helpers respect real `Retry-After` limits instead of weakening authentication policy.

## Verification status

Incident Management in [PR #17](https://github.com/chmajster/OpenServiceNOW/pull/17) passed local Go unit/race/vet/build, disposable MySQL integration and migration lifecycle 1–32, 15 frontend unit tests, TypeScript/build and 24/24 desktop/mobile browser scenarios. Additional 4/4 isolated scenarios verify priority override/reset and a retained proposal after concurrent closure. Coverage includes concurrent/idempotent creation, lost-response reconciliation, object/tenant isolation, all nine real worker projections and out-of-order history. An actual API/worker process restart preserved UI-created incidents, numbers, versions, assignments, history and saved login. Native lifecycle CI also verifies those records through port changes and ordinary uninstall/reinstall. Final CI/review/merge results are recorded in PR #17.

Own-account implementation in [PR #16](https://github.com/chmajster/OpenServiceNOW/pull/16) passed local Go unit/race/vet/build, disposable MySQL integration and migration lifecycle 1–27, 13 frontend unit tests and 22/22 desktop/mobile browser scenarios. Tests cover credential-version fencing, global session rotation/revocation, restricted temporary accounts, profile conflicts, rollback on audit failure, restart persistence and legacy Unicode credentials. The PR records final CI and merge evidence; [STATUS.md](STATUS.md) distinguishes these delivered APIs/screens from internal shared libraries and future providers.

The preceding installer revision passed all six jobs in [foundation-ci #600](https://github.com/chmajster/OpenServiceNOW/actions/runs/38189545233): backend/migrations/integration, frontend, desktop/mobile E2E, native local lifecycle, Docker lifecycle and Docker Engine tests. September runner-allocation failures are historical and do not describe the current CI state.

The first-run installer was additionally verified against an isolated MySQL/Redis/API/Nginx stack: all 25 migrations, preservation of existing credentials on upgrade, transactional rollback, concurrent setup and permanent closure after restart/account removal passed. Desktop/mobile browser coverage includes setup validation, expired installer session, network retry, stale-window conflict, account creation, login, RBAC and audit. The implementation and final checks are recorded in [PR #9](https://github.com/chmajster/OpenServiceNOW/pull/9).

The native lifecycle fixes and expanded known-ID scheduler/worker verification in [PR #14](https://github.com/chmajster/OpenServiceNOW/pull/14) passed all six jobs in [foundation-ci #606](https://github.com/chmajster/OpenServiceNOW/actions/runs/38237188306). The complete native lifecycle also passed locally in an isolated environment, including actual expired-session deletion and ordinary uninstall/reinstall preservation. See [STATUS.md](STATUS.md) for the checked executable revision, results and capability limits.

## Architecture and project state

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [ROADMAP.md](ROADMAP.md)
- [TODO.md](TODO.md)
- [STATUS.md](STATUS.md)
- [docs/adr](docs/adr)

Repository working rules are defined in [AGENTS.md](AGENTS.md).
