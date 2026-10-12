#!/usr/bin/env bash
set -Eeuo pipefail
trap 'printf "[FAIL] Native lifecycle verification failed at line %s.\n" "$LINENO" >&2' ERR

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
  mkdir -p "$repo_dir/.opensnow/local-runtime-verification"
  cp -- "$test_dir/"*.log "$repo_dir/.opensnow/local-runtime-verification/" 2>/dev/null || true
  cp -- "$checkout/.opensnow/logs/"*.log "$repo_dir/.opensnow/local-runtime-verification/" 2>/dev/null || true
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
SESSION_CLEANUP_INTERVAL=5s
EOF
installer() { env -u CI bash "$checkout/install.sh" --local "$@"; }
sql() {
  docker compose -p "$project" -f "$checkout/docker-compose.yml" exec -T -e MYSQL_PWD=local-mysql-verification mysql mysql -u opensnow opensnow -N -e "$1"
}
pid_of() { awk '{print $1}' "$checkout/.opensnow/run/$1.pid"; }
terminate_component() {
  local pid attempt
  pid=$(pid_of "$1")
  kill -TERM "$pid"
  for ((attempt=0; attempt<50; attempt++)); do
    if [[ ! -r "/proc/$pid/stat" || "$(ps -o stat= -p "$pid")" == Z* ]]; then return; fi
    sleep 0.1
  done
  echo "$1 did not terminate" >&2; return 1
}
scheduled_id() {
  python3 - "$1" <<'PY'
import json, sys
with open(sys.argv[1]) as log:
    for line in log:
        try:
            entry = json.loads(line)
        except ValueError:
            continue
        if entry.get('msg') == 'scheduled durable job' and entry.get('created') is True:
            job_id = entry['job_id']
            assert isinstance(job_id, str) and job_id.isdecimal() and int(job_id) > 0
            print(job_id)
            break
    else:
        raise AssertionError('Scheduler did not report a newly created durable job')
PY
}
assert_completed_job() {
  local job_id=$1 deleted=$2 state attempt
  for ((attempt=0; attempt<30; attempt++)); do
    state=$(sql "SELECT status FROM jobs WHERE id=$job_id AND job_type='system.session_cleanup' AND scope_key='system'")
    if [[ "$state" == COMPLETED ]]; then break; fi
    [[ "$state" != FAILED && "$state" != CANCELLED ]]
    sleep 1
  done
  [[ "$state" == COMPLETED ]]
  [[ "$(sql "SELECT CONCAT(progress, ':', attempt_count, ':', JSON_UNQUOTE(JSON_EXTRACT(result, '$.deleted_sessions'))) FROM jobs WHERE id=$job_id")" == "100:1:$deleted" ]]
  [[ "$(sql "SELECT COUNT(*) FROM job_attempts WHERE job_id=$job_id AND status='COMPLETED' AND finished_at IS NOT NULL AND worker_id LIKE '%:$(pid_of worker):%/jobs/%'")" == 1 ]]
  [[ "$(sql "SELECT COUNT(*) FROM job_logs WHERE job_id=$job_id AND message IN ('Job attempt started','Job handler completed')")" == 2 ]]
  printf 'Verified scheduler job %s: COMPLETED by worker PID %s, deleted_sessions=%s\n' "$job_id" "$(pid_of worker)" "$deleted"
}
assert_session() {
  python3 - "$1" "$test_dir/session-cookies" "$test_dir/incident-proof" <<'PY'
import http.cookiejar, json, sys, urllib.request
cookies = http.cookiejar.MozillaCookieJar(sys.argv[2])
cookies.load(ignore_discard=True)
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))
session = json.load(client.open(sys.argv[1] + '/api/v1/auth/me'))
assert session['tenant']['slug'] == 'e2e'
assert session['user']['email'] == 'admin@example.test'
assert 'admin.rbac.manage' in session['permissions']
with open(sys.argv[3]) as proof_file:
    proof = json.load(proof_file)
