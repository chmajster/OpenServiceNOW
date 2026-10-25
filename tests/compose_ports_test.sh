#!/usr/bin/env bash
set -Eeuo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture=$(mktemp -d /tmp/opensnow-compose-ports.XXXXXX)
listener_pid=''
cleanup() {
  if [[ -n "$listener_pid" ]]; then kill "$listener_pid" 2>/dev/null || true; wait "$listener_pid" 2>/dev/null || true; fi
  rm -rf -- "$fixture"
}
trap cleanup EXIT
# shellcheck source=../install.sh
source "$repo_dir/install.sh"
export OPENSNOW_DOCKER_PORT=8080
ENV_FILE="$fixture/.env"
RUN_DIR="$fixture/run"
WEB_PORT_FILE="$RUN_DIR/web-port"
API_PORT_FILE="$RUN_DIR/api-port"
PROJECT_FILE="$RUN_DIR/compose-project"
mkdir -p "$RUN_DIR"
cp "$ENV_EXAMPLE" "$ENV_FILE"
printf '\nCOMPOSE_PROJECT_NAME=opensnow-ports-%s-%s\n' "$UID" "$RANDOM" >>"$ENV_FILE"

# A real foreign listener must block the native runtime but never Docker's
# private infrastructure. Use a kernel-selected port to avoid touching services.
python3 - "$fixture/listener-port" <<'PY' &
import pathlib, socket, sys, time
with socket.socket() as listener:
    listener.bind(('127.0.0.1', 0))
    listener.listen()
    pathlib.Path(sys.argv[1]).write_text(str(listener.getsockname()[1]))
    while True:
        time.sleep(1)
PY
listener_pid=$!
for _ in {1..50}; do
  kill -0 "$listener_pid"
  if [[ -s "$fixture/listener-port" ]]; then break; fi
  sleep 0.1
done
port=$(cat "$fixture/listener-port")
port_busy "$port"
MODE=local-docker
prepare_env
MYSQL_PORT=$port REDIS_PORT=$port check_dependency_ports
kill -0 "$listener_pid"
for service in mysql redis; do
  (
    MODE=local
    MYSQL_PORT=1
    REDIS_PORT=1
    if [[ "$service" == mysql ]]; then MYSQL_PORT=$port; else REDIS_PORT=$port; fi
    check_dependency_ports
  ) >"$fixture/native-error" 2>&1 && { echo "Accepted native $service port conflict" >&2; exit 1; }
  grep -q "outside this instance's $service container" "$fixture/native-error"
done

# Docker must also allow the same configured host port for both dependencies,
# including a port in the WebUI range, because neither dependency publishes it.
printf '\nMYSQL_PORT=8080\nREDIS_ADDR=127.0.0.1:8080\n' >>"$ENV_FILE"
prepare_env
if (MODE=local; prepare_env) >"$fixture/native-error" 2>&1; then
  echo 'Accepted identical native infrastructure ports' >&2; exit 1
fi
grep -q 'MySQL and Redis must use separate ports' "$fixture/native-error"
compose config --format json >"$fixture/docker.json"
docker_compose config --format json >"$fixture/docker-runtime.json"
MODE=local
compose config --format json >"$fixture/native.json"
python3 - "$fixture" <<'PY'
import json, pathlib, sys
fixture = pathlib.Path(sys.argv[1])
docker = json.loads((fixture / 'docker.json').read_text())
assert docker == json.loads((fixture / 'docker-runtime.json').read_text())
for name in ('mysql', 'redis', 'api', 'worker', 'scheduler'):
    assert not docker['services'][name].get('ports'), name
for name in ('api', 'worker', 'scheduler'):
    env = docker['services'][name]['environment']
    assert env['MYSQL_HOST'] == 'mysql'
    assert env['MYSQL_PORT'] == '3306'
    assert env['REDIS_ADDR'] == 'redis:6379'
frontend = docker['services']['frontend']['ports']
assert len(frontend) == 1
assert frontend[0]['host_ip'] == '127.0.0.1'
assert frontend[0]['published'] == '8080'
native = json.loads((fixture / 'native.json').read_text())
for name, target in (('mysql', 3306), ('redis', 6379)):
    ports = native['services'][name]['ports']
    assert len(ports) == 1
    assert ports[0]['host_ip'] == '127.0.0.1'
    assert ports[0]['published'] == '8080'
    assert ports[0]['target'] == target
assert docker['volumes']['mysql-data']['name'] == native['volumes']['mysql-data']['name']
PY
kill -0 "$listener_pid"
echo 'Compose private Docker ports, native conflict protection and SQL volume preservation passed.'
