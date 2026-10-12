#!/usr/bin/env bash
set -Eeuo pipefail
trap 'printf "[FAIL] Docker lifecycle verification failed at line %s.\n" "$LINENO" >&2' ERR

# This suite owns only its disposable checkout, Compose project and SQL volume.
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
test_dir=$(mktemp -d /tmp/opensnow-docker-runtime.XXXXXX)
checkout="$test_dir/checkout with spaces"
project="opensnow-docker-test-${UID}-${RANDOM}"
listener_pid=''
mkdir -p "$checkout"
cleanup() {
  local service
  if [[ -n "$listener_pid" ]]; then kill "$listener_pid" 2>/dev/null || true; fi
  mkdir -p "$repo_dir/.opensnow/docker-runtime-verification"
  for service in mysql redis api frontend worker scheduler; do
    compose logs --no-color --tail 100 "$service" >"$repo_dir/.opensnow/docker-runtime-verification/$service.log" 2>&1 || true
  done
  if [[ "${LOCAL_DOCKER_KEEP:-0}" == 1 && "${verified:-0}" == 1 ]]; then return; fi
  compose down -v >/dev/null 2>&1 || true
  rm -rf -- "$test_dir"
}
trap cleanup EXIT
git -C "$repo_dir" ls-files -z | tar -C "$repo_dir" --null -T - -cf - | tar -C "$checkout" -xf -
cat >>"$checkout/.env.example" <<EOF

MYSQL_PORT=23306
REDIS_ADDR=127.0.0.1:26379
REDIS_PASSWORD=docker-test-redis
MYSQL_PASSWORD=docker-test-mysql
COMPOSE_PROJECT_NAME=$project
EOF
installer() {
  case "${1:-install}" in
    --start|--stop|--restart|--status|--uninstall) bash "$checkout/install.sh" --local-docker "$@" ;;
    *) bash "$checkout/install.sh" --local-docker --skip-docker-update "$@" ;;
  esac
}
compose() {
  local port=8080
  if [[ -f "$checkout/.opensnow/run/docker-port" ]]; then port=$(cat "$checkout/.opensnow/run/docker-port"); fi
  OPENSNOW_DOCKER_PORT="$port" docker compose --project-directory "$checkout" -p "$project" -f "$checkout/docker-compose.yml" -f "$checkout/docker/compose.local.yaml" "$@"
}
sql() { compose exec -T -e MYSQL_PWD=docker-test-mysql mysql mysql -u opensnow opensnow -N -e "$1"; }
occupy_ports() {
  rm -f "$test_dir/listeners-ready"
  python3 - "$1" "$2" "$test_dir/listeners-ready" <<'PY' >"$test_dir/listeners.log" 2>&1 &
import pathlib, socket, sys, time
listeners = []
for port in range(int(sys.argv[1]), int(sys.argv[2]) + 1):
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(('127.0.0.1', port))
    listener.listen()
    listeners.append(listener)
pathlib.Path(sys.argv[3]).touch()
while True:
    time.sleep(1)
PY
  listener_pid=$!
  for _ in {1..50}; do
    kill -0 "$listener_pid" || { cat "$test_dir/listeners.log" >&2; return 1; }
    if [[ -f "$test_dir/listeners-ready" ]]; then return; fi
    sleep 0.1
  done
  echo 'Foreign listeners did not start' >&2; return 1
}
stop_listener() { kill "$listener_pid"; wait "$listener_pid" 2>/dev/null || true; listener_pid=''; }
assert_healthy() {
  local service id
  for service in mysql redis api frontend worker scheduler; do
    id=$(compose ps -aq "$service")
    [[ "$(docker inspect --format '{{.State.Health.Status}}' "$id")" == healthy ]]
  done
  for service in api worker scheduler; do
    [[ "$(docker inspect --format '{{.Config.User}}' "$(compose ps -aq "$service")")" == 1000:1000 ]]
  done
  docker inspect --format '{{json .NetworkSettings.Ports}}' "$(compose ps -aq api)" | python3 -c 'import json, sys; assert not any(json.load(sys.stdin).values()), "API has a public port mapping"'
}

