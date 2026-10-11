#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ENV_FILE="${ROOT_DIR}/.env"
ENV_EXAMPLE="${ROOT_DIR}/.env.example"
STATE_DIR="${ROOT_DIR}/.opensnow"
RUN_DIR="${STATE_DIR}/run"
LOG_DIR="${STATE_DIR}/logs"
BIN_DIR="${ROOT_DIR}/bin"
WEB_PORT_FILE="${RUN_DIR}/web-port"
PROJECT_FILE="${RUN_DIR}/compose-project"
REQUESTED_WEB_PORT="${OPENSNOW_WEB_PORT:-}"
WEB_PORT=5173
PURGE_DATA=0
NO_START=0
BOOTSTRAP=0
ACTION=install
declare -a STARTED_COMPONENTS=() STARTED_SERVICES=()

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_GREEN=$'\033[32m'; C_BLUE=$'\033[36m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'; C_RESET=$'\033[0m'
else
  C_GREEN=''; C_BLUE=''; C_YELLOW=''; C_RED=''; C_RESET=''
fi
ok()   { printf '%s[ OK ]%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
info() { printf '%s[INFO]%s %s\n' "$C_BLUE" "$C_RESET" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
fail() { printf '%s[FAIL]%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 1; }
stage() { printf '\n%s[%s/7]%s %s\n' "$C_BLUE" "$1" "$C_RESET" "$2"; }

usage() {
  cat <<'EOF'
OpenServiceNOW local installer (Linux / WSL)

Usage:
  bash ./install.sh --local               Prepare, build and run this checkout
  bash ./install.sh --local --no-start    Prepare/build without running application processes
  bash ./install.sh --local --bootstrap   Create the administrator from .env instead of the WebUI
  bash ./install.sh --local --start       Start the previously built local runtime
  bash ./install.sh --local --stop        Stop API, Vite, worker, scheduler and local dependencies
  bash ./install.sh --local --restart     Restart the previously built local runtime
  bash ./install.sh --local --status      Show every component and the effective WebUI address
  bash ./install.sh --local --uninstall   Remove generated files, preserving .env and SQL data
  bash ./install.sh --local --uninstall --purge-data
                                         Also remove this instance's SQL volume and .env
  bash ./install.sh --help                Show this help

Without --local, existing commands use the same local mode for compatibility.
OPENSNOW_WEB_PORT selects the frontend port (default 5173); it is saved for later commands.
API remains on port 8080. Runtime state/logs are under this checkout's .opensnow/.
EOF
}

parse_args() {
  local selected='' argument
  for argument in "$@"; do
    case "$argument" in
      --local) ;;
      --start|--stop|--restart|--status|--uninstall|--help|-h)
        [[ -z "$selected" ]] || fail "Conflicting actions: ${selected} and ${argument}. Select exactly one action."
        selected=$argument
        ACTION=${argument#--}
        [[ "$argument" != -h ]] || ACTION=help
        ;;
      --no-start) NO_START=1 ;;
      --bootstrap) BOOTSTRAP=1 ;;
      --purge-data) PURGE_DATA=1 ;;
      *) fail "Unknown argument: ${argument}. Run bash ./install.sh --help." ;;
    esac
  done
  if (( NO_START || BOOTSTRAP )) && [[ "$ACTION" != install ]]; then
    fail "--no-start and --bootstrap apply only to installation; they cannot be combined with --${ACTION}."
  fi
  if (( PURGE_DATA )) && [[ "$ACTION" != uninstall ]]; then fail "--purge-data requires --uninstall."; fi
}

