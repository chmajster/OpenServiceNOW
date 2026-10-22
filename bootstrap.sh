#!/usr/bin/env bash
set -Eeuo pipefail

DEFAULT_INSTALL_DIR=/opt/OpenServiceNow
DEFAULT_REPOSITORY_URL=https://github.com/chmajster/OpenServiceNOW.git
DEFAULT_BRANCH=main

INSTALL_DIR=${OPENSNOW_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}
REPOSITORY_URL=${OPENSNOW_REPOSITORY_URL:-$DEFAULT_REPOSITORY_URL}
BRANCH=${OPENSNOW_BRANCH:-$DEFAULT_BRANCH}
OPERATOR_USER=''
OPERATOR_UID=''
OPERATOR_GROUP=''
declare -a INSTALLER_ARGS=()

info() { printf '[INFO] %s\n' "$*"; }
ok() { printf '[ OK ] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
OpenServiceNOW remote bootstrap (Linux / WSL)

Usage:
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    https://raw.githubusercontent.com/chmajster/OpenServiceNOW/main/bootstrap.sh | bash

The default command installs or updates the main branch in /opt/OpenServiceNow
and runs install.sh --local-docker. Installer lifecycle arguments are forwarded:

  ... | bash -s -- --status
  ... | bash -s -- --restart
  ... | bash -s -- --no-start

Run the bootstrap as your ordinary user, without prefixing the pipeline with sudo.
It requests sudo only for package management and creation of /opt/OpenServiceNow.
EOF
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1. $2"
}

run_privileged() {
  require_command sudo "Install sudo or create ${INSTALL_DIR} and install prerequisites manually."
  sudo -- "$@"
}

