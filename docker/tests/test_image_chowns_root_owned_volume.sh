#!/bin/bash
# A root-owned named volume and a root-owned bind mount become writable for uid 1000.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

new_volume chown; vol="$REPLY"
name="$(new_name chown)"
start_daemon "$name" -v "$vol:/data"
wait_healthy "$name" 30
[ "$(docker exec "$name" stat -c %u:%g /data)" = 1000:1000 ] || fail "the named volume was not chowned to 1000:1000"
docker exec --user smind "$name" sh -c 'touch /data/writable && test -s /data/token' || fail "uid 1000 cannot write /data"
pass "a root-owned named volume is chowned for the smind user"

# A file `docker exec` (root by default) writes into an already-correct /data is
# handed back to the smind user at the next start, instead of silently breaking it.
docker exec "$name" sh -c 'echo "# written by root" > /data/root-wrote.yaml'
docker restart "$name" >/dev/null
wait_healthy "$name" 30
[ "$(docker exec "$name" stat -c %u /data/root-wrote.yaml)" = 1000 ] || fail "a root-owned file in /data was not repaired"
pass "root-owned files left by docker exec are repaired at start"

# Files owned by anyone else are left alone, with a warning (an operator's own bind mount).
docker exec "$name" sh -c 'echo x > /data/foreign && chown 4242:4242 /data/foreign'
docker restart "$name" >/dev/null
wait_healthy "$name" 30
[ "$(docker exec "$name" stat -c %u /data/foreign)" = 4242 ] || fail "a file owned by another uid was re-owned"
pass "files owned by other users are not touched"
docker rm -f "$name" >/dev/null

# Docker Desktop's file sharing maps ownership itself, so a chown on a bind mount
# is not observable there; the check is meaningful on a Linux engine.
if docker info --format '{{.OperatingSystem}}' | grep -qi 'docker desktop'; then
  echo "skip - bind-mount chown is not observable on Docker Desktop"
else
  # (1) A bind mount root owns (made with sudo, or a fresh `docker volume`-style
  # directory) is handed to uid 1000. mktemp makes it owned by the runner, so make
  # it root-owned from inside a container.
  dir="$(mktemp -d)"; TMPDIRS+=("$dir")
  docker run --rm -v "$dir:/x" --entrypoint sh "$IMAGE" -c 'chown 0:0 /x && chmod 0755 /x' >/dev/null
  name="$(new_name chownbind)"
  start_daemon "$name" -v "$dir:/data"
  wait_healthy "$name" 30
  [ "$(docker exec "$name" stat -c %u /data)" = 1000 ] || fail "the root-owned bind mount was not chowned"
  pass "a root-owned bind mount is chowned for the smind user"
  docker rm -f "$name" >/dev/null

  # (2) A bind mount owned by someone else (the operator's own directory) is NOT
  # re-owned: the entrypoint says so, and --user <that uid> is the way to run it.
  dir2="$(mktemp -d)"; TMPDIRS+=("$dir2")
  docker run --rm -v "$dir2:/x" --entrypoint sh "$IMAGE" -c 'chown 4343:4343 /x' >/dev/null
  name="$(new_name foreignbind)"; CONTAINERS+=("$name")
  docker run -d --name "$name" -e "SMIND_PASSWORD=$PASSWORD" -v "$dir2:/data" "$IMAGE" >/dev/null
  poll 30 sh -c "[ \"\$(docker inspect --format '{{.State.Running}}' '$name')\" = false ]" || fail "a daemon that cannot write /data kept running"
  docker logs "$name" 2>&1 | grep -q 'owned by uid 4343' || fail "the entrypoint did not explain the foreign owner: $(docker logs "$name" 2>&1 | tail -3)"
  [ "$(stat -c %u "$dir2")" = 4343 ] || fail "the operator's directory was re-owned"
  docker rm -f "$name" >/dev/null
  name="$(new_name foreignbind2)"
  start_daemon "$name" --user 4343:4343 -v "$dir2:/data"
  wait_healthy "$name" 30
  pass "a bind mount owned by another uid keeps its owner; --user <uid> runs it"
fi

# The compose example's capability set (CHOWN, SETUID, SETGID, no DAC_READ_SEARCH): a root-owned
# tree with 0700 directories (a `sudo cp -r ~/.claude`) must still end up owned by uid 1000, which
# needs the children chowned before their parent.
new_volume caps; volcaps="$REPLY"
docker run --rm -v "$volcaps:/data" --entrypoint sh "$IMAGE" -c \
  'mkdir -p /data/rootdir/sub && echo x > /data/rootdir/sub/f && chmod -R 0700 /data/rootdir && chown -R 0:0 /data/rootdir' >/dev/null
name="$(new_name caps)"
start_daemon "$name" --cap-drop ALL --cap-add CHOWN --cap-add SETUID --cap-add SETGID \
  --security-opt no-new-privileges:true -v "$volcaps:/data"
wait_healthy "$name" 30
[ "$(docker exec --user smind "$name" stat -c %u /data/rootdir/sub/f)" = 1000 ] || fail "a nested 0700 root-owned tree was not handed to uid 1000 under the compose capability set"
pass "root-owned nested 0700 trees are repaired with only CHOWN/SETUID/SETGID"
docker rm -f "$name" >/dev/null

# Started non-root (--user), the entrypoint just runs: a writable mount works.
new_volume nonroot; vol2="$REPLY"
docker run --rm -v "$vol2:/data" --entrypoint sh "$IMAGE" -c 'touch /data/.keep && chown -R 4242:4242 /data' >/dev/null # non-empty: Docker re-applies the image's ownership to an empty volume at every mount
name="$(new_name nonroot)"
start_daemon "$name" --user 4242:4242 -v "$vol2:/data"
wait_healthy "$name" 30
pass "started with --user, the daemon runs as that user without the chown step"
