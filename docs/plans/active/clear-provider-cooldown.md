# Clear a provider cooldown ("I reset my quota, retry now")

Size: M (5) — scope 2, PRs 1, risk 1, decisions 0, verify 1.

After a provider rate limit the daemon holds a provider cooldown (ADR-0041 F1)
until the parsed reset time, and `--on-rate-limit=wait` retries sleep until
reset+grace. When the human resets the quota by hand (real incident
2026-10-30, GLM) nothing wakes. This adds a human-only way to clear it.

## Contract

- RPC `provider.clearCooldown {provider}` -> `{provider, cleared, woken}`.
  Human-only (run-scoped callers get `forbidden`; not on `RunScopedMethods`;
  plugins refused). Unknown provider id -> error. Idempotent.
- Clears the provider's `provider_cooldowns` row and releases held
  orchestrator rate-limit retries of that provider (`chat_queue.not_before`
  -> NULL, then `DeliverQueued`), each delivered exactly once.
- Publishes `provider.updated` so clients re-read `provider.list`.
- CLI `smind cooldown ls | clear <provider>`.
- Web: action beside the Rate limited footer and a cooldown line in
  Settings -> Providers. Mobile: follow-up if it does not fit.

## Acceptance criteria / tests

1. AC1 `TestProviderClearCooldown_RefusesAgentCaller` (wsapi): runScoped -> forbidden, cooldown row intact.
2. AC2 `TestProviderClearCooldown_ClearsBlock`: cooldown gone from provider.list; non-human send no longer fast-fails.
3. AC3 `TestProviderClearCooldown_UnknownProvider`.
4. AC4 `TestClearProviderCooldown_WakesHeldRetriesOnce` (runs): two chats held for glm + one for another provider; clear wakes exactly the glm ones; second clear wakes 0.
5. AC5 store `TestStore_ReleaseHeldRetries*`.
6. AC6 CLI `TestCooldownClear*`, `TestCooldownLs*`.
7. AC7 web vitest: action visible only while a cooldown exists; click calls `provider.clearCooldown`.
8. AC8 docs + `task docs:cli`; frames light/dark 1280/390.

## Decisions

- No new ADR: human-only RPC follows provider.setSecret; held retries are
  matched by `run_config.provider` + `source = orchestrator` + non-empty failoverOf.
- `cooldown ls` reads `provider.list`'s `cooldownUntil` (no new RPC).

## Progress

- [ ] store + runs + wsapi (peer-glm A)
- [ ] CLI (peer-glm B)
- [ ] web (peer-glm C)
- [ ] docs, frames, mutations, PR

## Validation

(filled in at the end)