record = json.load(client.open(sys.argv[1] + '/api/v1/incidents/' + proof['record']['id']))
assert record == proof['record'], 'Incident/number/version/assignment changed across lifecycle'
history = json.load(client.open(sys.argv[1] + '/api/v1/incidents/' + record['id'] + '/history'))
assert history == proof['history'], 'Incident business history changed across lifecycle'
PY
}
assert_running() {
  local component pid session terminal
  for component in api worker scheduler web; do
    pid=$(pid_of "$component")
    kill -0 "$pid"
    [[ "$(stat -c %u "/proc/$pid")" == "$UID" ]]
    # Closing the installer's terminal must not terminate any runtime component.
    read -r session terminal < <(ps -o sid=,tty= -p "$pid")
    [[ "$session" == "$pid" && "$terminal" == '?' ]]
  done
}
stop_listener() { kill "$listener_pid"; wait "$listener_pid" 2>/dev/null || true; listener_pid=''; }
occupy_api_ports() {
  rm -f "$test_dir/listeners-ready"
  python3 - "$1" "$2" "${3:-0}" "$test_dir/listeners-ready" <<'PY' >"$test_dir/listener.log" 2>&1 &
import pathlib, socket, sys, time
listeners = []
for port in range(int(sys.argv[1]), int(sys.argv[2]) + 1):
    if port == int(sys.argv[3]):
        continue
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(('127.0.0.1', port))
    listener.listen()
    listeners.append(listener)
pathlib.Path(sys.argv[4]).touch()
while True:
    time.sleep(1)
PY
  listener_pid=$!
  for _ in {1..50}; do
    kill -0 "$listener_pid" || { cat "$test_dir/listener.log" >&2; return 1; }
    if [[ -f "$test_dir/listeners-ready" ]]; then return; fi
    sleep 0.1
  done
  echo 'Foreign listeners did not start' >&2; return 1
}

# First run creates .env and runs from the script's location, not the caller's cwd.
cd /tmp
occupy_api_ports 8080 8081
installer
[[ -f "$checkout/.env" ]]
[[ "$(cat "$checkout/.opensnow/run/api-port")" == 8082 ]]
kill -0 "$listener_pid"
assert_running
curl --fail --silent http://127.0.0.1:5173/ | grep -q '/src/main.tsx'
curl --fail --silent http://127.0.0.1:5173/health/ready | grep -q '"status":"ok"'
python3 - "$test_dir/session-cookies" "$test_dir/incident-proof" <<'PY'
import http.cookiejar, json, sys, time, urllib.request
base = 'http://127.0.0.1:5173'
cookies = http.cookiejar.MozillaCookieJar(sys.argv[1])
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))
status = json.load(client.open(base + '/api/v1/setup'))
assert status['required'] is True
body = json.dumps(dict(tenant='e2e', tenant_name='Local runtime verification', email='admin', display_name='Administrator', password='local-runtime-admin-password')).encode()
request = urllib.request.Request(base + '/api/v1/setup', data=body, headers={'Content-Type':'application/json', 'X-Setup-Token':status['token'], 'Origin':base})
assert client.open(request).status == 201
assert json.load(client.open(base + '/api/v1/setup')) == {'required':False}
login = urllib.request.Request(base + '/api/v1/auth/login', data=json.dumps(dict(tenant='e2e', email='admin', password='local-runtime-admin-password')).encode(), headers={'Content-Type':'application/json'})
session = json.load(client.open(login))
cookies.save(ignore_discard=True)
csrf = next(cookie.value for cookie in cookies if cookie.name == 'opensnow_csrf')
headers = {'Content-Type':'application/json', 'X-CSRF-Token':csrf, 'Idempotency-Key':'native-incident-restart-proof'}
body = dict(title='Native restart proof', description='Persistent incident and worker history', category='other', subcategory='other', impact=3, urgency=3)
create = urllib.request.Request(base + '/api/v1/incidents/', data=json.dumps(body).encode(), headers=headers)
response = client.open(create)
assert response.status == 201
record = json.load(response)
repeat = client.open(create)
assert repeat.status == 200 and json.load(repeat)['id'] == record['id']
assignment = urllib.request.Request(base + '/api/v1/incidents/' + record['id'] + '/assignment', method='PUT', data=json.dumps(dict(version=record['version'], assigned_to=session['user']['id'], assignment_group_id=None)).encode(), headers=headers)
record = json.load(client.open(assignment))
transition = urllib.request.Request(base + '/api/v1/incidents/' + record['id'] + '/transition', data=json.dumps(dict(version=record['version'], state='IN_PROGRESS')).encode(), headers=headers)
record = json.load(client.open(transition))
assert record['version'] == '3' and record['number'] == 'INC0000001'
for _ in range(100):
    history = json.load(client.open(base + '/api/v1/incidents/' + record['id'] + '/history'))
    if not history['pending'] and len(history['entries']) == 3:
        break
    time.sleep(.2)
