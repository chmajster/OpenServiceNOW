#!/usr/bin/env bash
set -euo pipefail

# Real package installs in disposable privileged containers, never in the CI host.
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
name="opensnow-engine-test-${UID}-${RANDOM}"
fixture=$(mktemp -d /tmp/opensnow-engine-fixture.XXXXXX)
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf -- "$fixture"; }
trap cleanup EXIT
cat >"$fixture/Dockerfile" <<'EOF'
ARG DISTRIBUTION=ubuntu:26.04
FROM ${DISTRIBUTION}
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends systemd systemd-sysv \
    && rm -rf /var/lib/apt/lists/*
ENV container=docker
CMD ["/sbin/init"]
EOF
for distribution in ubuntu:26.04 debian:bookworm-slim; do
  image="opensnow-engine-fixture-${distribution//[:.]/-}"
  docker build --build-arg "DISTRIBUTION=$distribution" -t "$image" "$fixture"
  docker run -d --name "$name" --privileged --cgroupns private \
    --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
    --mount "type=bind,src=$repo_dir/docker/engine-install.sh,dst=/engine-install.sh,readonly" \
    "$image" >/dev/null
  docker exec "$name" bash -euc '
      # Finish boot/tmpfiles setup before APT creates temporary signature files.
      boot_state=$(timeout 60s systemctl is-system-running --wait || true)
      case "$boot_state" in
        running|degraded) ;;
        *) echo "Engine fixture did not finish booting: $boot_state" >&2; exit 1 ;;
      esac
      bash /engine-install.sh root
      docker info >/dev/null
      docker compose version
      # Updating an existing package installation must keep its data and engine.
      docker volume create opensnow-engine-preservation >/dev/null
      useradd --create-home --shell /bin/bash engine-test
      bash /engine-install.sh engine-test
      runuser -u engine-test -- docker info >/dev/null
      docker volume inspect opensnow-engine-preservation >/dev/null
      echo "Fresh Engine/Compose installation, idempotent update and volume preservation passed."
    '
  docker rm -f "$name" >/dev/null
done
