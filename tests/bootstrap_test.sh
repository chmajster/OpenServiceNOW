#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
fixture=$(mktemp -d /tmp/opensnow-bootstrap-unit.XXXXXX)
cleanup() { rm -rf -- "$fixture"; }
trap cleanup EXIT

# shellcheck source=../bootstrap.sh
source "$repo_dir/bootstrap.sh"

publisher="$fixture/publisher"
remote="$fixture/OpenServiceNOW.git"
mkdir -p "$publisher/docker"
git -C "$publisher" init -b main >/dev/null
git -C "$publisher" config user.name 'OpenServiceNOW installer test'
git -C "$publisher" config user.email 'installer@example.test'
cp "$repo_dir/install.sh" "$publisher/install.sh"
cp "$repo_dir/docker/local-runtime.sh" "$publisher/docker/local-runtime.sh"
printf 'initial\n' >"$publisher/release.txt"
git -C "$publisher" add .
git -C "$publisher" commit -m 'initial release' >/dev/null
git clone --bare "$publisher" "$remote" >/dev/null
git -C "$publisher" remote add origin "$remote"

INSTALL_DIR="$fixture/installation with spaces"
REPOSITORY_URL="$remote"
BRANCH=main
OPERATOR_USER=$(id -un)
OPERATOR_UID=$(id -u)
OPERATOR_GROUP=$(id -gn)

sync_repository
[[ "$(cat "$INSTALL_DIR/release.txt")" == initial ]]
[[ "$(git -C "$INSTALL_DIR" remote get-url origin)" == "$remote" ]]

printf 'updated\n' >"$publisher/release.txt"
git -C "$publisher" add release.txt
git -C "$publisher" commit -m 'updated release' >/dev/null
git -C "$publisher" push origin main >/dev/null
sync_repository
[[ "$(cat "$INSTALL_DIR/release.txt")" == updated ]]

printf 'local change\n' >>"$INSTALL_DIR/release.txt"
if (sync_repository) >"$fixture/dirty.log" 2>&1; then
  echo 'Accepted a dirty installed checkout' >&2
  exit 1
fi
grep -q 'Local source changes exist' "$fixture/dirty.log"
git -C "$INSTALL_DIR" restore release.txt

git -C "$INSTALL_DIR" config user.name 'OpenServiceNOW installer test'
git -C "$INSTALL_DIR" config user.email 'installer@example.test'
printf 'local commit\n' >"$INSTALL_DIR/local.txt"
git -C "$INSTALL_DIR" add local.txt
git -C "$INSTALL_DIR" commit -m 'local-only commit' >/dev/null
if (sync_repository) >"$fixture/diverged.log" 2>&1; then
  echo 'Accepted an installed branch with local-only commits' >&2
  exit 1
fi
grep -q 'commits not present upstream' "$fixture/diverged.log"

git -C "$INSTALL_DIR" remote set-url origin "$fixture/untrusted.git"
if (sync_repository) >"$fixture/origin.log" 2>&1; then
  echo 'Accepted an unexpected installed checkout origin' >&2
  exit 1
fi
grep -q 'expected' "$fixture/origin.log"
git -C "$INSTALL_DIR" remote set-url origin "$remote"

nonempty="$fixture/nonempty"
mkdir -p "$nonempty"
printf 'preserve\n' >"$nonempty/user-file"
INSTALL_DIR="$nonempty"
if (sync_repository) >"$fixture/nonempty.log" 2>&1; then
  echo 'Accepted a non-empty non-Git installation target' >&2
  exit 1
fi
grep -q 'not empty' "$fixture/nonempty.log"
[[ "$(cat "$nonempty/user-file")" == preserve ]]

target="$fixture/real-target"
link="$fixture/link-target"
mkdir -p "$target"
ln -s "$target" "$link"
INSTALL_DIR="$link"
if (prepare_install_directory) >"$fixture/symlink.log" 2>&1; then
  echo 'Accepted a symbolic-link installation target' >&2
  exit 1
fi
grep -q 'symbolic-link' "$fixture/symlink.log"

prepare_installer_args
[[ "${INSTALLER_ARGS[*]}" == '--local-docker' ]]
prepare_installer_args --status
[[ "${INSTALLER_ARGS[*]}" == '--local-docker --status' ]]
prepare_installer_args --local --no-start
[[ "${INSTALLER_ARGS[*]}" == '--local --no-start' ]]

printf '%s\n' 'Remote clone/update, source-integrity and installer-argument tests passed.'
