# 0020: Per-run usage from agent events; runs stay off the proxy

## Status

Accepted (2026-10-11). This is a rewrite of the 2026-09-28 draft
"smind runs through smind's own proxy".

The user rejected that draft's central trade-off, its decision 8. Routing
a claude-native run through the proxy moves it off the user's own Claude
login, and Claude Code disables claude.ai-only features (Remote Control,
`/schedule`, claude.ai MCP connectors) once `ANTHROPIC_AUTH_TOKEN` is set.

The user's position (2026-10-10): "proxy chỉ để quản lý thôi — paseo có
cần đâu?" The proxy exists only to manage accounts for external clients.
Paseo meters agents without any proxy: it reads usage from the agents'
own events (`refs/paseo/packages/server/src/server/agent/agent-sdk-types.ts`,
`AgentUsage`, plus `turn_completed` / `usage_updated`).

## Context

- The M1 proxy meter (`request_log`, `usage.*`) only sees clients that
  were pointed at `:4648` by hand. smind's own runs call their providers
  directly, so per-run usage is unknown today.
- Every backend smind drives already reports usage itself, so no proxy is
  needed to learn it (verified 2026-10-10):

| Backend | Usage source | What it carries |
|---|---|---|
| claude-native | the stream-json `result` message at the end of each turn | `usage{input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens}`, `total_cost_usd`, `modelUsage` (per model) |
| codex-native | `thread/tokenUsage/updated` (`refs/codex/codex-rs/app-server-protocol/.../ThreadTokenUsageUpdatedNotification.ts`) | `tokenUsage.last` / `.total`: `inputTokens`, `cachedInputTokens`, `cacheWriteInputTokens`, `outputTokens`, `reasoningOutputTokens`, `totalTokens`; `modelContextWindow` |
| ACP (GLM, kimi) | `PromptResponse.usage`, behind the unstable `unstable_end_turn_token_usage` feature (`refs/agent-client-protocol/.../v1/agent.rs:3104`) | session-cumulative `input_tokens`, `output_tokens`, `thought_tokens?`, cache read/write, `total_tokens` |
| ACP (GLM, kimi) | the `usage_update` session notification, stabilized | context `used` / `size`, and optional cumulative `cost{amount, currency}` |

## Decision

1. **Runs never go through the proxy.** There is no `viaProxy`, no
   per-run token and no spawn-env injection. Claude Code keeps the
   user's own login and every claude.ai feature. The proxy stays an
   account-management surface for external clients only. This supersedes
   decisions 1–3 and 8 of the 2026-09-28 draft.
2. **Per-run usage comes from provider events.** The runner normalizes
   each backend's report into one `taskrunner` usage value:
   - input tokens;
   - cached input (cache read);
   - cache write;
   - output;
   - reasoning/thought;
   - cost in USD;
   - model;
   - context used / size.

   Every field is nullable. Values are **never estimated**: a backend
   that reports nothing leaves the field NULL.
   - **claude-native:** read from the turn's `result` message.
     `modelUsage` gives the model, and the cost is `total_cost_usd`.
   - **codex-native:** use `tokenUsage.last` from the last
     `thread/tokenUsage/updated` of the turn. That is per-turn, so no
     differencing is needed.
   - **ACP:** if `PromptResponse.usage` is present, it is
     session-cumulative, so the run's value is the difference from the
     snapshot stored for the same session at the end of the previous run.
     That snapshot is stored with the run, which is O1's session-handle
     persistence. Context `used`/`size` and cumulative `cost` come from
     the last `usage_update`; cost is differenced the same way. A
     backend that sends neither leaves the run's usage NULL.
3. **Storage.** A `run_usage` table holds one row per run, keyed by
   `run_id`. It carries `workspace_id`, `task_id`, `chat_id`, `provider`,
   `model`, the token fields, `cost_usd`, `context_used`,
   `context_size`, the cumulative session snapshot (ACP only), and
   `source` (`claude_result` | `codex_token_usage` | `acp_prompt_usage` |
   `acp_usage_update`).
   - The row is upserted at each turn end and when the run finishes.
   - A structured run event `usage` (ADR-0008) is also emitted, so a
     live timeline can show it without polling.
   - Nothing about prompts or secrets is stored.
4. **Reporting.**
   - `usage.summary` gains `scope: proxy|runs|all` (default `all`). For
     runs it gains `groupBy: workspace|task|chat|run|provider|model`.
     Proxy rows keep their account grouping.
   - `run.list` / `RunSummary` carry the run's token and cost totals.
   - `smind usage` gains `--scope` and the new groupings.
5. **Unchanged proxy improvements for external clients.** These are
   carried over from the draft, because they don't touch runs:
   - **(a) Base-URL semantics, the cliproxyapi convention.** For the
     Anthropic family the base is host plus prefix, and the full incoming
     path is appended. For the OpenAI family the base includes `/v1`,
     and the incoming path is appended minus its leading `/v1`. A legacy
     stored value ending in `/v1/messages` or `/chat/completions` has
     that suffix stripped at read time, with no migration. New
     pass-through route: `POST /v1/messages/count_tokens`, which is not
     metered.
   - **(b) Model-aware routing.** `accounts.models` is a JSON array of
     `path.Match` globs, settable via `account.add`/`account.update*` and
     `smind account add --models`. Candidate selection goes: accounts
     whose globs match explicitly, then accounts with no list, then a
     provider-shaped 400 `model_not_found`. A request with no `model`
     only matches accounts with no list.
6. **Out of scope.** Rotating smind's own runs across pooled accounts is
   out of scope. If several z.ai keys ever need pooling for GLM runs, that
   is a new ADR limited to API-key providers. Those have no login
   trade-off, which is why it would be a separate decision.

## Alternatives considered

- **Route runs through the proxy (the 2026-09-28 draft).** Rejected by
  the user: it costs claude-native the user's login and claude.ai
  features, and the proxy is meant for account management, not for
  smind's own agents.
- **Estimate tokens client-side, from prompt and response text.**
  Rejected: inaccurate, and it would make up numbers for backends that
  report nothing. NULL is honest.
- **Store usage only as run events.** Rejected as the only store:
  aggregating across runs needs a queryable row. The event is kept for
  live display.

## Consequences

- Per-run cost and tokens are available for Claude and Codex
  immediately. For GLM they depend on `glm-acp-agent` sending
  `usage_update` and/or `PromptResponse.usage`; where it sends neither,
  the run shows "usage not reported".
- External proxy clients keep their account-level metering and gain
  correct base-URL handling and model-aware routing.

## Cross-references

- ADR-0008 (structured run events: new `usage` kind).
- ADR-0015 (account RPCs gain `models`).
- ADR-0016 (chat-scoped runs).
- `docs/plans/active/orchestration-and-metering.md` (M1, ADR-M section).
- `refs/paseo/packages/server/src/server/agent/agent-sdk-types.ts:230` (`AgentUsage`).
- `refs/cliproxyapi`: `internal/runtime/executor/claude_executor_execute.go:30`,
  `openai_compat_executor.go:358`, `sdk/api/handlers/handlers_routing.go:203-219`.
