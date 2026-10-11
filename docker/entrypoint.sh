#!/bin/sh
set -eu

# Native DevBox database bindings use DB_*; the application uses MYSQL_*.
export MYSQL_HOST="${DB_HOST:-${MYSQL_HOST:-}}"
export MYSQL_PORT="${DB_PORT:-${MYSQL_PORT:-3306}}"
export MYSQL_DATABASE="${DB_NAME:-${MYSQL_DATABASE:-}}"
export MYSQL_USER="${DB_USER:-${MYSQL_USER:-}}"
export MYSQL_PASSWORD="${DB_PASSWORD:-${MYSQL_PASSWORD:-}}"
: "${MYSQL_HOST:?Bind a MySQL database in DevBox before starting the stack}"
: "${MYSQL_DATABASE:?MySQL database is required}"
: "${MYSQL_USER:?MySQL application user is required}"
: "${MYSQL_PASSWORD:?MySQL application password is required}"

case "${1:-api}" in
  api)
    : "${BOOTSTRAP_ADMIN_PASSWORD:?Set BOOTSTRAP_ADMIN_PASSWORD in DevBox SecretStore}"
    opensnow-migrate up
    opensnow-bootstrap
    unset BOOTSTRAP_ADMIN_PASSWORD
    exec opensnow-api
    ;;
  worker|scheduler|migrate|bootstrap)
    command=$1
    shift
    exec "opensnow-$command" "$@"
    ;;
  *)
    echo 'Expected api, worker, scheduler, migrate or bootstrap' >&2
    exit 2
    ;;
esac
