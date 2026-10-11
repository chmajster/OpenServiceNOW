#!/usr/bin/env bash
set -euo pipefail

# Run only against a new disposable checkout and its own Compose project/SQL volume.
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
test_parent=${LOCAL_RUNTIME_TEST_ROOT:-/tmp}
mkdir -p "$test_parent"
test_dir="$(mktemp -d "$test_parent/opensnow-local-runtime.XXXXXX")"
checkout="$test_dir/checkout with spaces"
project="opensnow-local-test-${UID}-${RANDOM}"
listener_pid=''
mkdir -p "$checkout"
cleanup() {
  if [[ -n "$listener_pid" ]]; then kill "$listener_pid" 2>/dev/null || true; fi
  if [[ "${LOCAL_RUNTIME_KEEP:-0}" == 1 && "${verified:-0}" == 1 ]]; then return; fi
  bash "$checkout/install.sh" --local --stop >/dev/null 2>&1 || true
  docker compose -p "$project" -f "$checkout/docker-compose.yml" down -v >/dev/null 2>&1 || true
  if [[ -f "$checkout/.opensnow/logs/api.log" ]]; then
    mkdir -p "$repo_dir/.opensnow/local-runtime-verification"
    cp -- "$checkout/.opensnow/logs/"*.log "$repo_dir/.opensnow/local-runtime-verification/"
  fi
  rm -rf -- "$test_dir"
}
trap cleanup EXIT
git -C "$repo_dir" ls-files -z | tar -C "$repo_dir" --null -T - -cf - | tar -C "$checkout" -xf -
cat >>"$checkout/.env.example" <<EOF

MYSQL_PORT=23306
REDIS_ADDR=127.0.0.1:26379
REDIS_PASSWORD=local-redis-verification
MYSQL_PASSWORD=local-mysql-verification
COMPOSE_PROJECT_NAME=$project
EOF
installer() { bash "$checkout/install.sh" --local "$@"; }
sql() {
  docker compose -p "$project" -f "$checkout/docker-compose.yml" exec -T -e MYSQL_PWD=local-mysql-verification mysql mysql -u opensnow opensnow -N -e "$1"
}
pid_of() { awk '{print $1}' "$checkout/.opensnow/run/$1.pid"; }
assert_running() {
  local component
  for component in api worker scheduler web; do
    kill -0 "$(pid_of "$component")"
    [[ "$(stat -c %u "/proc/$(pid_of "$component")")" == "$UID" ]]
  done
}

# First run creates .env and runs from the script's location, not the caller's cwd.
cd /tmp
installer
[[ -f "$checkout/.env" ]]
assert_running
curl --fail --silent http://127.0.0.1:5173/ | grep -q '/src/main.tsx'
curl --fail --silent http://127.0.0.1:5173/health/ready | grep -q '"status":"ok"'
python3 - <<'PY'
import http.cookiejar, json, urllib.request
base = 'http://127.0.0.1:5173'
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
status = json.load(client.open(base + '/api/v1/setup'))
assert status['required'] is True
body = json.dumps(dict(tenant='e2e', tenant_name='Local runtime verification', email='admin', display_name='Administrator', password='admin')).encode()
request = urllib.request.Request(base + '/api/v1/setup', data=body, headers={'Content-Type':'application/json', 'X-Setup-Token':status['token'], 'Origin':base})
assert client.open(request).status == 201
assert json.load(client.open(base + '/api/v1/setup')) == {'required':False}
PY
original_hash="$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")"
for _ in {1..20}; do
  completed="$(sql "SELECT COUNT(*) FROM jobs WHERE job_type='system.session_cleanup' AND status='COMPLETED'")"
  if (( completed > 0 )); then break; fi
  sleep 1
done
(( completed > 0 )) # The real scheduler enqueued work and the real worker completed it.
api_before=$(pid_of api)
web_before=$(pid_of web)
worker_before=$(pid_of worker)
scheduler_before=$(pid_of scheduler)
installer --start
[[ "$(pid_of api)" == "$api_before" && "$(pid_of web)" == "$web_before" ]]
[[ "$(pid_of worker)" == "$worker_before" && "$(pid_of scheduler)" == "$scheduler_before" ]]
installer --status

