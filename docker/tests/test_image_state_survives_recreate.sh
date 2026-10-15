#!/bin/bash
# Stopping and removing the container and starting a new one on the same /data
# volume keeps the database, the token and the config.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

vol="$(new_volume state)"
c1="$(new_name state1)"
start_daemon "$c1" -v "$vol:/data"
wait_healthy "$c1" 30
token1="$(docker exec "$c1" cat /data/token)"
[ -n "$token1" ] || fail "no token file was written"

docker exec --user smind "$c1" smind profile add survivor claude-native >/dev/null
docker exec --user smind "$c1" smind profile ls | grep -q survivor || fail "the profile was not created"
docker stop "$c1" >/dev/null
docker rm "$c1" >/dev/null

c2="$(new_name state2)"
start_daemon "$c2" -v "$vol:/data"
wait_healthy "$c2" 30
[ "$(docker exec "$c2" cat /data/token)" = "$token1" ] || fail "the token changed across a recreate"
docker exec --user smind "$c2" smind profile ls | grep -q survivor || fail "the profile did not survive the recreate"
pass "database and token survive stop/remove/recreate on the same /data volume"