require_command() { command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1. $2"; }
version_at_least() { [[ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" == "$2" ]]; }

check_docker() {
  require_command docker "Install Docker Engine/Desktop with Compose v2."
  docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required."
  docker info >/dev/null 2>&1 || fail "Docker Engine is unavailable or this user cannot access it. Enable Docker and user access to its socket."
}

check_toolchains() {
  check_docker
  require_command go "Install Go 1.27 or newer."
  require_command node "Install Node.js 22.12 or newer."
  require_command npm "Install npm for the selected Node.js release."
  local go_version node_version
  go_version="$(go version | awk '{print $3}' | sed 's/^go//')"
  node_version="$(node --version | sed 's/^v//')"
  version_at_least "$go_version" 1.27 || fail "Go ${go_version} detected; Go >= 1.27 is required."
  version_at_least "$node_version" 22.12.0 || fail "Node.js ${node_version} detected; Node.js >= 22.12.0 is required."
  ok "Docker, Compose, Go ${go_version}, Node.js ${node_version} and npm are available."
}

ensure_state_dirs() { mkdir -p "$RUN_DIR" "$LOG_DIR" "$BIN_DIR"; }

load_env() {
  if [[ -f "$ENV_FILE" ]]; then
    set -a
    # shellcheck disable=SC1090 # operator-owned local configuration
    source "$ENV_FILE"
    set +a
  fi
}

valid_port() { [[ "$1" =~ ^[1-9][0-9]{0,4}$ ]] && (( 10#$1 <= 65535 )); }

load_instance() {
  local saved_root saved_project
  if [[ -f "${RUN_DIR}/instance-root" ]]; then
    saved_root="$(cat "${RUN_DIR}/instance-root")"
    [[ "$saved_root" == "$ROOT_DIR" ]] || fail "Local runtime state belongs to another checkout: ${saved_root}. Use a checkout without copied .opensnow state."
  fi
  WEB_PORT=5173
  if [[ -f "$WEB_PORT_FILE" ]]; then WEB_PORT="$(cat "$WEB_PORT_FILE")"; fi
  valid_port "$WEB_PORT" || fail "Invalid saved frontend port in ${WEB_PORT_FILE}."
  if [[ "$ACTION" != status && "$ACTION" != stop && "$ACTION" != uninstall && -n "$REQUESTED_WEB_PORT" ]]; then WEB_PORT=$REQUESTED_WEB_PORT; fi
  valid_port "$WEB_PORT" || fail "OPENSNOW_WEB_PORT must be an integer from 1 to 65535."
  [[ "$WEB_PORT" != 8080 ]] || fail "Frontend port 8080 conflicts with the API; choose another OPENSNOW_WEB_PORT."
  if [[ -f "$PROJECT_FILE" ]]; then
    saved_project="$(cat "$PROJECT_FILE")"
    if [[ -n "${COMPOSE_PROJECT_NAME:-}" && "$COMPOSE_PROJECT_NAME" != "$saved_project" ]]; then
      fail "COMPOSE_PROJECT_NAME differs from this instance's saved project (${saved_project}). Restore it before managing this instance."
    fi
    COMPOSE_PROJECT_NAME=$saved_project
  else
    COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-$(basename "$ROOT_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]//g; s/^[-_]*//')}"
  fi
  [[ "$COMPOSE_PROJECT_NAME" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || fail "COMPOSE_PROJECT_NAME must start with a lowercase letter/digit and contain only lowercase letters, digits, underscores or hyphens."
  export COMPOSE_PROJECT_NAME
}

save_instance() {
  printf '%s\n' "$ROOT_DIR" >"${RUN_DIR}/instance-root"
  printf '%s\n' "$COMPOSE_PROJECT_NAME" >"$PROJECT_FILE"
  printf '%s\n' "$WEB_PORT" >"${WEB_PORT_FILE}.tmp"
  mv -- "${WEB_PORT_FILE}.tmp" "$WEB_PORT_FILE"
}

prepare_env() {
  [[ -f "$ENV_EXAMPLE" ]] || fail ".env.example is missing from ${ROOT_DIR}."
  if [[ ! -f "$ENV_FILE" ]]; then cp -- "$ENV_EXAMPLE" "$ENV_FILE"; ok "Created .env from .env.example."; else info "Using existing .env."; fi
  chmod 600 "$ENV_FILE"
  load_env
  case "${HTTP_ADDR:-127.0.0.1:8080}" in
    127.0.0.1:8080|localhost:8080|0.0.0.0:8080|:8080|'[::]:8080') ;;
    *) fail "HTTP_ADDR must bind locally on port 8080, matching the Vite proxy." ;;
  esac
  [[ "${MYSQL_HOST:-127.0.0.1}" == 127.0.0.1 || "${MYSQL_HOST:-}" == localhost ]] || fail "Local mode requires MYSQL_HOST=127.0.0.1 or localhost."
  valid_port "${MYSQL_PORT:-3306}" || fail "MYSQL_PORT must be an integer from 1 to 65535."
  [[ "${REDIS_ADDR:-127.0.0.1:6379}" =~ ^(127\.0\.0\.1|localhost):([1-9][0-9]{0,4})$ ]] || fail "Local mode requires REDIS_ADDR=127.0.0.1:port or localhost:port."
  REDIS_PORT=${BASH_REMATCH[2]}
  valid_port "$REDIS_PORT" || fail "Redis port must be from 1 to 65535."
  export REDIS_PORT
  local mysql_port=${MYSQL_PORT:-3306}
  [[ "$mysql_port" != "$REDIS_PORT" && "$mysql_port" != 8080 && "$mysql_port" != "$WEB_PORT" && "$REDIS_PORT" != 8080 && "$REDIS_PORT" != "$WEB_PORT" ]] || fail "MySQL, Redis, API and frontend must use separate ports."
  [[ "${BOOTSTRAP_ON_START:-false}" == true || "${BOOTSTRAP_ON_START:-false}" == false ]] || fail "BOOTSTRAP_ON_START must be true or false."
  load_instance
}

compose() { (cd "$ROOT_DIR" && docker compose --project-directory "$ROOT_DIR" -p "$COMPOSE_PROJECT_NAME" -f "$ROOT_DIR/docker-compose.yml" "$@"); }

check_dependency_ownership() {
  local service id working_dir config_file
  for service in mysql redis; do
    id="$(compose ps -aq "$service")"
    [[ -n "$id" ]] || continue
    working_dir="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")"
    config_file="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$id")"
    [[ "$working_dir" == "$ROOT_DIR" && "$config_file" == "$ROOT_DIR/docker-compose.yml" ]] ||
      fail "Compose project ${COMPOSE_PROJECT_NAME} belongs to another checkout/configuration (${working_dir}); choose a separate COMPOSE_PROJECT_NAME in .env."
  done
}

container_health() {
  local id
  id="$(compose ps -aq "$1")"
  [[ -n "$id" ]] || return 1
  docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id"
}

wait_for_service() {
  local service=$1 state attempt
  for ((attempt=0; attempt<60; attempt++)); do
    state="$(container_health "$service" || true)"
    case "$state" in
      healthy) ok "${service} is healthy."; return 0 ;;
      unhealthy|exited|dead) fail "${service} entered state ${state}. Inspect: docker compose -p ${COMPOSE_PROJECT_NAME} -f \"${ROOT_DIR}/docker-compose.yml\" logs ${service}" ;;
    esac
    sleep 2
  done
  fail "Timed out waiting for ${service}; inspect its Compose logs."
}

start_dependencies() {
  check_dependency_ownership
  local service state
  for service in mysql redis; do
    state="$(container_health "$service" || true)"
    [[ "$state" == healthy || "$state" == starting || "$state" == running ]] || STARTED_SERVICES+=("$service")
  done
  compose up -d mysql redis
  wait_for_service mysql
  wait_for_service redis
  printf '%s\n' "$ROOT_DIR" >"${RUN_DIR}/instance-root"
  printf '%s\n' "$COMPOSE_PROJECT_NAME" >"$PROJECT_FILE"
}

process_stamp() {
  local proc_stat
  local -a fields
  [[ -r "/proc/$1/stat" ]] || return 1
  proc_stat="$(cat "/proc/$1/stat")"
  read -r -a fields <<<"${proc_stat##*) }"
  [[ "${fields[0]}" != Z ]] || return 1
  printf '%s %s\n' "${fields[19]}" "$(cat /proc/sys/kernel/random/boot_id)"
}

process_matches() {
  local pid=$1 component=$2 expected cwd executable
  [[ "$pid" =~ ^[1-9][0-9]*$ && -r "/proc/$pid/cmdline" ]] || return 1
  [[ "$(stat -c %u "/proc/$pid")" == "$EUID" ]] || return 1
  cwd="$(readlink "/proc/$pid/cwd")" || return 1
  local -a argv=()
  mapfile -d '' -t argv <"/proc/$pid/cmdline"
  if [[ "$component" == web ]]; then
    [[ "$cwd" == "$ROOT_DIR/web" && "${argv[1]:-}" == "$ROOT_DIR/web/node_modules/vite/bin/vite.js" ]]
  else
    expected="$BIN_DIR/opensnow-$component"
    executable="$(readlink "/proc/$pid/exe")" || return 1
    [[ "$cwd" == "$ROOT_DIR" && "${argv[0]:-}" == "$expected" && ( "$executable" == "$expected" || "$executable" == "$expected (deleted)" ) ]]
  fi
}

pid_from_file() {
  local pid stamp recorded_stamp
  [[ -f "$RUN_DIR/$1.pid" ]] || return 1
  read -r pid recorded_stamp <"$RUN_DIR/$1.pid" || return 1
  process_matches "$pid" "$1" || return 1
  stamp="$(process_stamp "$pid")" || return 1
  [[ -z "$recorded_stamp" || "$recorded_stamp" == "$stamp" ]] || return 1
  printf '%s\n' "$pid"
}
pid_running() { pid_from_file "$1" >/dev/null 2>&1; }

stop_pid() {
  local component=$1 pid stamp attempt
  if ! pid="$(pid_from_file "$component")"; then
    if [[ -f "$RUN_DIR/$component.pid" ]]; then warn "Stale/unrelated ${component} PID file; no foreign process will be stopped."; fi
    rm -f -- "$RUN_DIR/$component.pid"
    return 0
  fi
  stamp="$(process_stamp "$pid")"
  kill -TERM "$pid" 2>/dev/null || true
  for ((attempt=0; attempt<40; attempt++)); do
    if [[ "$(process_stamp "$pid" 2>/dev/null || true)" != "$stamp" ]]; then
      rm -f -- "$RUN_DIR/$component.pid"
      ok "${component} stopped."
      return 0
    fi
    sleep 0.5
  done
  if process_matches "$pid" "$component" && [[ "$(process_stamp "$pid")" == "$stamp" ]]; then kill -KILL "$pid" 2>/dev/null || true; warn "${component} required SIGKILL."; fi
  rm -f -- "$RUN_DIR/$component.pid"
}

port_busy() { [[ -n "$(ss -H -ltn "sport = :$1")" ]]; }
port_owned() {
  local pid
  pid="$(pid_from_file "$1")" || return 1
  [[ "$(ss -H -ltnp "sport = :$2")" == *"pid=$pid,"* ]]
}

check_ports() {
  local component port
  for component in api web; do
    port=8080; [[ "$component" != web ]] || port=$WEB_PORT
    if port_busy "$port" && ! port_owned "$component" "$port"; then
      fail "Port ${port} is occupied by another process/checkout; ${component} cannot start. Stop that process or choose another OPENSNOW_WEB_PORT for the frontend."
    fi
  done
  if [[ "$ACTION" == start ]] && pid_running web && ! port_owned web "$WEB_PORT"; then fail "This checkout's frontend is already running on another port. Use --local --restart to change its port."; fi
  check_dependency_ownership
  local service container_port mapped_port state
  for service in mysql redis; do
    port=${MYSQL_PORT:-3306}; container_port=3306
    if [[ "$service" == redis ]]; then port=$REDIS_PORT; container_port=6379; fi
    if port_busy "$port"; then
      state="$(container_health "$service" || true)"
      mapped_port="$(compose port "$service" "$container_port" 2>/dev/null || true)"
      [[ ( "$state" == healthy || "$state" == starting || "$state" == running ) && "$mapped_port" == "127.0.0.1:$port" ]] || fail "Port ${port} is occupied outside this instance's ${service} container. Choose another port in .env or stop the conflicting service."
    fi
  done
}

start_component() {
  local component=$1 cwd="$ROOT_DIR" pid attempt log_line
  if pid_running "$component"; then info "${component} is already running (PID $(pid_from_file "$component"))."; return 0; fi
  local -a command=("$BIN_DIR/opensnow-$component")
  if [[ "$component" == web ]]; then cwd="$ROOT_DIR/web"; command=(node "$ROOT_DIR/web/node_modules/vite/bin/vite.js" --host 127.0.0.1 --port "$WEB_PORT" --strictPort); fi
  touch "$LOG_DIR/$component.log"
  log_line=$(( $(wc -l <"$LOG_DIR/$component.log") + 1 ))
  (
    exec 9>&-
    trap - ERR EXIT INT TERM
    cd "$cwd"
    exec nohup "${command[@]}"
  ) </dev/null >>"$LOG_DIR/$component.log" 2>&1 &
  pid=$!
  printf '%s %s\n' "$pid" "$(process_stamp "$pid")" >"$RUN_DIR/$component.pid"
  STARTED_COMPONENTS+=("$component")
  for ((attempt=0; attempt<50; attempt++)); do
    if pid_running "$component"; then break; fi
    kill -0 "$pid" 2>/dev/null || fail "${component} exited during startup; see ${LOG_DIR}/${component}.log."
    sleep 0.1
  done
  pid_running "$component" || fail "Cannot confirm ${component} process ownership; see ${LOG_DIR}/${component}.log."
  if [[ "$component" == worker || "$component" == scheduler ]]; then
    for ((attempt=0; attempt<45; attempt++)); do
      pid_running "$component" || fail "${component} exited; see ${LOG_DIR}/${component}.log."
      if tail -n "+$log_line" "$LOG_DIR/$component.log" | grep "\"msg\":\"${component} started\"" >/dev/null; then ok "${component} started."; return 0; fi
      sleep 1
    done
    fail "${component} did not report readiness; see ${LOG_DIR}/${component}.log."
  fi
}

wait_for_http() {
  local component=$1 port=$2 path=$3 attempt
  for ((attempt=0; attempt<45; attempt++)); do
    pid_running "$component" || fail "${component} exited; see ${LOG_DIR}/${component}.log."
    if port_owned "$component" "$port" && curl --silent --fail --max-time 2 "http://127.0.0.1:${port}${path}" >/dev/null; then ok "${component} is ready."; return 0; fi
    sleep 1
  done
  fail "${component} did not become ready on port ${port}; see ${LOG_DIR}/${component}.log."
}
start_api() { start_component api; wait_for_http api 8080 /health/ready; }
start_web() { start_component web; wait_for_http web "$WEB_PORT" /; }

verify_runtime() {
  local component
  for component in api worker scheduler web; do pid_running "$component" || fail "${component} is not running; see ${LOG_DIR}/${component}.log."; done
  curl --silent --fail --max-time 3 "http://127.0.0.1:${WEB_PORT}/health/ready" >/dev/null || fail "Frontend /health proxy cannot reach the ready API."
  curl --silent --fail --max-time 3 "http://127.0.0.1:${WEB_PORT}/api/v1/setup" >/dev/null || fail "Frontend /api proxy cannot reach the setup API."
  save_instance
  ok "OpenServiceNOW is running: http://127.0.0.1:${WEB_PORT}"
  info "Runtime logs: ${LOG_DIR} (api.log, web.log, worker.log, scheduler.log)"
}

check_built_runtime() {
  local component
  for component in api worker scheduler; do [[ -x "$BIN_DIR/opensnow-$component" ]] || fail "Missing ${component} binary. Run bash \"${ROOT_DIR}/install.sh\" --local first."; done
  [[ -f "$ROOT_DIR/web/node_modules/vite/bin/vite.js" ]] || fail "Frontend dependencies are missing; run --local first."
}

start_runtime() { start_api; start_component worker; start_component scheduler; start_web; verify_runtime; }
stop_runtime() { local component; for component in web scheduler worker api; do stop_pid "$component"; done; }
start_all() { check_toolchains; prepare_env; check_built_runtime; check_ports; start_dependencies; start_runtime; }
stop_all() {
  # Local processes remain stoppable even when Docker Engine has gone offline.
  stop_runtime
  check_docker
  check_dependency_ownership
  compose stop redis mysql
  ok "Local processes and dependencies stopped; SQL data and .env preserved."
}

status_line() { case "$2" in healthy|ready|running) ok "$1: $2" ;; *) warn "$1: $2" ;; esac; }
show_status() {
  local component state failures=0 port
  check_docker
  check_dependency_ownership
  for component in mysql redis; do
    state="$(container_health "$component" || true)"; state=${state:-stopped}
    status_line "$component" "$state"
    [[ "$state" == healthy ]] || failures=$((failures+1))
  done
  for component in api worker scheduler web; do
    state=stopped
    if pid_running "$component"; then
      state=running
      if [[ "$component" == api || "$component" == web ]]; then
        port=8080; [[ "$component" != web ]] || port=$WEB_PORT
        state=unready
        if port_owned "$component" "$port" && curl --silent --fail --max-time 2 "http://127.0.0.1:$port/health/ready" >/dev/null; then state=ready; fi
      fi
    elif [[ -f "$RUN_DIR/$component.pid" ]]; then state=stale/unrelated; fi
    status_line "$component" "$state"
    [[ "$state" == ready || "$state" == running ]] || failures=$((failures+1))
  done
  info "WebUI address: http://127.0.0.1:${WEB_PORT}"
  info "Runtime logs: ${LOG_DIR}"
  (( failures == 0 ))
}

uninstall() {
  stop_all
  if (( PURGE_DATA )); then compose down -v; rm -f -- "$ENV_FILE"; ok "This instance's SQL volume and .env removed."; else compose down; info "SQL volume and .env preserved."; fi
  rm -f -- "$BIN_DIR/opensnow-api" "$BIN_DIR/opensnow-worker" "$BIN_DIR/opensnow-scheduler"
  rm -rf -- "$RUN_DIR" "$ROOT_DIR/web/dist" "$ROOT_DIR/web/node_modules"
  ok "Generated local runtime files removed; sources and logs preserved."
}

build_runtime() {
  local component temporary
  for component in api worker scheduler; do
    temporary="$(mktemp "$BIN_DIR/.opensnow-$component.XXXXXX")"
    if ! go build -trimpath -o "$temporary" "./cmd/$component"; then rm -f -- "$temporary"; fail "${component} build failed."; fi
    mv -- "$temporary" "$BIN_DIR/opensnow-$component"
  done
  (cd "$ROOT_DIR/web" && npm run build)
  ok "API, worker, scheduler and frontend builds completed."
}

install() {
  cd "$ROOT_DIR"
  stage 1 "Preflight"
  check_toolchains
  stage 2 "Configuration"
  prepare_env
  check_ports
  check_dependency_ownership
  stop_runtime
  stage 3 "MySQL and Redis"
  start_dependencies
  stage 4 "Dependencies"
  go mod download
  (cd "$ROOT_DIR/web" && npm ci --no-audit --no-fund)
  ok "Go and frontend dependencies installed."
  stage 5 "Database migration and account setup"
  go run ./cmd/migrate up
  if (( BOOTSTRAP )) || [[ "${BOOTSTRAP_ON_START:-false}" == true ]]; then go run ./cmd/bootstrap; ok "Explicit administrator bootstrap completed."; else ok "Schema ready; the WebUI creates the first administrator on a fresh installation."; fi
  stage 6 "Build"
  build_runtime
  stage 7 "Runtime"
  if (( NO_START )); then save_instance; info "Build completed without running application processes (--no-start). Use --local --start."; else start_runtime; fi
}

on_exit() {
  local result=$? component
  trap - ERR EXIT INT TERM
  if (( result != 0 )); then
    warn "Command failed. Runtime logs: ${LOG_DIR}"
    for component in "${STARTED_COMPONENTS[@]}"; do stop_pid "$component" || true; done
    if (( ${#STARTED_SERVICES[@]} > 0 )); then compose stop "${STARTED_SERVICES[@]}" >/dev/null 2>&1 || true; fi
  fi
  return "$result"
}

main() {
  parse_args "$@"
  if [[ "$ACTION" == help ]]; then usage; return; fi
  [[ "$(uname -s)" == Linux && -d /proc/self ]] || fail "Local runtime supports Linux and WSL. Run this script in your Linux/WSL shell."
  local command_name
  for command_name in flock ss curl nohup stat readlink awk sed sort; do require_command "$command_name" "Install standard Linux utilities (util-linux, iproute2, coreutils and curl)."; done
  ensure_state_dirs
  exec 9>"$RUN_DIR/installer.lock"
  flock -n 9 || fail "Another installer command is running for this checkout. Wait for it to finish."
  trap 'on_exit' EXIT
  trap 'printf "[FAIL] Command failed at install.sh line %s.\n" "$LINENO" >&2' ERR
  trap 'exit 130' INT
  trap 'exit 143' TERM
  load_env
  load_instance
  case "$ACTION" in
    install) install ;;
    start) start_all ;;
    stop) stop_all ;;
    restart) check_toolchains; prepare_env; check_built_runtime; check_ports; check_dependency_ownership; stop_runtime; start_dependencies; start_runtime ;;
    status) show_status ;;
    uninstall) uninstall ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
