#!/bin/bash
# A root-owned named volume and a root-owned bind mount become writable for uid 1000.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

vol="$(new_volume chown)"
name="$(new_name chown)"
start_daemon "$name" -v "$vol:/data"
wait_healthy "$name" 30
[ "$(docker exec "$name" stat -c %u:%g /data)" = 1000:1000 ] || fail "the named volume was not chowned to 1000:1000"
docker exec --user smind "$name" sh -c 'touch /data/writable && test -s /data/token' || fail "uid 1000 cannot write /data"
pass "a root-owned named volume is chowned for the smind user"
docker rm -f "$name" >/dev/null

# Docker Desktop's file sharing maps ownership itself, so a chown on a bind mount
# is not observable there; the check is meaningful on a Linux engine.
if docker info --format '{{.OperatingSystem}}' | grep -qi 'docker desktop'; then
  echo "skip - bind-mount chown is not observable on Docker Desktop"
else
  dir="$(mktemp -d)"; TMPDIRS+=("$dir")
  chmod 0755 "$dir"
  name="$(new_name chownbind)"
  start_daemon "$name" -v "$dir:/data"
  wait_healthy "$name" 30
  [ "$(docker exec "$name" stat -c %u /data)" = 1000 ] || fail "the bind mount was not chowned"
  pass "a root-owned bind mount is chowned for the smind user"
  docker rm -f "$name" >/dev/null
fi

# Started non-root (--user), the entrypoint just runs: a writable mount works.
vol2="$(new_volume nonroot)"
docker run --rm -v "$vol2:/data" --entrypoint sh "$IMAGE" -c 'touch /data/.keep && chown -R 4242:4242 /data' >/dev/null # non-empty: Docker re-applies the image's ownership to an empty volume at every mount
name="$(new_name nonroot)"
start_daemon "$name" --user 4242:4242 -v "$vol2:/data"
wait_healthy "$name" 30
pass "started with --user, the daemon runs as that user without the chown step"
