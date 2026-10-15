#!/bin/bash
# `docker exec <c> smind relay connect ...` persists the relay config in /data and
# a restarted container dials the relay again.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

net="$(new_network relay)"
relay="$(new_name relay)"; CONTAINERS+=("$relay")
docker run -d --name "$relay" --network "$net" --network-alias relay "$IMAGE" smind relay >/dev/null
for _ in $(seq 1 20); do docker logs "$relay" 2>&1 | grep -qi 'listening\|serving' && break; sleep 1; done
secret="$(docker exec --user smind "$relay" smind relay host new h1 | awk '/^  [0-9a-f]{32,}$/ {print $1}')"
[ -n "$secret" ] || fail "could not enrol a relay host"
docker restart "$relay" >/dev/null # the relay reads its host store at start
sleep 3

vol="$(new_volume relaycfg)"
d="$(new_name relayd)"
start_daemon "$d" --network "$net" -v "$vol:/data"
wait_healthy "$d" 30
docker exec --user smind "$d" smind relay connect relay:7400 h1 "$secret" >/dev/null
docker exec "$d" test -s /data/relay-connect.json || fail "the relay config was not written to /data"
if docker exec "$d" grep -qF "$secret" /data/relay-connect.json 2>/dev/null; then
  : # the daemon stores the host secret it needs to dial; it must stay owner-only
fi
[ "$(docker exec "$d" stat -c %a /data/relay-connect.json)" = 600 ] || fail "relay-connect.json is not mode 0600"

docker restart "$d" >/dev/null
wait_healthy "$d" 30
for _ in $(seq 1 30); do docker logs "$d" 2>&1 | grep -qi 'relay' && break; sleep 1; done
docker logs "$d" 2>&1 | grep -qi 'relay' || fail "the restarted daemon did not mention the relay (it should dial it again)"
if docker logs "$d" 2>&1 | grep -qF "$secret"; then fail "the relay host secret appears in the daemon log"; fi
pass "relay config persists in /data and the restarted daemon dials the relay again"
