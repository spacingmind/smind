# 0020: smind runs through smind's own proxy

## Status

Proposed (2026-09-28). The user accepted the direction with
`docs/plans/active/orchestration-and-metering.md` on 2026-09-28:
- a per-run bearer token;
- model-aware, explicit-match-first routing;
- opt-in per run;
- Codex deferred.

The sub-decisions below await review. Depends on ADR-0019 landing first,
since it rewrites the `task.prompt` run-config fields this ADR extends.

## Context

- **Runs bypass the proxy.** Nothing in `internal/taskrunner`,
  `internal/acp`, or `internal/codex` sets a provider base URL. Claude
  Code, `glm-acp-agent`, and `codex app-server` call their providers
  directly with their own credentials, so the M1 `request_log` sees only
  clients that were pointed at `:4648` by hand.
- **Proxy auth is one daemon token.** It is
  `auth.RequireToken(s.token, ...)` on `POST /v1/messages` and
  `POST /v1/chat/completions` (`internal/server/server.go:92-93`), so a
  request cannot be tied to a workspace/task/chat/run.
- **Routing ignores the request.** Candidates are every account whose
  `provider` string matches (`internal/server/proxy.go:111-116`), always
  under `routing.PolicyPool` (`:124`). `workspace_accounts` is ignored.
  A GLM key filed as `anthropic` is pooled with real Anthropic accounts.
- **`base_url` replaces the whole endpoint URL**
  (`proxy.go:132-135`), so no other path, such as `count_tokens`, can be
  proxied.

Per-backend feasibility (verified 2026-09-28):

| Backend | Base URL / credential seam | smind-side seam |
|---|---|---|
| claude-native | `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` (sent as `Authorization: Bearer`) | `claudecode.WithEnv`, already used at `internal/taskrunner/runner.go:549` |
| GLM (`glm-acp-agent@1.3.0`) | `ACP_GLM_BASE_URL`, `Z_AI_API_KEY`; OpenAI Chat Completions wire (`dist/llm/glm-client.js:39,149`) | none: `internal/acp/rpc.go` never sets `cmd.Env` |
| codex-native | Responses API only; `wire_api="chat"` removed (`refs/codex/codex-rs/model-provider-info/src/lib.rs:57`) | the proxy has no `/v1/responses` |
| kimi | unverified | none |

## Decision

1. **Opt-in per run.** `task.prompt`/`run.start` gain an optional
   `viaProxy` bool, default false, persisted as `runs.via_proxy`. It sits
   next to ADR-0019's `permissionMode`, and a profile (ADR-0014) may carry
   it later, additively. It is rejected with a clear "not supported for
   this provider yet" error for `codex-native` and `kimi`.
2. **Per-run token.** When a `viaProxy` run starts, the daemon mints a
   random token (`smr_` + 32 random bytes, base64url). It is held only in
   memory in `internal/runs` as
   token → `{workspaceID, taskID, chatID, runID}`, and revoked in
   `Registry.finish`. It is never persisted, logged, or written to
   `run_events`. On the proxy routes, the auth middleware accepts either:
   - the daemon token, as today, with no attribution; or
   - a live run token, with attribution.

   It resolves run tokens through a small `RunTokenResolver` interface
   injected into `internal/server`, so `internal/server` does not import
   `internal/runs`.
