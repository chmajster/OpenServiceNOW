#!/usr/bin/env bash
# Sourced by install.sh; shares checkout, locking, ownership and data lifecycle.
DOCKER_WEB_PORT=8080
REQUESTED_DOCKER_PORT=${OPENSNOW_DOCKER_PORT:-}

docker_compose() {
  (cd "$ROOT_DIR" && OPENSNOW_DOCKER_PORT="$DOCKER_WEB_PORT" docker compose --project-directory "$ROOT_DIR" -p "$COMPOSE_PROJECT_NAME" -f "$ROOT_DIR/docker-compose.yml" -f "$ROOT_DIR/docker/compose.local.yaml" "$@")
}

check_runtime_mode() {
  local saved=local component pid stamp owner
  if [[ -f "$MODE_FILE" ]]; then saved=$(cat "$MODE_FILE"); fi
  case "$saved" in local|local-docker) ;; *) fail "Invalid runtime mode in ${MODE_FILE}." ;; esac
  if [[ "$saved" != "$MODE" && "$ACTION" != install && "$ACTION" != restart ]]; then
    fail "This checkout uses --${saved}. Use that mode for --${ACTION}, or install/restart with --${MODE} to switch while preserving SQL data."
  fi
  if [[ "$ACTION" != status ]]; then
    for component in api worker scheduler web; do
      [[ -f "$RUN_DIR/$component.pid" ]] || continue
      read -r pid stamp <"$RUN_DIR/$component.pid" || continue
      [[ "$pid" =~ ^[1-9][0-9]*$ && -d "/proc/$pid" ]] || continue
      owner=$(stat -c %u "/proc/$pid")
      if [[ "$owner" != "$EUID" && -n "$stamp" && "$stamp" == "$(process_stamp "$pid" 2>/dev/null || true)" ]]; then
        fail "Local ${component} belongs to UID ${owner}. Run the installer as that user; sudo is only needed for Docker package management."
      fi
    done
  fi
  if [[ -f "$RUN_DIR/docker-port" ]]; then DOCKER_WEB_PORT=$(cat "$RUN_DIR/docker-port"); fi
  valid_api_port "$DOCKER_WEB_PORT" || fail "Invalid saved Docker frontend port; expected 8080 through 8100."
  if [[ "$ACTION" == install || "$ACTION" == restart ]] && [[ -n "$REQUESTED_DOCKER_PORT" ]]; then DOCKER_WEB_PORT=$REQUESTED_DOCKER_PORT; fi
  valid_api_port "$DOCKER_WEB_PORT" || fail "OPENSNOW_DOCKER_PORT must be an integer from 8080 through 8100."
}