assert [entry['type'] for entry in history['entries']] == ['incident.state.changed','incident.assigned','incident.created']
assert not history['pending']
for entry in history['entries']:
    assert entry['event_time'] == entry['data']['occurred_at']
    assert entry['data']['incident_version'] == entry['data']['after']['version']
with open(sys.argv[2], 'w') as proof_file:
    json.dump(dict(record=record, history=history), proof_file)
PY
original_hash="$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")"
[[ -n "$original_hash" ]]
original_env=$(sha256sum "$checkout/.env")
original_tenant="$(sql 'SELECT CONCAT(id, ":", slug, ":", name, ":", version) FROM tenants')"
# Correlate an ID from the installed scheduler's JSON log with this worker's
# durable attempt, result and execution logs, rather than count unrelated jobs.
assert_completed_job "$(scheduled_id "$checkout/.opensnow/logs/scheduler.log")" 0
api_before=$(pid_of api)
web_before=$(pid_of web)
worker_before=$(pid_of worker)
scheduler_before=$(pid_of scheduler)
installer --start
[[ "$(pid_of api)" == "$api_before" && "$(pid_of web)" == "$web_before" ]]
[[ "$(pid_of worker)" == "$worker_before" && "$(pid_of scheduler)" == "$scheduler_before" ]]
installer --status
assert_session http://127.0.0.1:5173
stop_listener

# A dead background process must be visible while synchronous API/auth still work.
# Freeze scheduling and drain existing work to prove status performs no writes.
terminate_component scheduler
for _ in {1..30}; do
  if [[ "$(sql "SELECT COUNT(*) FROM jobs WHERE status IN ('QUEUED','RUNNING','RETRYING')")" == 0 ]]; then break; fi
  sleep 0.1
done
terminate_component worker
jobs_before="$(sql 'SELECT CONCAT((SELECT COUNT(*) FROM jobs), ":", (SELECT COUNT(*) FROM job_attempts), ":", (SELECT COUNT(*) FROM sessions))')"
state_before=$(find "$checkout/.opensnow/run" -type f -exec sha256sum {} + | sort | sha256sum)
if installer --status >"$test_dir/background-stopped.log" 2>&1; then echo 'Dead background processes reported healthy' >&2; exit 1; fi
grep -q 'worker: stale/unrelated' "$test_dir/background-stopped.log"
grep -q 'scheduler: stale/unrelated' "$test_dir/background-stopped.log"
grep -q 'api: ready' "$test_dir/background-stopped.log"
grep -q 'web: ready' "$test_dir/background-stopped.log"
[[ "$(sql 'SELECT CONCAT((SELECT COUNT(*) FROM jobs), ":", (SELECT COUNT(*) FROM job_attempts), ":", (SELECT COUNT(*) FROM sessions))')" == "$jobs_before" ]]
[[ "$(find "$checkout/.opensnow/run" -type f -exec sha256sum {} + | sort | sha256sum)" == "$state_before" ]]
assert_session http://127.0.0.1:5173