3. **Spawn env.**
   - claude-native gets `ANTHROPIC_BASE_URL=http://127.0.0.1:<port>`,
     `ANTHROPIC_AUTH_TOKEN=<token>`, and `ANTHROPIC_API_KEY=` (blanked,
     so an inherited key can't win).
   - GLM gets `ACP_GLM_BASE_URL=http://127.0.0.1:<port>/v1` and
     `Z_AI_API_KEY=<token>`. This needs a new `acp.WithEnv` option that
     sets `cmd.Env = append(os.Environ(), extra...)` in `rpc.go`'s
     `newConn`.
   - `<port>` is the daemon's configured listen port.
4. **Attribution.** `request_log` gains nullable `workspace_id`,
   `task_id`, `chat_id`, and `run_id`, filled from the run token.
   `usage.list` gains `taskId`/`runId` filters, and `usage.summary` gains
   `groupBy: workspace|task|chat|run`.
5. **Workspace policy.** For attributed requests:
   - candidates are narrowed by `workspace_accounts` (no restriction rows
     means all accounts, matching existing semantics);
   - the workspace's `hard`/`pool` policy replaces the hardcoded
     `PolicyPool`;
   - the affinity key is `chat:<chatID>` instead of the credential hash,
     so a chat stays on one account and keeps its provider-side prompt
     cache.

   Unattributed requests behave as today.
6. **Base-URL semantics** (the cliproxyapi convention):
   - **Anthropic family:** the base is host plus optional prefix (default
     `https://api.anthropic.com`), and the proxy appends the full incoming
     path (`/v1/messages`, `/v1/messages/count_tokens`).
   - **OpenAI family:** the base includes the version segment (default
     `https://api.openai.com/v1`), and the proxy appends the incoming path
     minus its leading `/v1` (`/chat/completions`).
   - **Legacy values:** a stored value ending in `/v1/messages` or
     `/chat/completions` has that suffix stripped at read time. There is
     no data migration, and existing accounts, including the Perplexity
     one (#140/#142), keep working.
   - New route: `POST /v1/messages/count_tokens`, a pass-through that is
     not written to `request_log`.
7. **Model-aware routing.** `accounts` gains nullable `models` (a JSON
   array of `path.Match` globs), settable through ADR-0015's
   `account.add`/`account.update*` RPCs and
   `smind account add --models`. Candidate selection for a request's
   `model`:
   1. accounts of the route's provider whose globs match;
   2. otherwise, accounts with no `models` list;
   3. otherwise, a provider-shaped **400 `model_not_found`**, logged as
      `route_error`.

   A request with no `model` field only matches accounts with no `models`
   list. Overlapping globs across accounts are allowed on purpose: that is
   how several keys for one upstream form a pool.
8. **Recorded trade-off.** `viaProxy` moves a claude-native run off the
   user's own Claude login onto smind-managed `anthropic` accounts, and
   Claude Code disables some claude.ai-only features when
   `ANTHROPIC_AUTH_TOKEN` is set (its CHANGELOG, "Remote Control,
   `/schedule`, claude.ai MCP connectors ... disabled"). This is why
   `viaProxy` is opt-in, not a default.

## Alternatives considered

- **Always route runs through the proxy.** Rejected. It silently moves
  claude-native off the user's login, and Codex can't follow yet.
- **Attribution by URL path prefix** (`/r/<token>/v1/...`). Rejected.
  The secret ends up in URLs that clients and intermediaries log, and
  clients differ in how they join a base URL with a path.
- **Attribution by a custom header** (`ANTHROPIC_CUSTOM_HEADERS`).
  Rejected. It is Claude-only, and GLM's agent has no equivalent.
- **Named upstream groups, or new provider ids (e.g. `zai`), for
  compatible upstreams.** Rejected for now. Model globs fix the
  mixed-pool bug with one nullable column. Groups can be layered on
  later if per-workspace upstream assignment is asked for.
- **Persisting run tokens.** Rejected. A run does not outlive the daemon
  process that spawned it, so a persisted token would only widen the
  exposure window.

## Rationale

Metering is only meaningful if it sees smind's own runs; today it sees
none. A per-run bearer token is the one identity mechanism every backend
already supports: every client that can be pointed at a base URL also
takes an API key or auth token. The same token doubles as the missing
proxy-side authorization boundary per run. Model-aware routing and
base-URL semantics are required as soon as runs go through the proxy.
A GLM run requests `glm-5.3` on the OpenAI route, which must reach a
z.ai account, and must never reach a real OpenAI or Anthropic account.
Everything is additive on the wire: optional fields, nullable columns,
and one new route.

## Cross-references

- ADR-0014 (profiles may bundle `viaProxy` later).
- ADR-0015 (account RPCs gain `models`).
- ADR-0016 (chat-scoped affinity).
- ADR-0017 (MCP `task_send` can pass `viaProxy`).
- ADR-0019 (run-config fields this extends).
- `docs/plans/active/orchestration-and-metering.md` (M1 table, Step 2).
- `refs/cliproxyapi`: `internal/runtime/executor/claude_executor_execute.go:30`,
  `openai_compat_executor.go:358`, and `sdk/api/handlers/handlers_routing.go:203-219`.