prepare_docker_engine() {
  local username endpoint quoted_command
  if (( SKIP_DOCKER_UPDATE )); then info "Using the existing managed Docker Engine (--skip-docker-update)."; return; fi
  if [[ "${OPENSNOW_ENGINE_PREPARED:-}" == 1 ]]; then unset OPENSNOW_ENGINE_PREPARED; return; fi
  [[ -z "${DOCKER_HOST:-}" || "${DOCKER_HOST:-}" == unix:///var/run/docker.sock || "${DOCKER_HOST:-}" == unix:///run/docker.sock ]] ||
    fail "Engine updates require the local Linux Docker socket. Use --skip-docker-update for a remote/managed engine."
  if command -v docker >/dev/null 2>&1; then
    endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)
    [[ -z "$endpoint" || "$endpoint" == unix:///var/run/docker.sock || "$endpoint" == unix:///run/docker.sock ]] ||
      fail "Selected Docker context is managed externally. Use --skip-docker-update or select the local Linux engine."
  fi
  if [[ "$(readlink -f /var/run/docker.sock 2>/dev/null || true)" == *docker-desktop* ]]; then
    fail "Docker Desktop is managed on Windows. Update it there and use --skip-docker-update in WSL."
  fi
  username=$(id -un)
  if (( EUID == 0 )); then
    bash "$ROOT_DIR/docker/engine-install.sh" "${SUDO_USER:-$username}" 9>&-
  else
    require_command sudo "Docker package installation requires sudo."
    sudo bash "$ROOT_DIR/docker/engine-install.sh" "$username" 9>&-
  fi
  if ! docker info >/dev/null 2>&1 && (( EUID != 0 )) && id -nG "$username" | tr ' ' '\n' | grep -qx docker && ! id -nG | tr ' ' '\n' | grep -qx docker; then
    require_command sg "Open a new terminal to refresh Docker group membership."
    printf -v quoted_command '%q ' env OPENSNOW_ENGINE_PREPARED=1 bash "$ROOT_DIR/install.sh" "${INSTALLER_ARGS[@]}"
    exec 9>&-
    exec sg docker -c "$quoted_command"
  fi
}

check_docker_runtime_tools() {
  require_command curl "Install curl."
  require_command ss "Install iproute2."
  check_docker
  local version
  version=$(docker compose version --short); version=${version#v}
  version_at_least "$version" 2.20.0 || fail "Docker mode requires Compose >= 2.20.0. Run --local-docker to update it."
}

stop_docker_application() {
  local ids
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" --filter 'label=com.docker.compose.service=api')
  [[ -n "$ids" ]] || return 0
  docker_compose stop frontend scheduler worker api
}

docker_frontend_owns_port() {
  local id
  id=$(docker_compose ps -aq frontend)
  [[ -n "$id" && "$(docker inspect --format '{{.State.Running}}' "$id")" == true && "$(docker_compose port frontend 8080 2>/dev/null || true)" == "127.0.0.1:$1" ]]
}

docker_port_available() {
  [[ "$1" != "${MYSQL_PORT:-3306}" && "$1" != "${REDIS_PORT:-6379}" ]] || return 1
  ! port_busy "$1" || docker_frontend_owns_port "$1" ||
    { [[ "$ACTION" == install || "$ACTION" == restart ]] && { port_owned api "$1" || port_owned web "$1"; }; }
}

select_docker_port() {
  local port
  if docker_port_available "$DOCKER_WEB_PORT"; then return; fi
  if [[ -n "$REQUESTED_DOCKER_PORT" ]]; then fail "Requested Docker frontend port ${DOCKER_WEB_PORT} is occupied or reserved."; fi
  for ((port=8080; port<=8100; port++)); do
    if docker_port_available "$port"; then DOCKER_WEB_PORT=$port; info "Selected available Docker WebUI port ${port} (range 8080-8100)."; return; fi
  done
  fail "No available Docker WebUI port in 8080-8100. Foreign listeners were preserved."
}

docker_save_instance() {
  save_instance
  printf '%s\n' "$DOCKER_WEB_PORT" >"$RUN_DIR/docker-port.tmp"
  mv -- "$RUN_DIR/docker-port.tmp" "$RUN_DIR/docker-port"
}

check_docker_images() {
  local image
  for image in backend frontend; do
    docker image inspect "$COMPOSE_PROJECT_NAME-$image:local" >/dev/null 2>&1 || fail "Local ${image} image is missing. Run --local-docker to build current sources."
  done
}

capture_docker_logs() {
  local service
  for service in mysql redis api frontend worker scheduler; do
    docker_compose logs --no-color --tail 100 "$service" >"$LOG_DIR/docker-$service.log" 2>&1 || true
  done
}

docker_start_services() {
  local service state
  for service in "$@"; do
    state=$(container_health "$service" || true)
    [[ "$state" == healthy || "$state" == running ]] || STARTED_SERVICES+=("$service")
  done
  docker_compose up -d --no-build --pull never "$@"
  for service in "$@"; do wait_for_service "$service"; done
}

verify_docker_runtime() {
  local service state
  for service in mysql redis api frontend worker scheduler; do
    state=$(container_health "$service" || true)
    [[ "$state" == healthy ]] || fail "Docker ${service} is ${state:-missing}; see ${LOG_DIR}/docker-${service}.log."
  done
  docker_frontend_owns_port "$DOCKER_WEB_PORT" || fail "Cannot confirm this checkout's Docker frontend mapping."
  curl --silent --fail --max-time 5 "http://127.0.0.1:$DOCKER_WEB_PORT/health/ready" >/dev/null || fail "Docker frontend cannot reach the ready API."
  curl --silent --fail --max-time 5 "http://127.0.0.1:$DOCKER_WEB_PORT/api/v1/setup" >/dev/null || fail "Docker frontend cannot reach the setup API."
  docker_save_instance
  ok "OpenServiceNOW is running in Docker: http://127.0.0.1:${DOCKER_WEB_PORT}"
  info "API is private; /api and /health use the same WebUI address."
  info "Container logs: ${LOG_DIR}/docker-*.log; SQL data and administrator credentials are preserved."
}

docker_start_runtime() {
  docker_start_services api
  docker_start_services frontend worker scheduler
  verify_docker_runtime
  capture_docker_logs
}

docker_status() {
  local service state failures=0
  check_dependency_ownership
  for service in mysql redis api frontend worker scheduler; do
    state=$(container_health "$service" || true); state=${state:-stopped}
    status_line "$service" "$state"
    [[ "$state" == healthy ]] || failures=$((failures+1))
  done
  if ! docker_frontend_owns_port "$DOCKER_WEB_PORT" || ! curl --silent --fail --max-time 3 "http://127.0.0.1:$DOCKER_WEB_PORT/health/ready" >/dev/null; then
    warn "Docker frontend proxy is unavailable."; failures=$((failures+1))
  fi
  info "Docker WebUI address: http://127.0.0.1:$DOCKER_WEB_PORT"
  (( failures == 0 ))
}

docker_install() {
  stage 2 "Configuration"
  prepare_env
  check_dependency_ownership
  check_dependency_ports
  select_docker_port
  docker_compose config --quiet
  stage 3 "Update infrastructure images"
  docker_compose pull mysql redis
  stage 4 "Build application images from this checkout"
  docker_compose build --pull api frontend
  check_docker_images
  stage 5 "Stop old application and start infrastructure"
  stop_runtime
  stop_docker_application
  select_docker_port
  start_dependencies
  stage 6 "Database migration and account setup"
  docker_compose run --rm --no-deps api migrate up
  if (( BOOTSTRAP )) || [[ "${BOOTSTRAP_ON_START:-false}" == true ]]; then
    docker_compose run --rm --no-deps api bootstrap
    ok "Explicit administrator bootstrap completed."
  else ok "Schema ready; the WebUI creates the first administrator on a fresh installation."; fi
  stage 7 "Container runtime"
  if (( NO_START )); then docker_save_instance; info "Images and schema ready (--no-start). Use --local-docker --start."; else docker_start_runtime; fi
}

docker_main() {
  if [[ "$ACTION" == install ]]; then stage 1 "Docker Engine installation/update"; prepare_docker_engine; fi
  check_docker_runtime_tools
  case "$ACTION" in
    install) docker_install ;;
    status) docker_status ;;
    stop) check_dependency_ownership; capture_docker_logs; docker_compose stop frontend scheduler worker api redis mysql; ok "Docker application stopped; SQL data and .env preserved." ;;
    uninstall)
      check_dependency_ownership
      capture_docker_logs
      if (( PURGE_DATA )); then docker_compose down -v; rm -f -- "$ENV_FILE"; else docker_compose down; fi
      rm -f -- "$MODE_FILE" "$RUN_DIR/docker-port"
      ok "Docker application removed; Engine and local image cache preserved."
      ;;
    start|restart)
      prepare_env
      check_dependency_ownership
      check_dependency_ports
      check_docker_images
      select_docker_port
      if [[ "$ACTION" == restart ]]; then stop_runtime; stop_docker_application; fi
      start_dependencies
      docker_start_runtime
      ;;
  esac
}