# Inspect local processes even with an unavailable Docker Engine.
if DOCKER_HOST=tcp://127.0.0.1:1 installer --status >"$test_dir/docker-status.log" 2>&1; then echo 'Unavailable Docker reported healthy' >&2; exit 1; fi
grep -q 'api: ready' "$test_dir/docker-status.log"
grep -q 'worker: stale/unrelated' "$test_dir/docker-status.log"
grep -q 'mysql: unavailable' "$test_dir/docker-status.log"

# Prove the real handler's side effect with a second known job on disposable SQL.
[[ "$(sql "SELECT COUNT(*) FROM jobs WHERE status IN ('QUEUED','RUNNING','RETRYING')")" == 0 ]]
expired_id=$(sql "INSERT INTO sessions(tenant_id,user_id,token_hash,csrf_hash,expires_at) SELECT tm.tenant_id,tm.user_id,UNHEX(SHA2('local-expired-token',256)),UNHEX(SHA2('local-expired-csrf',256)),UTC_TIMESTAMP()-INTERVAL 1 HOUR FROM tenant_memberships tm JOIN users u ON u.id=tm.user_id WHERE u.email='admin@example.test'; SELECT LAST_INSERT_ID()")
sleep 6 # A fresh idempotency bucket, beyond the stopped scheduler's final tick.
# shellcheck source=/dev/null # Configuration generated in the disposable checkout.
(cd "$checkout"; set -a; source .env; set +a; ./bin/opensnow-scheduler --once) >"$test_dir/scheduler-once.log" 2>&1
cleanup_id=$(scheduled_id "$test_dir/scheduler-once.log")
[[ "$(sql "SELECT status FROM jobs WHERE id=$cleanup_id")" == QUEUED ]]
[[ "$(sql "SELECT COUNT(*) FROM sessions WHERE id=$expired_id")" == 1 ]]
installer --start
assert_completed_job "$cleanup_id" 1
[[ "$(sql "SELECT COUNT(*) FROM sessions WHERE id=$expired_id")" == 0 ]]
assert_session http://127.0.0.1:5173
worker_before=$(pid_of worker)
scheduler_before=$(pid_of scheduler)

# A saved API port taken after API exit also updates an already-running Vite proxy.
kill -TERM "$api_before"
sleep 1
occupy_api_ports 8082 8082
installer --start
[[ "$(cat "$checkout/.opensnow/run/api-port")" == 8080 ]]
[[ "$(pid_of web)" != "$web_before" ]]
[[ "$(pid_of worker)" == "$worker_before" && "$(pid_of scheduler)" == "$scheduler_before" ]]
curl --fail --silent http://127.0.0.1:5173/health/ready | grep -q '"status":"ok"'
kill -0 "$listener_pid"
stop_listener
api_before=$(pid_of api)
web_before=$(pid_of web)

# A failed new scheduler startup rolls back the newly started worker, preserving old API/Vite.
kill -TERM "$(pid_of scheduler)" "$(pid_of worker)"
sleep 2
mv "$checkout/bin/opensnow-scheduler" "$checkout/bin/scheduler-original"
printf '#!/bin/sh\nexit 42\n' >"$checkout/bin/opensnow-scheduler"
chmod +x "$checkout/bin/opensnow-scheduler"
if installer --start >"$test_dir/failure.log" 2>&1; then echo 'Accepted failed scheduler startup' >&2; exit 1; fi
grep -q 'scheduler' "$test_dir/failure.log"
grep -q 'Runtime logs:' "$test_dir/failure.log"
grep -q 'Process state after startup rollback' "$test_dir/failure.log"
grep -q 'api: ready' "$test_dir/failure.log"
grep -q 'web: ready' "$test_dir/failure.log"
grep -q 'worker: stopped' "$test_dir/failure.log"
if grep -q 'unbound variable' "$test_dir/failure.log"; then echo 'Process-exit race caused an unexpected shell error' >&2; exit 1; fi
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
assert_session http://127.0.0.1:5173

