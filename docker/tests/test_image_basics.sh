#!/bin/bash
# Version, non-root runtime, healthcheck within 30 s, refusal without a password,
# and `smind update` refusing inside the image.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

out="$(docker run --rm "$IMAGE" smind --version)"
if [ -n "${EXPECT_VERSION:-}" ]; then
  [[ "$out" == "smind ${EXPECT_VERSION}"* ]] || fail "smind --version is '$out', expected ${EXPECT_VERSION}"
fi
pass "smind --version: $out"

docker run --rm "$IMAGE" node --version | grep -q '^v22\.' || fail "Node 22 is not in the image"
for tool in git ssh curl gosu tini; do
  docker run --rm --entrypoint sh "$IMAGE" -c "command -v $tool" >/dev/null || fail "$tool is missing from the image"
done
pass "git, ssh, curl, gosu, tini and Node 22 are present"

# --- refusal: the image listens on 0.0.0.0, so a password is mandatory -----------
for env in "" "-e SMIND_PASSWORD=" "-e SMIND_PASSWORD=short"; do
  set +e
  # shellcheck disable=SC2086
  msg="$(docker run --rm $env "$IMAGE" 2>&1)"
  code=$?
  set -e
  [ "$code" -ne 0 ] || fail "the container started without a usable password ($env)"
  grep -q 'SMIND_PASSWORD' <<<"$msg" || fail "refusal message does not name SMIND_PASSWORD: $msg"
done
pass "refuses to start on 0.0.0.0 with no, an empty or a short SMIND_PASSWORD"

# An explicit loopback listen needs no password (relay or tunnel use).
name="$(new_name loopback)"; CONTAINERS+=("$name")
docker run -d --name "$name" -e SMIND_LISTEN=127.0.0.1:4648 "$IMAGE" >/dev/null
wait_healthy "$name" 30
pass "SMIND_LISTEN=127.0.0.1:4648 starts without a password"
docker rm -f "$name" >/dev/null

# A password stored in /data/config.yaml (smind set-password) is as good as SMIND_PASSWORD,
# for images whose smind can store one.
if image_has_direct_connections; then
  new_volume stored; vol="$REPLY"
  docker run --rm -v "$vol:/data" --user 1000:1000 --entrypoint sh "$IMAGE" -c 'printf %s "a stored twelve+ password" | smind set-password --password-stdin' >/dev/null
  name="$(new_name stored)"; CONTAINERS+=("$name")
  docker run -d --name "$name" -v "$vol:/data" "$IMAGE" >/dev/null
  wait_healthy "$name" 30
  pass "a password stored in /data/config.yaml lets the image start without SMIND_PASSWORD"
  docker rm -f "$name" >/dev/null
fi

# --- healthy, uid 1000, update refusal --------------------------------------------
name="$(new_name basics)"
start_daemon "$name"
wait_healthy "$name" 30
pass "container becomes healthy within 30s"

uid="$(docker exec "$name" sh -c 'pgrep -x -u smind smind >/dev/null && id -u smind')"
[ "$uid" = 1000 ] || fail "the daemon does not run as uid 1000 (got '$uid')"
[ "$(docker exec "$name" stat -c %u /data)" = 1000 ] || fail "/data is not owned by uid 1000"
pass "the daemon runs as uid 1000 after the root entrypoint"

set +e
msg="$(docker exec --user smind "$name" smind update 2>&1)"
code=$?
set -e
[ "$code" -ne 0 ] || fail "smind update succeeded inside the image"
grep -q 'pulling a new image' <<<"$msg" || fail "update refusal does not say to pull a new image: $msg"
pass "smind update refuses: $msg"

# develop's smind has `smind service` (and with it managedBy); only an image built from
# something older may skip this, and the release job (REQUIRE_DIRECT_CONNECTIONS) may not.
if image_has_service || [ "${REQUIRE_DIRECT_CONNECTIONS:-}" = 1 ]; then
  docker exec "$name" curl -fsS http://127.0.0.1:4648/healthz | grep -q '"managedBy":"docker"' || fail "healthz does not report managedBy=docker"
  pass "healthz reports managedBy=docker"
else
  echo "skip - this image's smind has no managedBy (predates smind service)"
fi

# docker stop reaches the daemon (tini -> entrypoint -> gosu -> smind) and ends it gracefully.
started="$(date +%s)"
docker stop "$name" >/dev/null
[ $(( $(date +%s) - started )) -lt 8 ] || fail "docker stop took >= 8s: SIGTERM does not reach the daemon"
[ "$(docker inspect --format '{{.State.ExitCode}}' "$name")" = 0 ] || fail "the daemon did not exit 0 on SIGTERM"
docker logs "$name" 2>&1 | grep -q 'smind stopped' || fail "the daemon did not shut down gracefully"
pass "docker stop shuts the daemon down gracefully"

# No secret in the container's logs.
if docker logs "$name" 2>&1 | grep -qF "$PASSWORD"; then fail "the password appears in the container logs"; fi
pass "the password is not in the logs"
