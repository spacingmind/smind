# 0050: Run failure classes with a next action

## Status

**Accepted — 2026-11-01, Supervisor default, Human may veto.** The Human was away; the Steward accepted the spec
(`docs/plans/active/run-failure-classes.md`) and decisions D1–D3 below as Supervisor defaults. A veto changes the PR named next to it
in the plan. Amends [ADR-0041](0041-in-smind-orchestration.md) B1 (the typed `failureKind`) and F1/F2b (cooldown, failover), and
points at [ADR-0028](0028-account-usage-windows.md) (usage windows and routing failover, whose bare-429 back-off versus hard
rejection split this reuses).

## Context

A failed run's `failureKind` was only `rate_limited` or `cli_exited`. A 401, a model the plan does not include, a 429 that clears in
seconds and a spent weekly window all read alike to a Lead and to a person: one sentence of provider text and no instruction. ADR-0041
B1 named `auth` and `overloaded` as "the next obvious values".

## Decision

1. **Split `rate_limited` (D1, Supervisor default).** `failureKind` takes `plan_window_exhausted` (a window, a reset time, a rejected
   `rate_limit_event`, or the provider's own "usage limit reached"), `transient_429` (a 429 / overloaded with a Retry-After or an
   overload body, never a plan problem), `auth_expired`, `model_not_in_plan` and `cli_exited`. `rate_limited` stays only as the
   **residual**: a rate limit that cannot be told apart (a bare 429 and rate words, no window, no reset, no Retry-After). It keeps
   today's behaviour (cooldown only with a known reset, profile policy as before) and is never guessed into another class. Run status
   still stays `error` (ADR-0041 D13). **Old rows migrate by rule** in one idempotent store migration: `rate_limited` with a window
   or a reset time becomes `plan_window_exhausted`, the rest stay. No installed base, so this break is stated here and in the release
   notes, with no compat shim; the web keeps its plan-limit UI working for both kinds.
2. **Precedence is fixed:** a rejected signal or explicit plan-usage text beats a typed backend code (codex `CodexErrorInfo`), which
   beats auth text, which beats model text, which beats the generic 429 words. An expired login can surface as "issue with the selected
   model" (claude-code changelog), so auth wins over model. Every pattern is sourced (refs/claude-code, refs/codex, the Anthropic
   API error types); a GLM error code is not used until a recorded payload exists.
3. **A typed `action {kind, target?, label}` built by the daemon (D2, Supervisor default).** Kinds: `sign_in`, `wait_until`,
   `switch_profile`, `pick_model`, `auto_retry`, `retry_now`, `continue`. Derived on read from the stored kind, provider, model,
   window and reset time, never stored. Additive and `omitempty` everywhere `failureKind` travels; a client renders `label` for a kind
   it does not know.
4. **Routing, cooldown and failover by class.** `plan_window_exhausted` and the residual behave as `rate_limited` did (F1 cooldown on a
   future provider-wide reset, F2b `wait` / `fallback`). `auth_expired` and `model_not_in_plan` set no cooldown, queue no wait retry
   and start no fallback successor: a person fixes setup, and a fallback would spend a turn on another account and hide the broken
   one. `transient_429` sets no cooldown.
5. **Auto-retry of `transient_429` (D3, Supervisor default).** Twice, after 30 s then 2 min (a Retry-After wins, clamped to
   [5 s, 5 min]), on the same chat, **on** for profile runs and orchestrator/agent-sourced runs, **off** for a plain human send (a human
   gets `retry_now`). **Hard cap:** a retry is never queued while its provider is in cooldown and is dropped at delivery if one began
   meanwhile; a retry is a real run (`failoverOf` chain), so its usage and the per-tree guard count it like any other run. System runs
   never retry.

## Consequences

- One helper family (`IsPlanLimit`, `IsLimitKind`, `IsHumanFix`) replaces every literal `== "rate_limited"`; a new class cannot be
  missed by a consumer.
- The inbox kind, MCP routing text, CLI text and the UI cards follow in later PRs of the plan; until then the new kinds show their raw
  text with the plan-limit UI only for plan windows.
- Not decided here: marking an account "needs sign-in" in the Usage view (follow-up), mobile UI.

## Amendment 2026-11-01: the picker twin (gate reasons)

A picker row carries the preventive twin of a class (`internal/gate`,
`docs/plans/active/picker-gate-reasons.md`): `not_signed_in` <-> `auth_expired`,
`out_of_plan` and `cooling_down` <-> `plan_window_exhausted`, `model_not_in_plan`
<-> `model_not_in_plan`; `unsupported_by_harness` has no run twin and
`transient_429`, the residual `rate_limited` and `cli_exited` never gate a row.
The gate reuses `failure.Action` (kinds `clear_cooldown` and `pick_provider` are
added to the shared vocabulary), and `gate.TwinOf` / `gate.ReasonFor` are the only
mapping, enforced by a test. Runs use the CLI's own login (ADR-0020), so a gate
resting on smind accounts' cached windows or on a recent failed run is `advisory`
(warns, stays selectable, never refuses a send); only facts the daemon enforces
(unset required provider secret, harness mismatch, cooldown) are hard.

