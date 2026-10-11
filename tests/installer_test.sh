#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture="$(mktemp -d /tmp/opensnow-installer-unit.XXXXXX)"
foreign_pid=''
owned_pid=''
web_pid=''
cleanup() {
  if [[ -n "$owned_pid" ]]; then kill "$owned_pid" 2>/dev/null || true; fi
  if [[ -n "$foreign_pid" ]]; then kill "$foreign_pid" 2>/dev/null || true; fi
  if [[ -n "$web_pid" ]]; then kill "$web_pid" 2>/dev/null || true; fi
  rm -rf -- "$fixture"
}
trap cleanup EXIT
# shellcheck source=../install.sh
source "$repo_dir/install.sh"
ROOT_DIR="$fixture/checkout with spaces"
RUN_DIR="$ROOT_DIR/.opensnow/run"
LOG_DIR="$ROOT_DIR/.opensnow/logs"
BIN_DIR="$ROOT_DIR/bin"
WEB_PORT_FILE="$RUN_DIR/web-port"
API_PORT_FILE="$RUN_DIR/api-port"
PROJECT_FILE="$RUN_DIR/compose-project"
ensure_state_dirs

for args in '--start --stop' '--status --no-start' '--restart --bootstrap' '--purge-data' '--help --start' '--uninstall --no-start'; do
  read -r -a values <<<"$args"
  if bash "$repo_dir/install.sh" --local "${values[@]}" >"$fixture/error" 2>&1; then echo "Accepted conflicting arguments: $args" >&2; exit 1; fi
  grep -Eq 'Conflicting|cannot be combined|requires' "$fixture/error"
done
for port in 0 65536 -1 bad '8090;echo injected' 08090; do
  if valid_port "$port"; then echo "Accepted invalid port: $port" >&2; exit 1; fi
done
valid_port 8090
valid_api_port 8080
valid_api_port 8100
for port in 8079 8101 bad '8081;echo injected'; do
  if valid_api_port "$port"; then echo "Accepted invalid API port: $port" >&2; exit 1; fi
done
WEB_PORT=8090
API_PORT=8082
COMPOSE_PROJECT_NAME=installer-unit
save_instance
REQUESTED_WEB_PORT=''
load_instance
[[ "$WEB_PORT" == 8090 && "$API_PORT" == 8082 ]]
ACTION=status
REQUESTED_WEB_PORT=9000
load_instance
[[ "$WEB_PORT" == 8090 ]]
ACTION=restart
load_instance
[[ "$WEB_PORT" == 9000 ]]

# Exercise real process identity and PID reuse defenses, without any application mocks.
printf '#include <unistd.h>\nint main(void) { sleep(600); return 0; }\n' | cc -x c - -o "$BIN_DIR/opensnow-api"
(cd "$ROOT_DIR" && exec "$BIN_DIR/opensnow-api") &
owned_pid=$!
sleep 0.1
printf '%s %s\n' "$owned_pid" "$(process_stamp "$owned_pid")" >"$RUN_DIR/api.pid"
pid_running api
printf '%s invalid-start-time\n' "$owned_pid" >"$RUN_DIR/api.pid"
if pid_running api; then echo 'Accepted reused PID metadata' >&2; exit 1; fi
stop_pid api
kill -0 "$owned_pid"
printf '%s\n' "$owned_pid" >"$RUN_DIR/api.pid"
pid_running api # Safe adoption of an old installer PID file.
stop_pid api
wait "$owned_pid" 2>/dev/null || true
owned_pid=''

sleep 600 &
foreign_pid=$!
printf '%s %s\n' "$foreign_pid" "$(process_stamp "$foreign_pid")" >"$RUN_DIR/api.pid"
if pid_running api; then echo 'Accepted an unrelated process' >&2; exit 1; fi
stop_pid api
kill -0 "$foreign_pid"

mkdir -p "$ROOT_DIR/web/node_modules/vite/bin" "$ROOT_DIR/web/node_modules/.bin"
printf 'setInterval(() => {}, 1000);\n' >"$ROOT_DIR/web/node_modules/vite/bin/vite.js"
ln -s ../vite/bin/vite.js "$ROOT_DIR/web/node_modules/.bin/vite"
(cd "$ROOT_DIR/web" && exec node ./node_modules/.bin/vite --port 8092) &
web_pid=$!
sleep 0.1
printf '%s\n' "$web_pid" >"$RUN_DIR/web.pid"
pid_running web
rm -- "$WEB_PORT_FILE"
REQUESTED_WEB_PORT=''
load_instance
[[ "$WEB_PORT" == 8092 ]]
stop_pid web
wait "$web_pid" 2>/dev/null || true
web_pid=''

exec 8>"$RUN_DIR/installer.lock"
flock -n 8
cp "$repo_dir/install.sh" "$ROOT_DIR/install.sh"
if bash "$ROOT_DIR/install.sh" --local --status >"$fixture/error" 2>&1; then echo 'Accepted concurrent lifecycle action' >&2; exit 1; fi
grep -q 'Another installer command' "$fixture/error"
exec 8>&-
printf '%s\n' 'Installer argument, port/state, process ownership, PID reuse and lifecycle-lock tests passed.'
