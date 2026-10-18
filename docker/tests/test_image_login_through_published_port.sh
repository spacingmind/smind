#!/bin/bash
# With a password, `docker run -p` works: login succeeds, a wrong password fails,
# the daemon token is never served, and an unlisted Host is refused (ADR-0031 D8).
# Needs a daemon with direct connections (smind config); skipped on older builds.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

if ! image_has_direct_connections; then
  # A daemon without direct connections ignores SMIND_LISTEN and SMIND_PASSWORD:
  # the container is "healthy" on loopback and `-p 4648:4648` reaches nothing. That
  # must never be published, so the release job sets REQUIRE_DIRECT_CONNECTIONS=1
  # and this becomes a failure there; PR builds may skip until the daemon has it.
  if [ "${REQUIRE_DIRECT_CONNECTIONS:-}" = 1 ]; then
    fail "this image's smind has no direct connections (smind config): published-port login cannot work, refusing to pass"
  fi
  echo "skip - this image's smind predates direct connections (P4); published-port login is not testable yet"
  exit 0
fi

name="$(new_name login)"
start_daemon "$name" -p 127.0.0.1::4648
wait_healthy "$name" 30
port="$(published_port "$name")"
base="http://127.0.0.1:$port"

curl -fsS "$base/healthz" | grep -q '"authRequired":true' || fail "healthz does not report authRequired"
code="$(curl -s -o /dev/null -w '%{http_code}' "$base/api/token")"
[ "$code" = 403 ] || fail "/api/token answered $code through the published port (want 403)"

code="$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{"password":"definitely not it"}' "$base/api/login")"
[ "$code" = 401 ] || fail "a wrong password answered $code (want 401)"

body="$(curl -fsS -H 'Content-Type: application/json' -d "{\"password\":\"$PASSWORD\"}" "$base/api/login")"
grep -q '"token"' <<<"$body" || fail "login with the right password failed: $body"
pass "login works through a published port (wrong password 401, /api/token 403)"

code="$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: evil.example.com' "$base/healthz")"
[ "$code" = 403 ] || fail "an unlisted Host answered $code (want 403)"
pass "an unlisted Host is refused"

if docker logs "$name" 2>&1 | grep -qF "$PASSWORD"; then fail "the password appears in the container logs"; fi
pass "no secret in the logs"
