#!/usr/bin/env bash
set -Eeuo pipefail
export LC_ALL=C

engine_fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }
package_installed() { [[ "$(dpkg-query -W -f='${Status}' "$1" 2>/dev/null || true)" == 'install ok installed' ]]; }
package_available() { [[ "$(apt-cache policy "$1" | awk '/Candidate:/ {print $2}')" != '(none)' && -n "$(apt-cache policy "$1" | awk '/Candidate:/ {print $2}')" ]]; }

engine_package_family() {
  if package_installed docker-ce || package_installed docker-ce-cli; then printf 'docker-ce\n'
  elif package_installed docker.io; then printf 'docker.io\n'
  elif command -v dockerd >/dev/null 2>&1 || command -v docker >/dev/null 2>&1; then
    engine_fail "Docker is not managed by docker.io/docker-ce apt packages. Update the managed engine separately and use --skip-docker-update."
  else printf 'new\n'; fi
}

configure_docker_repository() {
  local distribution=$1 codename=$2 architecture
  [[ "$codename" =~ ^[a-z][a-z0-9-]*$ ]] || engine_fail 'Cannot determine a supported distribution codename.'
  architecture=$(dpkg --print-architecture)
  [[ "$architecture" =~ ^[a-z0-9]+$ ]] || engine_fail 'Invalid system architecture.'
  install -m 0755 -d /etc/apt/keyrings
  curl --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 120 --fail --silent --show-error --location "https://download.docker.com/linux/$distribution/gpg" -o /etc/apt/keyrings/opensnow-docker.asc
  chmod a+r /etc/apt/keyrings/opensnow-docker.asc
  cat >/etc/apt/sources.list.d/opensnow-docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/$distribution
Suites: $codename
Components: stable
Architectures: $architecture
Signed-By: /etc/apt/keyrings/opensnow-docker.asc
EOF
  apt-get -o DPkg::Lock::Timeout=120 update 8>&-
}

install_docker_engine() {
  [[ "$EUID" == 0 ]] || engine_fail 'Run engine-install.sh through sudo; application installation runs as the ordinary user.'
  local username=${1:-root} distribution codename family
  id "$username" >/dev/null 2>&1 || engine_fail 'The requested Docker user does not exist.'
  [[ -f /etc/os-release ]] || engine_fail 'Cannot identify the Linux distribution.'
  # shellcheck disable=SC1091 # trusted operating-system metadata
  source /etc/os-release
  distribution=${ID:-}; codename=${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}
  case "$distribution" in ubuntu|debian) ;; *) engine_fail "Automatic Engine updates support Ubuntu/Debian; use --skip-docker-update with an existing engine on ${distribution:-this OS}." ;; esac
  command -v apt-get >/dev/null || engine_fail 'apt-get is required for Engine package management.'
  family=$(engine_package_family)
  install -m 0700 -d /run/opensnow
  exec 8>/run/opensnow/docker-engine.lock
  flock -w 120 8 || engine_fail 'Another Docker package installation is running.'
  export DEBIAN_FRONTEND=noninteractive
  apt-get -o DPkg::Lock::Timeout=120 update 8>&-
  apt-get -o DPkg::Lock::Timeout=120 install -y --no-remove ca-certificates curl iproute2 util-linux 8>&-
  if [[ "$family" == new ]]; then
    if package_available docker-compose-v2 && package_available docker.io; then family=docker.io; else family=docker-ce; fi
  fi
  local -a packages=()
  if [[ "$family" == docker.io ]]; then
    package_available docker-compose-v2 || engine_fail 'This docker.io installation has no Compose v2 apt package. Configure Compose v2 separately and use --skip-docker-update.'
    packages=(docker.io docker-compose-v2)
    if package_available docker-buildx; then packages+=(docker-buildx); fi
  else
    if ! package_available docker-ce; then configure_docker_repository "$distribution" "$codename"; fi
    packages=(docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin)
  fi
  # --no-remove preserves the installed package family and refuses destructive replacements.
  apt-get -o DPkg::Lock::Timeout=120 install -y --no-remove "${packages[@]}" 8>&-
  if [[ "$(cat /proc/1/comm)" == systemd ]]; then
    systemctl enable --now docker 8>&- || engine_fail 'Cannot start Docker.service. Inspect systemctl status docker.'
  elif command -v service >/dev/null 2>&1; then
    service docker start 8>&- || engine_fail 'Cannot start Docker. In WSL, enable systemd and reopen the distribution.'
  else engine_fail 'No supported service manager. Enable systemd in WSL or start Docker externally and use --skip-docker-update.'; fi
  if [[ "$username" != root ]]; then usermod -aG docker "$username"; fi
  local attempt
  for ((attempt=0; attempt<30; attempt++)); do
    if docker info >/dev/null 2>&1; then
      docker compose version >/dev/null || engine_fail 'Compose v2 is unavailable after package installation.'
      printf '[ OK ] Docker Engine and Compose packages are installed/up to date (%s).\n' "$family"
      return
    fi
    sleep 1
  done
  engine_fail 'Docker packages are installed but the Engine is not ready. Inspect its service logs.'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then install_docker_engine "$@"; fi
