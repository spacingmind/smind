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

docker exec "$name" curl -fsS http://127.0.0.1:4648/healthz | grep -q '"managedBy":"docker"' && pass "healthz reports managedBy=docker" || echo "note - healthz has no managedBy (daemon build predates smind service)"

# No secret in the container's logs.
if docker logs "$name" 2>&1 | grep -qF "$PASSWORD"; then fail "the password appears in the container logs"; fi
pass "the password is not in the logs"