# Port selection persists across status/restart/start without repeating the environment variable.
OPENSNOW_WEB_PORT=8090 installer --restart
installer --status | tee "$test_dir/status.log"
grep -q 'http://127.0.0.1:8090' "$test_dir/status.log"
assert_session http://127.0.0.1:8090
installer --restart
[[ "$(cat "$checkout/.opensnow/run/web-port")" == 8090 ]]
assert_session http://127.0.0.1:8090
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

# Every lower API port is unavailable, including the frontend's reserved port.
occupy_api_ports 8080 8099 8090
installer --start
[[ "$(cat "$checkout/.opensnow/run/api-port")" == 8100 ]]
curl --fail --silent http://127.0.0.1:8090/health/ready | grep -q '"status":"ok"'
curl --fail --silent http://127.0.0.1:8090/api/v1/setup | grep -q '"required":false'
api_before=$(pid_of api)
installer --start
[[ "$(pid_of api)" == "$api_before" ]]
installer --status | tee "$test_dir/status.log"
grep -q 'API address: http://127.0.0.1:8100' "$test_dir/status.log"
assert_session http://127.0.0.1:8090
kill -0 "$listener_pid"
installer --stop
stop_listener

occupy_api_ports 8080 8100 8090
if installer --start >"$test_dir/conflict.log" 2>&1; then echo 'Exceeded API port limit' >&2; exit 1; fi
grep -q 'No available API port in 8080-8100' "$test_dir/conflict.log"
for component in api worker scheduler web; do [[ ! -f "$checkout/.opensnow/run/$component.pid" ]]; done
kill -0 "$listener_pid"
stop_listener

installer --start
assert_running
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]
installer --no-start
for component in api worker scheduler web; do [[ ! -f "$checkout/.opensnow/run/$component.pid" ]]; done
installer --start
assert_running
installer --status

# Explicit bootstrap with the obsolete default password must fail, never reset
# the administrator that the WebUI created with a custom password.
if installer --bootstrap --no-start >"$test_dir/bootstrap.log" 2>&1; then echo 'Accepted an incorrect existing administrator password' >&2; exit 1; fi
grep -q 'bootstrap admin already exists and the supplied password does not match' "$test_dir/bootstrap.log"
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]
[[ "$(sql 'SELECT CONCAT(id, ":", slug, ":", name, ":", version) FROM tenants')" == "$original_tenant" ]]
[[ "$(sha256sum "$checkout/.env")" == "$original_env" ]]
installer --start
assert_session http://127.0.0.1:8090

# Ordinary uninstall removes generated runtime files, preserving credentials,
# account/data volume, source files, logs and unrelated files in bin/.
printf 'operator-owned\n' >"$checkout/bin/preserved-file"
installer --uninstall
[[ ! -e "$checkout/.opensnow/run" && ! -e "$checkout/bin/opensnow-worker" && ! -e "$checkout/web/node_modules" ]]
[[ -f "$checkout/bin/preserved-file" && -f "$checkout/.opensnow/logs/worker.log" && -f "$checkout/install.sh" ]]
[[ "$(sha256sum "$checkout/.env")" == "$original_env" ]]
docker volume inspect "${project}_mysql-data" >/dev/null
installer
assert_running
[[ "$(sql "SELECT password_hash FROM users WHERE email='admin@example.test'")" == "$original_hash" ]]
[[ "$(sql 'SELECT CONCAT(id, ":", slug, ":", name, ":", version) FROM tenants')" == "$original_tenant" ]]
assert_session http://127.0.0.1:5173
installer --status
verified=1
printf '%s\n' 'Local runtime fresh install, known-ID scheduler/worker execution, incident/number/assignment/history persistence across restarts and reinstall, session cleanup, read-only status, background failure/API independence, lifecycle, port/proxy changes and bootstrap safety passed.'
if [[ "${LOCAL_RUNTIME_KEEP:-0}" == 1 ]]; then printf 'Verification checkout: %s\n' "$checkout"; fi
