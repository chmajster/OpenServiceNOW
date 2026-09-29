#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
ENV_EXAMPLE="${ROOT_DIR}/.env.example"
STATE_DIR="${ROOT_DIR}/.opensnow"
RUN_DIR="${STATE_DIR}/run"
LOG_DIR="${STATE_DIR}/logs"
BIN_DIR="${ROOT_DIR}/bin"
API_BIN="${BIN_DIR}/opensnow-api"
API_PID_FILE="${RUN_DIR}/api.pid"
WEB_PID_FILE="${RUN_DIR}/web.pid"
WEB_PORT="${OPENSNOW_WEB_PORT:-5173}"
PURGE_DATA=0
NO_START=0

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_GREEN=$'\033[32m'
  C_BLUE=$'\033[36m'
  C_YELLOW=$'\033[33m'
  C_RED=$'\033[31m'
  C_RESET=$'\033[0m'
else
  C_GREEN=''
  C_BLUE=''
  C_YELLOW=''
  C_RED=''
  C_RESET=''
fi

ok()   { printf '%s[ OK ]%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
info() { printf '%s[INFO]%s %s\n' "$C_BLUE" "$C_RESET" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
fail() { printf '%s[FAIL]%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 1; }
stage() { printf '\n%s[%s/7]%s %s\n' "$C_BLUE" "$1" "$C_RESET" "$2"; }

on_error() {
  local exit_code=$?
  local line_no=${1:-?}
  printf '%s[FAIL]%s Installer stopped at line %s (exit %s).\n' "$C_RED" "$C_RESET" "$line_no" "$exit_code" >&2
  exit "$exit_code"
}
trap 'on_error "$LINENO"' ERR

usage() {
  cat <<'EOF'
OpenServiceNOW installer

Usage:
  ./install.sh                  Install, build and start OpenServiceNOW
  ./install.sh --no-start       Install and build without starting API/frontend
  ./install.sh --start          Start dependencies, API and frontend
  ./install.sh --stop           Stop API, frontend and Docker dependencies
  ./install.sh --restart        Restart OpenServiceNOW
  ./install.sh --status         Show application and dependency status
  ./install.sh --uninstall      Stop and remove generated build/runtime files
  ./install.sh --uninstall --purge-data
                                Also delete Docker volumes and .env
  ./install.sh --help           Show this help

The installer preserves .env and MySQL data during ordinary uninstall.
EOF
}

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

require_command() {
  local command_name=$1
  local hint=$2
  if ! command_exists "$command_name"; then
    fail "Missing required command: ${command_name}. ${hint}"
  fi
}

version_at_least() {
  local have=$1
  local want=$2
  local first
  first="$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n1)"
  [[ "$first" == "$want" ]]
}

check_toolchains() {
  require_command docker "Install Docker Engine/Desktop and enable Docker Compose v2."
  if ! docker compose version >/dev/null 2>&1; then
    fail "Docker Compose v2 is required (docker compose)."
  fi
  require_command go "Install Go 1.27 or newer."
  require_command node "Install Node.js 22.12 or newer."
  require_command npm "Install npm matching the selected Node.js release."
  require_command curl "Install curl."
  require_command awk "Install awk."
  require_command sed "Install sed."
  require_command sort "Install coreutils."

  local go_version node_version
  go_version="$(go version | awk '{print $3}' | sed 's/^go//')"
  node_version="$(node --version | sed 's/^v//')"

  version_at_least "$go_version" "1.27" || fail "Go ${go_version} detected; OpenServiceNOW requires Go >= 1.27."
  version_at_least "$node_version" "22.12.0" || fail "Node.js ${node_version} detected; OpenServiceNOW requires Node.js >= 22.12.0."
  docker info >/dev/null 2>&1 || fail "Docker daemon is not available."
  ok "Docker, Compose, Go ${go_version}, Node.js ${node_version}, npm and curl are available."
}

ensure_state_dirs() {
  mkdir -p "$RUN_DIR" "$LOG_DIR" "$BIN_DIR"
}

generate_secret() {
  if command_exists openssl; then
    openssl rand -hex 24
    return
  fi
  od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
}

set_env_value() {
  local key=$1
  local value=$2
  local tmp
  tmp="$(mktemp "${ENV_FILE}.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { replaced = 0 }
    $0 ~ "^" key "=" {
      print key "=" value
      replaced = 1
      next
    }
    { print }
    END {
      if (!replaced) {
        print key "=" value
      }
    }
  ' "$ENV_FILE" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_FILE"
}

env_value() {
  local key=$1
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "$ENV_FILE"
}

prepare_env() {
  [[ -f "$ENV_EXAMPLE" ]] || fail ".env.example is missing."
  if [[ ! -f "$ENV_FILE" ]]; then
    cp "$ENV_EXAMPLE" "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    ok "Created .env from .env.example."
  else
    chmod 600 "$ENV_FILE"
    info "Using existing .env."
  fi

  local current_password
  current_password="$(env_value BOOTSTRAP_ADMIN_PASSWORD || true)"
  current_password="${current_password%\"}"
  current_password="${current_password#\"}"
  if [[ -z "$current_password" ]]; then
    set_env_value BOOTSTRAP_ADMIN_PASSWORD "$(generate_secret)"
    ok "Generated BOOTSTRAP_ADMIN_PASSWORD and saved it in .env."
  fi

  set -a
  source "$ENV_FILE"
  set +a

  local api_port
  api_port="${HTTP_ADDR##*:}"
  if [[ "$api_port" != "8080" ]]; then
    fail "HTTP_ADDR must currently use port 8080 because web/vite.config.ts proxies API requests to 127.0.0.1:8080."
  fi
  [[ -n "${BOOTSTRAP_ADMIN_PASSWORD:-}" ]] || fail "BOOTSTRAP_ADMIN_PASSWORD must not be empty."
}

compose() {
  (cd "$ROOT_DIR" && docker compose "$@")
}

container_health() {
  local service=$1
  local id
  id="$(compose ps -q "$service" 2>/dev/null || true)"
  [[ -n "$id" ]] || return 1
  docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id" 2>/dev/null
}

wait_for_service() {
  local service=$1
  local timeout=${2:-90}
  local elapsed=0
  local state=''
  while (( elapsed < timeout )); do
    state="$(container_health "$service" || true)"
    if [[ "$state" == "healthy" || "$state" == "running" ]]; then
      ok "${service} is ${state}."
      return 0
    fi
    if [[ "$state" == "unhealthy" || "$state" == "exited" || "$state" == "dead" ]]; then
      compose logs --tail=80 "$service" >&2 || true
      fail "${service} entered state ${state}."
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  compose logs --tail=80 "$service" >&2 || true
  fail "Timed out waiting for ${service}."
}

start_dependencies() {
  compose up -d mysql redis
  wait_for_service mysql
  wait_for_service redis
}

pid_from_file() {
  local file=$1
  [[ -f "$file" ]] || return 1
  local pid
  pid="$(cat "$file" 2>/dev/null || true)"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  printf '%s\n' "$pid"
}

pid_running() {
  local file=$1
  local pid
  pid="$(pid_from_file "$file" || true)"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

process_matches() {
  local pid=$1
  local expected=$2
  local command_line
  command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command_line" == *"$expected"* ]]
}

stop_pid() {
  local file=$1
  local expected=$2
  local label=$3
  local pid
  pid="$(pid_from_file "$file" || true)"
  if [[ -z "$pid" ]]; then
    rm -f "$file"
    return 0
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$file"
    return 0
  fi
  if ! process_matches "$pid" "$expected"; then
    warn "PID ${pid} from ${file} does not look like ${label}; refusing to kill it."
    rm -f "$file"
    return 0
  fi

  kill "$pid" 2>/dev/null || true
  local i
  for i in {1..20}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$file"
      ok "${label} stopped."
      return 0
    fi
    sleep 0.5
  done
  kill -KILL "$pid" 2>/dev/null || true
  rm -f "$file"
  warn "${label} required SIGKILL."
}

wait_for_http() {
  local url=$1
  local label=$2
  local timeout=${3:-45}
  local elapsed=0
  while (( elapsed < timeout )); do
    if curl --silent --show-error --fail --max-time 2 "$url" >/dev/null 2>&1; then
      ok "${label} is reachable at ${url}."
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

start_api() {
  if pid_running "$API_PID_FILE"; then
    info "API is already running (PID $(pid_from_file "$API_PID_FILE"))."
  else
    rm -f "$API_PID_FILE"
    (
      cd "$ROOT_DIR"
      set -a
      source "$ENV_FILE"
      set +a
      exec "$API_BIN"
    ) >>"${LOG_DIR}/api.log" 2>&1 &
    echo "$!" >"$API_PID_FILE"
  fi

  if ! wait_for_http "http://127.0.0.1:8080/health/ready" "API readiness" 45; then
    tail -n 100 "${LOG_DIR}/api.log" >&2 || true
    fail "API did not become ready."
  fi
}

start_web() {
  if pid_running "$WEB_PID_FILE"; then
    info "Frontend is already running (PID $(pid_from_file "$WEB_PID_FILE"))."
  else
    rm -f "$WEB_PID_FILE"
    (
      cd "${ROOT_DIR}/web"
      exec ./node_modules/.bin/vite --host 127.0.0.1 --port "$WEB_PORT" --strictPort
    ) >>"${LOG_DIR}/web.log" 2>&1 &
    echo "$!" >"$WEB_PID_FILE"
  fi

  if ! wait_for_http "http://127.0.0.1:${WEB_PORT}/" "Frontend" 45; then
    tail -n 100 "${LOG_DIR}/web.log" >&2 || true
    fail "Frontend did not become reachable."
  fi
}

start_all() {
  ensure_state_dirs
  prepare_env
  start_dependencies
  [[ -x "$API_BIN" ]] || fail "Backend binary is missing. Run ./install.sh first."
  [[ -d "${ROOT_DIR}/web/node_modules" ]] || fail "Frontend dependencies are missing. Run ./install.sh first."
  start_api
  start_web
  ok "OpenServiceNOW is running: http://127.0.0.1:${WEB_PORT}"
}

stop_all() {
  ensure_state_dirs
  stop_pid "$WEB_PID_FILE" "vite" "Frontend"
  stop_pid "$API_PID_FILE" "opensnow-api" "API"
  compose stop redis mysql >/dev/null 2>&1 || true
  ok "OpenServiceNOW services stopped."
}

status_line() {
  local label=$1
  local state=$2
  if [[ "$state" == "running" || "$state" == "healthy" || "$state" == "ready" ]]; then
    ok "${label}: ${state}"
  else
    warn "${label}: ${state}"
  fi
}

show_status() {
  ensure_state_dirs
  local failures=0
  local state

  state="$(container_health mysql || true)"
  [[ -n "$state" ]] || state="stopped"
  status_line "MySQL" "$state"
  [[ "$state" == "healthy" || "$state" == "running" ]] || failures=$((failures + 1))

  state="$(container_health redis || true)"
  [[ -n "$state" ]] || state="stopped"
  status_line "Redis" "$state"
  [[ "$state" == "healthy" || "$state" == "running" ]] || failures=$((failures + 1))

  if pid_running "$API_PID_FILE" && curl --silent --fail --max-time 2 "http://127.0.0.1:8080/health/ready" >/dev/null 2>&1; then
    status_line "API" "ready"
  else
    status_line "API" "stopped/unready"
    failures=$((failures + 1))
  fi

  if pid_running "$WEB_PID_FILE" && curl --silent --fail --max-time 2 "http://127.0.0.1:${WEB_PORT}/" >/dev/null 2>&1; then
    status_line "Frontend" "ready"
  else
    status_line "Frontend" "stopped/unready"
    failures=$((failures + 1))
  fi

  info "Runtime logs: ${LOG_DIR}"
  return "$failures"
}

uninstall() {
  stop_all
  if (( PURGE_DATA )); then
    compose down -v --remove-orphans
    rm -f "$ENV_FILE"
    ok "Docker volumes and .env removed."
  else
    compose down --remove-orphans
    info "MySQL volume and .env preserved. Use --uninstall --purge-data to remove them."
  fi

  rm -rf "$STATE_DIR" "$BIN_DIR" "${ROOT_DIR}/web/dist" "${ROOT_DIR}/web/node_modules"
  ok "Generated OpenServiceNOW files removed."
}

install() {
  cd "$ROOT_DIR"
  ensure_state_dirs

  stage 1 "Preflight"
  check_toolchains

  stage 2 "Configuration"
  prepare_env

  stage 3 "MySQL and Redis"
  start_dependencies

  stage 4 "Dependencies"
  go mod download
  (cd web && npm ci)
  ok "Go and frontend dependencies installed."

  stage 5 "Database migration and bootstrap"
  go run ./cmd/migrate up
  go run ./cmd/bootstrap
  ok "Database schema and initial administrator are ready."

  stage 6 "Build"
  go build -trimpath -o "$API_BIN" ./cmd/api
  (cd web && npm run build)
  ok "Backend and frontend builds completed."

  stage 7 "Runtime"
  if (( NO_START )); then
    info "Installation completed without starting API/frontend (--no-start)."
  else
    start_api
    start_web
    ok "OpenServiceNOW is running: http://127.0.0.1:${WEB_PORT}"
  fi
}

ACTION="install"
while (( $# > 0 )); do
  case "$1" in
    --help|-h)
      ACTION="help"
      ;;
    --status)
      ACTION="status"
      ;;
    --start)
      ACTION="start"
      ;;
    --stop)
      ACTION="stop"
      ;;
    --restart)
      ACTION="restart"
      ;;
    --uninstall)
      ACTION="uninstall"
      ;;
    --purge-data)
      PURGE_DATA=1
      ;;
    --no-start)
      NO_START=1
      ;;
    *)
      fail "Unknown argument: $1. Run ./install.sh --help."
      ;;
  esac
  shift
done

case "$ACTION" in
  help)
    usage
    ;;
  status)
    trap - ERR
    set +e
    show_status
    exit $?
    ;;
  start)
    check_toolchains
    start_all
    ;;
  stop)
    stop_all
    ;;
  restart)
    check_toolchains
    stop_all
    start_all
    ;;
  uninstall)
    uninstall
    ;;
  install)
    install
    ;;
esac
