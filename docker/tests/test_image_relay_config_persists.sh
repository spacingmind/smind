#!/bin/bash
# `docker exec <c> smind relay connect ...` persists the relay config in /data and
# a restarted container dials the relay again.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

new_network relay; net="$REPLY"
relay="$(new_name relay)"; CONTAINERS+=("$relay")
docker run -d --name "$relay" --network "$net" --network-alias relay "$IMAGE" smind relay >/dev/null
poll 30 sh -c "docker logs '$relay' 2>&1 | grep -qi 'listening\|serving'" || fail "the relay did not start"
secret="$(docker exec --user smind "$relay" smind relay host new h1 | awk '/^  [0-9a-f]{32,}$/ {print $1}')"
[ -n "$secret" ] || fail "could not enrol a relay host"
docker restart "$relay" >/dev/null # the relay reads its host store at start
relay_restarted_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
poll 30 sh -c "docker logs --since '$relay_restarted_at' '$relay' 2>&1 | grep -qi 'listening\|serving'" || sleep 3
# The relay serves a self-signed certificate, so `relay connect` needs its pin
# (it refuses to trust-on-first-use without confirmation, ADR-0031 P12c).
fp="$(docker logs --since "$relay_restarted_at" "$relay" 2>&1 | grep -oE 'fingerprint [0-9a-f]{64}' | tail -1 | awk '{print $2}')"
[ -n "$fp" ] || fail "could not read the relay's certificate fingerprint from its log"

new_volume relaycfg; vol="$REPLY"
d="$(new_name relayd)"
start_daemon "$d" --network "$net" -v "$vol:/data"
wait_healthy "$d" 30
docker exec --user smind "$d" smind relay connect --fingerprint "$fp" relay:7400 h1 "$secret" >/dev/null
docker exec "$d" test -s /data/relay-connect.json || fail "the relay config was not written to /data"
[ "$(docker exec "$d" stat -c %a /data/relay-connect.json)" = 600 ] || fail "relay-connect.json is not mode 0600"

restarted_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
docker restart "$d" >/dev/null
wait_healthy "$d" 30
# Only the log of the restarted daemon counts, and a failed dial is not "dialing".
poll 30 sh -c "docker logs --since '$restarted_at' '$d' 2>&1 | grep -qi 'relay'" || fail "the restarted daemon did not mention the relay (it should dial it again)"
if docker logs --since "$restarted_at" "$d" 2>&1 | grep -qiE 'relay.*(refused|failed|error|no such host)'; then
  docker logs --since "$restarted_at" "$d" >&2; fail "the restarted daemon's relay dial failed"
fi
if docker logs "$d" 2>&1 | grep -qF "$secret"; then fail "the relay host secret appears in the daemon log"; fi
pass "relay config persists in /data and the restarted daemon dials the relay again"