validate_context() {
  [[ "$(uname -s)" == Linux ]] || fail 'The remote bootstrap supports Linux and WSL.'
  (( EUID != 0 )) || fail 'Run this bootstrap as your ordinary user, without sudo. It requests limited privileges when needed.'
  OPERATOR_USER=$(id -un)
  OPERATOR_UID=$(id -u)
  OPERATOR_GROUP=$(id -gn)
  [[ -n "$OPERATOR_USER" && "$OPERATOR_UID" =~ ^[1-9][0-9]*$ ]] || fail 'Cannot identify a non-root installation user.'
  [[ "$INSTALL_DIR" == /* && "$INSTALL_DIR" != / && "$INSTALL_DIR" != /opt ]] || fail 'OPENSNOW_INSTALL_DIR must be a specific absolute path.'
  [[ "$INSTALL_DIR" != *$'\n'* && "$INSTALL_DIR" != *$'\r'* ]] || fail 'OPENSNOW_INSTALL_DIR contains an invalid newline.'
  [[ "$BRANCH" =~ ^[A-Za-z0-9._/-]+$ && "$BRANCH" != -* ]] || fail 'OPENSNOW_BRANCH contains invalid characters.'
  case "/$BRANCH/" in */../*) fail 'OPENSNOW_BRANCH cannot contain a parent-directory segment.' ;; esac
  [[ "$REPOSITORY_URL" != -* && "$REPOSITORY_URL" != *$'\n'* && "$REPOSITORY_URL" != *$'\r'* ]] || fail 'OPENSNOW_REPOSITORY_URL is invalid.'
}

install_git() {
  if command -v git >/dev/null 2>&1; then return; fi
  command -v apt-get >/dev/null 2>&1 || fail 'Git is required. Automatic installation is supported on apt-based Ubuntu/Debian systems.'
  info 'Installing Git and CA certificates.'
  run_privileged env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update
  run_privileged env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y --no-remove git ca-certificates
  require_command git 'Git installation did not provide the git command.'
}

prepare_install_directory() {
  local parent owner
  parent=$(dirname -- "$INSTALL_DIR")
  if [[ -L "$INSTALL_DIR" ]]; then fail "Refusing symbolic-link installation target: ${INSTALL_DIR}."; fi
  if [[ -e "$INSTALL_DIR" ]]; then
    [[ -d "$INSTALL_DIR" ]] || fail "Installation target exists and is not a directory: ${INSTALL_DIR}."
    owner=$(stat -c %u -- "$INSTALL_DIR")
    [[ "$owner" == "$OPERATOR_UID" ]] || fail "Installation target must be owned by ${OPERATOR_USER}: ${INSTALL_DIR}."
    return
  fi
  if [[ -d "$parent" && -w "$parent" ]]; then
    install -d -m 0755 -- "$INSTALL_DIR"
  else
    info "Creating ${INSTALL_DIR} for ${OPERATOR_USER}."
    run_privileged install -d -m 0755 -o "$OPERATOR_USER" -g "$OPERATOR_GROUP" -- "$INSTALL_DIR"
  fi
}

ensure_clean_checkout() {
  local changes
  changes=$(git -C "$INSTALL_DIR" status --porcelain --untracked-files=all)
  [[ -z "$changes" ]] || fail "Local source changes exist in ${INSTALL_DIR}; commit, move or remove them before updating. Runtime data in ignored .env/.opensnow paths is preserved."
}

clone_repository() {
  if [[ -n "$(find "$INSTALL_DIR" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    fail "Installation target is not empty and is not an OpenServiceNOW Git checkout: ${INSTALL_DIR}."
  fi
  info "Downloading OpenServiceNOW ${BRANCH} into ${INSTALL_DIR}."
  env GIT_TERMINAL_PROMPT=0 git clone --branch "$BRANCH" --single-branch --origin origin -- "$REPOSITORY_URL" "$INSTALL_DIR"
  ok "Downloaded OpenServiceNOW into ${INSTALL_DIR}."
}

update_repository() {
  local current_branch current_commit origin remote_commit
  origin=$(git -C "$INSTALL_DIR" remote get-url origin 2>/dev/null || true)
  [[ "$origin" == "$REPOSITORY_URL" ]] || fail "Existing checkout origin is '${origin:-missing}', expected '${REPOSITORY_URL}'."
  current_branch=$(git -C "$INSTALL_DIR" symbolic-ref --quiet --short HEAD 2>/dev/null || true)
  [[ "$current_branch" == "$BRANCH" ]] || fail "Existing checkout is on '${current_branch:-a detached HEAD}', expected '${BRANCH}'."
  ensure_clean_checkout
  info "Checking GitHub for OpenServiceNOW ${BRANCH} updates."
  env GIT_TERMINAL_PROMPT=0 git -C "$INSTALL_DIR" fetch --prune origin "$BRANCH"
  current_commit=$(git -C "$INSTALL_DIR" rev-parse HEAD)
  remote_commit=$(git -C "$INSTALL_DIR" rev-parse FETCH_HEAD)
  git -C "$INSTALL_DIR" merge-base --is-ancestor "$current_commit" "$remote_commit" ||
    fail "The installed ${BRANCH} branch has commits not present upstream; refusing to overwrite it."
  if [[ "$current_commit" == "$remote_commit" ]]; then
    ok "OpenServiceNOW is already current (${current_commit:0:12})."
    return
  fi
  git -C "$INSTALL_DIR" merge --ff-only --no-edit "$remote_commit"
  ok "Updated OpenServiceNOW from ${current_commit:0:12} to ${remote_commit:0:12}."
}

sync_repository() {
  prepare_install_directory
  if [[ -e "$INSTALL_DIR/.git" ]]; then update_repository; else clone_repository; fi
  [[ -f "$INSTALL_DIR/install.sh" && -f "$INSTALL_DIR/docker/local-runtime.sh" ]] ||
    fail "Downloaded checkout is incomplete: ${INSTALL_DIR}."
}

prepare_installer_args() {
  local argument mode_seen=0
  INSTALLER_ARGS=("$@")
  for argument in "${INSTALLER_ARGS[@]}"; do
    case "$argument" in --local|--local-docker) mode_seen=1 ;; esac
  done
  if (( mode_seen == 0 )); then INSTALLER_ARGS=(--local-docker "${INSTALLER_ARGS[@]}"); fi
}

main() {
  if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then usage; return; fi
  validate_context
  require_command dirname 'Install coreutils.'
  require_command find 'Install findutils.'
  require_command install 'Install coreutils.'
  require_command stat 'Install coreutils.'
  install_git
  sync_repository
  prepare_installer_args "$@"
  info "Running ${INSTALL_DIR}/install.sh ${INSTALLER_ARGS[*]}"
  bash "$INSTALL_DIR/install.sh" "${INSTALLER_ARGS[@]}"
}

if [[ -z "${BASH_SOURCE[0]:-}" || "${BASH_SOURCE[0]:-}" == "$0" || "${BASH_SOURCE[0]:-}" == /dev/stdin || "${BASH_SOURCE[0]:-}" == /dev/fd/* ]]; then
  main "$@"
fi