cd /tmp
occupy_ports 8080 8081
installer
[[ "$(cat "$checkout/.opensnow/run/docker-port")" == 8082 ]]
[[ ! -f "$checkout/bin/opensnow-api" && ! -d "$checkout/web/node_modules" ]]
assert_healthy
curl --fail --silent http://127.0.0.1:8082/health/ready | grep -q '"status":"ok"'
if [[ "${LOCAL_DOCKER_BROWSER:-0}" == 1 ]]; then
  (cd "$repo_dir/web" && E2E_BASE_URL=http://127.0.0.1:8082 E2E_TENANT=e2e E2E_ADMIN_EMAIL=admin E2E_ADMIN_PASSWORD=admin E2E_OPERATOR_PASSWORD=docker-test-operator-password npm run e2e)
else
  python3 - <<'PY'
import http.cookiejar, json, urllib.error, urllib.request
base = 'http://127.0.0.1:8082'
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
status = json.load(client.open(base + '/api/v1/setup'))
assert status['required'] is True
for path in ['/api/v1/auth/me', '/api/v1/admin/users/']:
    try:
        client.open(base + path)
        raise AssertionError('Anonymous request was accepted')
    except urllib.error.HTTPError as error:
        assert error.code == 401
body = json.dumps(dict(tenant='e2e', tenant_name='Docker verification', email='admin', display_name='Administrator', password='admin')).encode()
request = urllib.request.Request(base + '/api/v1/setup', data=body, headers={'Content-Type':'application/json', 'X-Setup-Token':status['token'], 'Origin':base})
assert client.open(request).status == 201
assert json.load(client.open(base + '/api/v1/setup')) == {'required':False}
PY
fi
original_hash=$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")
[[ -n "$original_hash" ]]
for _ in {1..25}; do
  completed=$(sql "SELECT COUNT(*) FROM jobs WHERE job_type='system.session_cleanup' AND status='COMPLETED'")
  if (( completed > 0 )); then break; fi
  sleep 1
done
(( completed > 0 ))
ids_before=$(compose ps -aq | sort)
installer --start
[[ "$(compose ps -aq | sort)" == "$ids_before" ]]
installer --status
stop_listener

# A failed build must leave the previous containers and real database operational.
cp "$checkout/internal/platform/health/handler.go" "$test_dir/handler-original.go"
printf '\ninvalid Go source\n' >>"$checkout/internal/platform/health/handler.go"
if installer >"$test_dir/build-failure.log" 2>&1; then echo 'Accepted invalid source' >&2; exit 1; fi
[[ "$(compose ps -aq | sort)" == "$ids_before" ]]
assert_healthy
cp "$test_dir/handler-original.go" "$checkout/internal/platform/health/handler.go"
python3 - "$checkout/internal/platform/health/handler.go" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text(path.read_text().replace('map[string]any{"status": "ok"}', 'map[string]any{"status": "docker-update-verified"}', 1))
PY
installer
curl --fail --silent http://127.0.0.1:8082/health/live | grep -q docker-update-verified
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]

# Failed worker startup preserves already-running API/frontend/scheduler containers.
compose stop worker
python3 - "$checkout/docker/compose.local.yaml" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text(path.read_text().replace('command: ["worker"]', 'command: ["invalid-command"]'))
PY
api_before=$(compose ps -aq api)
if installer --start >"$test_dir/start-failure.log" 2>&1; then echo 'Accepted failed worker startup' >&2; exit 1; fi
[[ "$(compose ps -aq api)" == "$api_before" ]]
curl --fail --silent http://127.0.0.1:8082/health/ready | grep -q '"status":"ok"'
python3 - "$checkout/docker/compose.local.yaml" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text(path.read_text().replace('command: ["invalid-command"]', 'command: ["worker"]'))
PY
installer --start
assert_healthy
if bash "$checkout/install.sh" --local --start >"$test_dir/mode-failure.log" 2>&1; then echo 'Accepted wrong runtime mode' >&2; exit 1; fi
grep -q 'uses --local-docker' "$test_dir/mode-failure.log"

OPENSNOW_DOCKER_PORT=8100 installer --restart
[[ "$(cat "$checkout/.opensnow/run/docker-port")" == 8100 ]]
installer --status | grep -q 'http://127.0.0.1:8100'
installer --stop
if installer --status >"$test_dir/stopped.log" 2>&1; then echo 'Stopped runtime reported healthy' >&2; exit 1; fi
occupy_ports 8080 8100
if installer --start >"$test_dir/exhausted.log" 2>&1; then echo 'Exceeded Docker port limit' >&2; exit 1; fi
grep -q 'No available Docker WebUI port in 8080-8100' "$test_dir/exhausted.log"
kill -0 "$listener_pid"
stop_listener
occupy_ports 8100 8100
installer --start
[[ "$(cat "$checkout/.opensnow/run/docker-port")" == 8080 ]]
kill -0 "$listener_pid"
stop_listener
installer --no-start
[[ "$(docker inspect --format '{{.State.Running}}' "$(compose ps -aq api)")" == false ]]
installer --start
assert_healthy
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]
verified=1
echo 'Docker fresh install, browser/API setup, real jobs, image/source update, build failure preservation, partial-start cleanup, lifecycle, port fallback/8100/exhaustion and credential/data preservation passed.'
if [[ "${LOCAL_DOCKER_KEEP:-0}" == 1 ]]; then printf 'Verification checkout: %s\n' "$checkout"; fi
