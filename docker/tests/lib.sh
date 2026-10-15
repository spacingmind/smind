#!/bin/bash
# Shared helpers for the image smoke tests (docker/tests/test_image_*.sh).
# Environment: IMAGE (default smind:ci), PASSWORD (any 12+ char value; a random
# one is generated), EXPECT_VERSION (when set, `smind --version` must match it).
set -euo pipefail

IMAGE="${IMAGE:-smind:ci}"
PASSWORD="${PASSWORD:-$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | head -c 24)}"
SUFFIX="$$-$RANDOM"
CONTAINERS=()
VOLUMES=()
NETWORKS=()
TMPDIRS=()

pass() { echo "ok - $*"; }
fail() { echo "not ok - $*" >&2; exit 1; }

cleanup() {
  local c v n d
  for c in "${CONTAINERS[@]+"${CONTAINERS[@]}"}"; do docker rm -f "$c" >/dev/null 2>&1 || true; done
  for n in "${NETWORKS[@]+"${NETWORKS[@]}"}"; do docker network rm "$n" >/dev/null 2>&1 || true; done
  for v in "${VOLUMES[@]+"${VOLUMES[@]}"}"; do docker volume rm -f "$v" >/dev/null 2>&1 || true; done
  # Directories may hold files owned by uid 1000: remove them from a container.
  for d in "${TMPDIRS[@]+"${TMPDIRS[@]}"}"; do
    docker run --rm -v "$d:/x" --entrypoint sh "$IMAGE" -c 'rm -rf /x/* /x/.[!.]* 2>/dev/null || true' >/dev/null 2>&1 || true
    rmdir "$d" 2>/dev/null || true
  done
}
trap cleanup EXIT

new_name() { echo "smind-test-$1-$SUFFIX"; }
new_volume() { local v; v="$(new_name "$1")"; docker volume create "$v" >/dev/null; VOLUMES+=("$v"); echo "$v"; }
new_network() { local n; n="$(new_name "$1")"; docker network create "$n" >/dev/null; NETWORKS+=("$n"); echo "$n"; }

# start_daemon <name> [docker run args...]: a daemon with a password, detached.
start_daemon() {
  local name="$1"; shift
  CONTAINERS+=("$name")
  docker run -d --name "$name" -e "SMIND_PASSWORD=$PASSWORD" "$@" "$IMAGE" >/dev/null
}

wait_healthy() { # <name> [seconds]
  local name="$1" limit="${2:-30}" i status
  for ((i = 0; i < limit; i++)); do
    status="$(docker inspect --format '{{.State.Health.Status}}' "$name" 2>/dev/null || echo missing)"
    [ "$status" = healthy ] && return 0
    [ "$(docker inspect --format '{{.State.Running}}' "$name" 2>/dev/null || echo false)" = true ] || {
      docker logs "$name" >&2 || true
      fail "$name exited before becoming healthy"
    }
    sleep 1
  done
  docker logs "$name" >&2 || true
  fail "$name was not healthy within ${limit}s (status: $status)"
}

# The daemon-with-P4 features (login, session tokens) exist only in images whose
# smind has `config`; older builds ignore SMIND_LISTEN and listen on loopback.
image_has_direct_connections() { docker run --rm "$IMAGE" smind config get >/dev/null 2>&1; }

published_port() { docker port "$1" 4648/tcp | head -n 1 | sed 's/.*://'; }
