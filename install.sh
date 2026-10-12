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
API_PORT_FILE="${RUN_DIR}/api-port"
PROJECT_FILE="${RUN_DIR}/compose-project"
REQUESTED_WEB_PORT="${OPENSNOW_WEB_PORT:-}"
WEB_PORT=5173
API_PORT=8080
PURGE_DATA=0
NO_START=0
BOOTSTRAP=0
ACTION=install
MODE=local
SKIP_DOCKER_UPDATE=0
MODE_FILE="${RUN_DIR}/runtime-mode"
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
  bash ./install.sh --local-docker        Install/update Docker Engine and the containerized app
  bash ./install.sh --local-docker --skip-docker-update
                                         Build/update the app using an already managed engine
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
API tries ports 8080 through 8100, saving the selected port and updating Vite's proxy.
Runtime state/logs are under this checkout's .opensnow/.
All lifecycle options also work with --local-docker. Go/Node are built inside images.
Docker mode publishes the WebUI on the first available port from 8080 through 8100.
Engine installation/upgrades support apt-managed Ubuntu/Debian engines and require sudo.
EOF
}

parse_args() {
  local selected='' selected_mode='' argument
  for argument in "$@"; do
    case "$argument" in
      --local|--local-docker)
        [[ -z "$selected_mode" || "$selected_mode" == "$argument" ]] || fail "Conflicting modes: ${selected_mode} and ${argument}."
        selected_mode=$argument
        MODE=${argument#--}
        ;;
      --start|--stop|--restart|--status|--uninstall|--help|-h)
        [[ -z "$selected" ]] || fail "Conflicting actions: ${selected} and ${argument}. Select exactly one action."
        selected=$argument
        ACTION=${argument#--}
        [[ "$argument" != -h ]] || ACTION=help
        ;;
      --no-start) NO_START=1 ;;
      --bootstrap) BOOTSTRAP=1 ;;
      --purge-data) PURGE_DATA=1 ;;
      --skip-docker-update) SKIP_DOCKER_UPDATE=1 ;;
      *) fail "Unknown argument: ${argument}. Run bash ./install.sh --help." ;;
    esac
  done
  if (( NO_START || BOOTSTRAP )) && [[ "$ACTION" != install ]]; then
    fail "--no-start and --bootstrap apply only to installation; they cannot be combined with --${ACTION}."
  fi
  if (( PURGE_DATA )) && [[ "$ACTION" != uninstall ]]; then fail "--purge-data requires --uninstall."; fi
  if (( SKIP_DOCKER_UPDATE )) && [[ "$MODE" != local-docker || "$ACTION" != install ]]; then fail "--skip-docker-update requires --local-docker installation."; fi
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
valid_api_port() { valid_port "$1" && (( 10#$1 >= 8080 && 10#$1 <= 8100 )); }

load_instance() {
  local saved_root saved_project pid argument port previous=''
  if [[ -f "${RUN_DIR}/instance-root" ]]; then
    saved_root="$(cat "${RUN_DIR}/instance-root")"
    [[ "$saved_root" == "$ROOT_DIR" ]] || fail "Local runtime state belongs to another checkout: ${saved_root}. Use a checkout without copied .opensnow state."
  fi
  WEB_PORT=5173
  if [[ -f "$WEB_PORT_FILE" ]]; then
    WEB_PORT="$(cat "$WEB_PORT_FILE")"
  elif pid="$(pid_from_file web 2>/dev/null)"; then
    # Adopt the effective port of a verified Vite process from the older installer.
    while IFS= read -r -d '' argument; do
      if [[ "$previous" == --port ]]; then WEB_PORT=$argument; break; fi
      previous=$argument
    done <"/proc/$pid/cmdline"
  fi
  valid_port "$WEB_PORT" || fail "Invalid saved frontend port in ${WEB_PORT_FILE}."
  if [[ "$ACTION" != status && "$ACTION" != stop && "$ACTION" != uninstall && -n "$REQUESTED_WEB_PORT" ]]; then WEB_PORT=$REQUESTED_WEB_PORT; fi
  valid_port "$WEB_PORT" || fail "OPENSNOW_WEB_PORT must be an integer from 1 to 65535."
  API_PORT=8080
  if [[ -f "$API_PORT_FILE" ]]; then
    API_PORT="$(cat "$API_PORT_FILE")"
  elif pid_running api; then
    # Older instances have no API port record; adopt only this checkout's listener.
    for ((port=8080; port<=8100; port++)); do
      if port_owned api "$port"; then API_PORT=$port; break; fi
    done
  fi
  valid_api_port "$API_PORT" || fail "Invalid saved API port in ${API_PORT_FILE}; expected 8080 through 8100."
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
  printf '%s\n' "$MODE" >"${MODE_FILE}.tmp"
  mv -- "${MODE_FILE}.tmp" "$MODE_FILE"
  printf '%s\n' "$WEB_PORT" >"${WEB_PORT_FILE}.tmp"
  mv -- "${WEB_PORT_FILE}.tmp" "$WEB_PORT_FILE"
  printf '%s\n' "$API_PORT" >"${API_PORT_FILE}.tmp"
  mv -- "${API_PORT_FILE}.tmp" "$API_PORT_FILE"
}

prepare_env() {
  [[ -f "$ENV_EXAMPLE" ]] || fail ".env.example is missing from ${ROOT_DIR}."
  if [[ ! -f "$ENV_FILE" ]]; then cp -- "$ENV_EXAMPLE" "$ENV_FILE"; ok "Created .env from .env.example."; else info "Using existing .env."; fi
  chmod 600 "$ENV_FILE"
  load_env
  if [[ "$MODE" == local ]]; then case "${HTTP_ADDR:-127.0.0.1:8080}" in
    127.0.0.1:8080|localhost:8080|0.0.0.0:8080|:8080|'[::]:8080') ;;
    *) fail "HTTP_ADDR must use a local bind address and base port 8080; the installer selects an available port up to 8100." ;;
  esac; fi
  [[ "${MYSQL_HOST:-127.0.0.1}" == 127.0.0.1 || "${MYSQL_HOST:-}" == localhost ]] || fail "Local mode requires MYSQL_HOST=127.0.0.1 or localhost."
  valid_port "${MYSQL_PORT:-3306}" || fail "MYSQL_PORT must be an integer from 1 to 65535."
  [[ "${REDIS_ADDR:-127.0.0.1:6379}" =~ ^(127\.0\.0\.1|localhost):([1-9][0-9]{0,4})$ ]] || fail "Local mode requires REDIS_ADDR=127.0.0.1:port or localhost:port."
  REDIS_PORT=${BASH_REMATCH[2]}
  valid_port "$REDIS_PORT" || fail "Redis port must be from 1 to 65535."
  export REDIS_PORT
  local mysql_port=${MYSQL_PORT:-3306}
  [[ "$mysql_port" != "$REDIS_PORT" ]] || fail "MySQL and Redis must use separate ports."
  if [[ "$MODE" == local ]]; then [[ "$mysql_port" != "$WEB_PORT" && "$REDIS_PORT" != "$WEB_PORT" ]] || fail "MySQL, Redis and frontend must use separate ports."; fi
  [[ "${BOOTSTRAP_ON_START:-false}" == true || "${BOOTSTRAP_ON_START:-false}" == false ]] || fail "BOOTSTRAP_ON_START must be true or false."
  load_instance
}

compose() {
  local -a files=(-f "$ROOT_DIR/docker-compose.yml")
  local ignore_orphans=false
  local docker_env_file=/dev/null
  if [[ -f "$ENV_FILE" ]]; then docker_env_file=$ENV_FILE; fi
  if [[ "$MODE" == local-docker ]]; then files+=(-f "$ROOT_DIR/docker/compose.local.yaml"); else ignore_orphans=true; fi
  # The native profile intentionally leaves verified Docker app containers stopped.
  (cd "$ROOT_DIR" && OPENSNOW_DOCKER_ENV_FILE="$docker_env_file" COMPOSE_IGNORE_ORPHANS="$ignore_orphans" docker compose --project-directory "$ROOT_DIR" -p "$COMPOSE_PROJECT_NAME" "${files[@]}" "$@")
}

check_dependency_ownership() {
  local service id working_dir config_file ids
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    working_dir="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")"
    config_file="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$id")"
    service="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$id")"
    [[ "$working_dir" == "$ROOT_DIR" && ( "$config_file" == "$ROOT_DIR/docker-compose.yml" || "$config_file" == "$ROOT_DIR/docker-compose.yml,$ROOT_DIR/docker/compose.local.yaml" ) ]] ||
      fail "Compose project ${COMPOSE_PROJECT_NAME} belongs to another checkout/configuration (${working_dir}); choose a separate COMPOSE_PROJECT_NAME in .env."
    case "$service" in mysql|redis|api|frontend|worker|scheduler) ;; *) fail "Unexpected service ${service} in Compose project ${COMPOSE_PROJECT_NAME}; refusing to manage it." ;; esac
  done <<<"$ids"
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
  local pid=$1 component=$2 expected cwd executable argument script
  [[ "$pid" =~ ^[1-9][0-9]*$ && -r "/proc/$pid/cmdline" ]] || return 1
  [[ "$(stat -c %u "/proc/$pid")" == "$EUID" ]] || return 1
  cwd="$(readlink "/proc/$pid/cwd")" || return 1
  local -a argv=()
  while IFS= read -r -d '' argument; do argv+=("$argument"); done <"/proc/$pid/cmdline"
  if [[ "$component" == web ]]; then
    executable="$(readlink "/proc/$pid/exe")" || return 1
    [[ "$executable" == */node || "$executable" == */'node (deleted)' ]] || return 1
    script=${argv[1]:-}
    [[ "$script" == /* ]] || script="$cwd/$script"
    [[ "$cwd" == "$ROOT_DIR/web" && "$(readlink -f "$script")" == "$ROOT_DIR/web/node_modules/vite/bin/vite.js" ]]
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

api_port_available() {
  [[ "$1" != "$WEB_PORT" && "$1" != "${MYSQL_PORT:-3306}" && "$1" != "${REDIS_PORT:-6379}" ]] || return 1
  ! port_busy "$1" || port_owned api "$1"
}

select_api_port() {
  local port
  if [[ "$ACTION" == start ]] && pid_running api && ! api_port_available "$API_PORT"; then
    fail "This checkout's API is already running on port ${API_PORT}, which conflicts with the requested configuration. Use --local --restart."
  fi
  # Keep a usable saved/live port so repeated start and restart remain stable.
  if api_port_available "$API_PORT"; then return; fi
  for ((port=8080; port<=8100; port++)); do
    if api_port_available "$port"; then
      API_PORT=$port
      info "Selected available API port ${API_PORT} (range 8080-8100)."
      return
    fi
  done
  fail "No available API port in 8080-8100; all ports are occupied or reserved for this instance's frontend/database/cache."
}

check_ports() {
  local port
  if port_busy "$WEB_PORT" && ! port_owned web "$WEB_PORT"; then
    if [[ "$ACTION" == start ]] || ! port_owned api "$WEB_PORT"; then
      fail "Port ${WEB_PORT} is occupied by another process/checkout; web cannot start. Choose another OPENSNOW_WEB_PORT for the frontend."
    fi
  fi
  if [[ "$ACTION" == start ]] && pid_running web && ! port_owned web "$WEB_PORT"; then fail "This checkout's frontend is already running on another port. Use --local --restart to change its port."; fi
  check_dependency_ownership
  check_dependency_ports
  select_api_port
}

check_dependency_ports() {
  local port
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
  local component=$1 cwd="$ROOT_DIR" pid attempt log_line bind_address=${HTTP_ADDR:-127.0.0.1:8080}
  if pid_running "$component"; then info "${component} is already running (PID $(pid_from_file "$component"))."; return 0; fi
  local -a command=("$BIN_DIR/opensnow-$component")
  if [[ "$component" == api ]]; then command=(env "HTTP_ADDR=${bind_address%:*}:$API_PORT" "$BIN_DIR/opensnow-api"); fi
  # Vite exits on stdin EOF outside CI mode; a detached server has /dev/null stdin.
  if [[ "$component" == web ]]; then cwd="$ROOT_DIR/web"; command=(env CI=true "OPENSNOW_API_PORT=$API_PORT" node "$ROOT_DIR/web/node_modules/vite/bin/vite.js" --host 127.0.0.1 --port "$WEB_PORT" --strictPort); fi
  touch "$LOG_DIR/$component.log"
  log_line=$(( $(wc -l <"$LOG_DIR/$component.log") + 1 ))
  (
    exec 9>&-
    trap - ERR EXIT INT TERM
    cd "$cwd"
    # Release the launching terminal/session as well as ignoring its hangup signal.
    exec nohup setsid "${command[@]}"
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
start_api() {
  # Recheck after migrations/builds: another process may have taken the chosen port.
  select_api_port
  start_component api
  wait_for_http api "$API_PORT" /health/ready
}
start_web() {
  local proxy_port=8080
  if [[ -f "$API_PORT_FILE" ]]; then proxy_port="$(cat "$API_PORT_FILE")"; fi
  if pid_running web && [[ "$proxy_port" != "$API_PORT" ]]; then
    info "Restarting Vite to proxy to API port ${API_PORT}."
    stop_pid web
  fi
  start_component web
  wait_for_http web "$WEB_PORT" /
}

verify_runtime() {
  local component
  for component in api worker scheduler web; do pid_running "$component" || fail "${component} is not running; see ${LOG_DIR}/${component}.log."; done
  curl --silent --fail --max-time 3 "http://127.0.0.1:${WEB_PORT}/health/ready" >/dev/null || fail "Frontend /health proxy cannot reach the ready API."
  curl --silent --fail --max-time 3 "http://127.0.0.1:${WEB_PORT}/api/v1/setup" >/dev/null || fail "Frontend /api proxy cannot reach the setup API."
  save_instance
  ok "OpenServiceNOW is running: http://127.0.0.1:${WEB_PORT}"
  info "API address: http://127.0.0.1:${API_PORT}"
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
  docker_compose stop frontend scheduler worker api redis mysql
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
        port=$API_PORT; [[ "$component" != web ]] || port=$WEB_PORT
        state=unready
        if port_owned "$component" "$port" && curl --silent --fail --max-time 2 "http://127.0.0.1:$port/health/ready" >/dev/null; then state=ready; fi
      fi
    elif [[ -f "$RUN_DIR/$component.pid" ]]; then state=stale/unrelated; fi
    status_line "$component" "$state"
    [[ "$state" == ready || "$state" == running ]] || failures=$((failures+1))
  done
  info "WebUI address: http://127.0.0.1:${WEB_PORT}"
  info "API address: http://127.0.0.1:${API_PORT}"
  info "Runtime logs: ${LOG_DIR}"
  (( failures == 0 ))
}

uninstall() {
  stop_all
  if (( PURGE_DATA )); then docker_compose down -v; rm -f -- "$ENV_FILE"; ok "This instance's SQL volume and .env removed."; else docker_compose down; info "SQL volume and .env preserved."; fi
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
  stop_docker_application
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
    if [[ "$MODE" == local-docker ]]; then capture_docker_logs; fi
    for component in "${STARTED_COMPONENTS[@]}"; do stop_pid "$component" || true; done
    if (( ${#STARTED_SERVICES[@]} > 0 )); then compose stop "${STARTED_SERVICES[@]}" >/dev/null 2>&1 || true; fi
  fi
  return "$result"
}

main() {
  declare -ga INSTALLER_ARGS=("$@")
  parse_args "$@"
  if [[ "$ACTION" == help ]]; then usage; return; fi
  [[ "$(uname -s)" == Linux && -d /proc/self ]] || fail "Local runtime supports Linux and WSL. Run this script in your Linux/WSL shell."
  local command_name
  for command_name in flock env stat readlink awk sed sort; do require_command "$command_name" "Install standard Linux utilities (util-linux and coreutils)."; done
  if [[ "$MODE" == local ]]; then
    for command_name in ss curl nohup setsid; do require_command "$command_name" "Install util-linux, iproute2, coreutils and curl."; done
  fi
  ensure_state_dirs
  exec 9>"$RUN_DIR/installer.lock"
  flock -n 9 || fail "Another installer command is running for this checkout. Wait for it to finish."
  trap 'on_exit' EXIT
  trap 'printf "[FAIL] Command failed at install.sh line %s.\n" "$LINENO" >&2' ERR
  trap 'exit 130' INT
  trap 'exit 143' TERM
  load_env
  load_instance
  check_runtime_mode
  if [[ "$MODE" == local-docker ]]; then docker_main; return; fi
  case "$ACTION" in
    install) install ;;
    start) start_all ;;
    stop) stop_all ;;
    restart) check_toolchains; prepare_env; check_built_runtime; check_ports; check_dependency_ownership; stop_runtime; stop_docker_application; start_dependencies; start_runtime ;;
    status) show_status ;;
    uninstall) uninstall ;;
  esac
}

# shellcheck source=docker/local-runtime.sh
source "$ROOT_DIR/docker/local-runtime.sh"

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
