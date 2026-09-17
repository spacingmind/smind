# Per-account base_url for upstream routing

## Goal

Allow an account to override the upstream provider URL so smind's
`/v1/messages` proxy can forward to a local Anthropic-compatible endpoint
(e.g. `pplx serve` — perplexity-proxy-go), not just `api.anthropic.com`.
This unlocks using a Perplexity Pro subscription as a provider through
smind (dogfood path: agent → smind :4648 → pplx serve :8080 → Perplexity).

## Design

- `APIKeyCredential` gains optional `base_url` (empty = provider default).
- `proxy.serve` resolves the upstream URL per account: if the routed
  account's credential carries a base_url, forward there; otherwise the
  built-in provider URL (unchanged behavior).
- Registry: `AddAPIKey` accepts an optional baseURL (variadic or a new
  method — prefer a new `AddAPIKeyWithBaseURL` to keep the existing
  signature stable for callers/tests).
- Security: only https or http+loopback base_urls are accepted (reject
  arbitrary http LAN URLs — this is a local dev tool but let's not make
  SSRF trivial).

## Acceptance criteria

1. `AddAPIKeyWithBaseURL(provider, label, key, baseURL)` persists and
   round-trips the URL through `Registry.Get`/`Account`.
2. Proxy forwards `/v1/messages` to the account's base_url when set, and to
   the provider default when not (httptest servers assert both).
3. Non-loopback `http://` base_url is rejected with a clear error; `https`
   and `http://127.0.0.1[:port]`/`localhost` are accepted.
4. Existing tests unchanged except where the new field flows through.
5. `task test` and `task lint` green.

## Test scenarios

- TestRegistry_AddAPIKeyWithBaseURL — persist + read back.
- TestProxy_ForwardsToAccountBaseURL — httptest upstream asserts request
  arrives; response passes through.
- TestProxy_DefaultURLWhenNoBaseURL — existing behavior preserved.
- TestProxy_RejectsNonLoopbackHTTP — error on http://example.com.

## Out of scope

- OpenAI provider parity (same mechanism, add later when needed).
- UI/config surface for base_url (CLI/store API only for now).

## Progress

- [x] Registry + credential field
- [x] Proxy per-account URL resolution
- [x] Tests

## Validation

All 5 acceptance criteria met (commit fee8d9d):
1. AddAPIKeyWithBaseURL round-trips base_url through Registry.Get.
2. Proxy forwards /v1/messages to the account base_url (httptest asserted);
   default provider URL preserved when unset.
3. http:// non-loopback rejected; https + loopback http accepted.
4. Existing tests unchanged beyond the new-field flow-through.
5. task test + task lint green (one flaky UI test on first run, clean on rerun).

Dogfood verified end-to-end: smind :4664 → pplx serve :8080 (perplexity-proxy-go,
Anthropic-compat) → Perplexity Pro — request "what is 2+2" returned "4".