# A failed new scheduler startup rolls back the newly started worker, preserving old API/Vite.
kill -TERM "$(pid_of scheduler)" "$(pid_of worker)"
sleep 2
mv "$checkout/bin/opensnow-scheduler" "$checkout/bin/scheduler-original"
printf '#!/bin/sh\nexit 42\n' >"$checkout/bin/opensnow-scheduler"
chmod +x "$checkout/bin/opensnow-scheduler"
if installer --start >"$test_dir/failure.log" 2>&1; then echo 'Accepted failed scheduler startup' >&2; exit 1; fi
grep -q 'scheduler' "$test_dir/failure.log"
grep -q 'Runtime logs:' "$test_dir/failure.log"
kill -0 "$api_before" "$web_before"
[[ ! -f "$checkout/.opensnow/run/worker.pid" ]]
mv "$checkout/bin/scheduler-original" "$checkout/bin/opensnow-scheduler"
installer --start
assert_running

# Reinstall must use modified local source and replace every old backend process.
python3 - "$checkout/internal/platform/health/handler.go" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text().replace('map[string]any{"status": "ok"}', 'map[string]any{"status": "local-rebuild-verified"}', 1)
path.write_text(text)
PY
installer
[[ "$(pid_of api)" != "$api_before" ]]
curl --fail --silent http://127.0.0.1:5173/health/live | grep -q 'local-rebuild-verified'
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]

# Port selection persists across status/restart/start without repeating the environment variable.
OPENSNOW_WEB_PORT=8090 installer --restart
installer --status | tee "$test_dir/status.log"
grep -q 'http://127.0.0.1:8090' "$test_dir/status.log"
installer --restart
[[ "$(cat "$checkout/.opensnow/run/web-port")" == 8090 ]]
# Docker failure must not prevent stopping this checkout's local processes.
if DOCKER_HOST=tcp://127.0.0.1:1 installer --stop >"$test_dir/docker-unavailable.log" 2>&1; then echo 'Unavailable Docker Engine reported success' >&2; exit 1; fi
grep -q 'Docker Engine is unavailable' "$test_dir/docker-unavailable.log"
for component in api worker scheduler web; do [[ ! -f "$checkout/.opensnow/run/$component.pid" ]]; done
installer --stop
if installer --status >"$test_dir/stopped.log" 2>&1; then echo 'Stopped runtime reported healthy' >&2; exit 1; fi

python3 -m http.server 8090 --bind 127.0.0.1 >"$test_dir/listener.log" 2>&1 &
listener_pid=$!
sleep 1
if installer --start >"$test_dir/conflict.log" 2>&1; then echo 'Accepted a foreign frontend listener' >&2; exit 1; fi
grep -q 'Port 8090 is occupied' "$test_dir/conflict.log"
kill -0 "$listener_pid"
kill "$listener_pid"; wait "$listener_pid" 2>/dev/null || true; listener_pid=''

python3 -m http.server 8080 --bind 127.0.0.1 >"$test_dir/listener.log" 2>&1 &
listener_pid=$!
sleep 1
if installer --start >"$test_dir/conflict.log" 2>&1; then echo 'Accepted a foreign API listener' >&2; exit 1; fi
grep -q 'Port 8080 is occupied' "$test_dir/conflict.log"
kill -0 "$listener_pid"
kill "$listener_pid"; wait "$listener_pid" 2>/dev/null || true; listener_pid=''

installer --start
assert_running
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]
installer --no-start
for component in api worker scheduler web; do [[ ! -f "$checkout/.opensnow/run/$component.pid" ]]; done
installer --start
assert_running
installer --status
verified=1
printf '%s\n' 'Local runtime fresh install, spaces/absolute path, proxy, real jobs, lifecycle, rebuild, persistence, port conflicts and partial-start cleanup passed.'
if [[ "${LOCAL_RUNTIME_KEEP:-0}" == 1 ]]; then printf 'Verification checkout: %s\n' "$checkout"; fi
