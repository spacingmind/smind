# Multi-agent orchestration primitives + proxy metering

Two tracks, one plan, because they meet at the same place: a **run**.

- **Track O (orchestration).** Let a smind *user* run a lead/peer
  workflow on *their own* project, including large codebases with
  vertical dependencies, by composing smind primitives. The model is the
  SLP write-up (vhLam, 2026-09-27), which composes Paseo's primitives
  without Paseo's bundled orchestration skills. smind ships primitives,
  not a methodology.
- **Track M (metering).** Per-request trace for proxied LLM traffic
  (model, account, tokens, latency, status), attributable to
  workspace/task/chat/run, plus correct routing for cheap compatible
  upstreams (GLM/z.ai, DeepSeek, MiniMax, OpenCode Go). This is pillar 1
  in `docs/research/local/synthesis-philosophy-2026-09.md`.

Where they meet: once runs go through the proxy (M2), every peer's token
usage is attributable. That is the evidence the O3 dogfood and any
"Better-SLP" review need (repeated conflicts, which escalations changed
outcomes, ceremony that only burns tokens).

## Current state (verified 2026-09-28)

**Orchestration**
- Multi-chat per task (ADR-0016) is done.
- `smind mcp serve` (ADR-0017) is on `feat/mcp-serve-readonly-tools`.
- The agent MCP servers store (ADR-0018) is on `feat/mcp-servers-store`.
- Task parent/child (`smind-control-parity.md`) is not started.
- A busy chat **rejects** a new prompt
  (`internal/runs/registry.go:470`). Queueing exists only client-side in
  the web composer.
- The session handle is persisted only when a turn succeeds
  (`internal/taskrunner/runner.go:339,582,756`). Stopping a chat's first
  turn therefore loses its session.

**Metering**
- `internal/server/proxy.go` does a plain `io.Copy`
  (`copyResponse`, `:274-296`). `usage` is never read, and no table
  stores per-request data. `routing_decisions` is per session key.
- smind's own runs bypass the proxy: nothing sets a provider base URL on
  a spawned agent. Caller identity at the proxy is only a hash of the
  caller's credential (`sessionKey`, `:231-241`).
- API-key accounts already accept an upstream override
  (`accounts.AddAPIKeyWithBaseURL`, `internal/accounts/registry.go:81`).
  The gaps:
  - `base_url` replaces the **whole endpoint URL** (`proxy.go:132-135`),
    and only two routes exist.
  - The candidate pool is every account with the same `provider` string
    (`:111-116`), with `PolicyPool` hardcoded (`:124`). A GLM key filed
    as `anthropic` is pooled with real Anthropic accounts.
  - `workspace_accounts` is ignored by the proxy.

## Research findings that shaped the proposals

| Question | Evidence | Consequence |
|---|---|---|
| Can each backend be pointed at the proxy with a per-run credential? | **Claude Code:** `claudecode.WithEnv` is already used (`runner.go:549`); `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` (Bearer). **glm-acp-agent@1.3.0:** `ACP_GLM_BASE_URL` + `Z_AI_API_KEY`, speaks OpenAI Chat Completions (`dist/llm/glm-client.js:39,149`), but `internal/acp/rpc.go` never sets `cmd.Env`. **Codex:** Responses API only; `wire_api="chat"` was removed (`refs/codex/codex-rs/model-provider-info/src/lib.rs:57`); smind has no `/v1/responses`. | Claude and GLM are feasible. GLM needs an env seam in `internal/acp`. Codex is deferred, consistent with ADR-0018's Codex deferral. |
| OpenAI stream usage | cliproxyapi forces `stream_options.include_usage=true` (`internal/runtime/executor/openai_compat_executor.go:355`) | Inject it. |
| Attribution | cliproxyapi attributes each usage record to the caller's client key (`sdk/cliproxy/usage/manager.go:28`) | A per-run token is the same pattern. |
| Compatible-upstream routing | cliproxyapi routes by the **requested model**: credentials register model names/aliases, and an unknown model returns 400 `model_not_found` (`sdk/api/handlers/handlers_routing.go:203-219`). There is no alias-collision check, so cross-vendor mixing is still possible by misconfiguration. | Use model-aware candidate filtering, explicit-match-first. |
| Base URL | cliproxyapi uses a base URL plus an appended path (`claude_executor_execute.go:30`, `openai_compat_executor.go:358`) and exposes `count_tokens`, `/v1/models`, `/v1/responses` | Switch to base semantics, stay compatible with legacy full URLs, and add `count_tokens` (Claude Code calls it against the base URL). |
| Log write path | cliproxyapi uses a bounded in-memory queue feeding sinks (`sdk/cliproxy/usage/manager.go`, `NewManager(512)`). Body logging is opt-in (`request-log`). | Use an async bounded writer into SQLite. Never log bodies. |
| Recursion guard | **Codex:** `agents.max_depth` default 1, message "Agent depth limit reached. Solve the task yourself." (`core/src/tools/handlers/multi_agents/spawn.rs:72`); max 4 concurrent sub-agent threads (`core/src/config/mod.rs:229,238`). **Claude Code:** nesting off by default, 20 concurrent. **Paseo:** none. | Add a daemon-enforced depth cap plus a per-tree concurrency cap, with a model-readable error. |
| Message to a busy agent | **Paseo:** replaces (cancels) the running turn. **Codex v2:** mailbox, `QueueOnly` vs `TriggerTurn`, pending queue in memory. **dsh:** durable FIFO inbox, interrupt keeps the inbox. **Claude Code:** file-backed inbox. | A persisted per-chat queue with explicit delivery modes. This one primitive covers both escalation and human redirect, so no separate "inbox". |
| Shared plan / blackboard | None of the four refs have one. dsh **deliberately** rejects shared todo state ("Single owner", `packages/todo/tool-todo/README.md`). Codex has only a per-thread `ThreadGoal`. | Don't build a structured shared plan. At most a free-text brief, demand-gated. |
| Write-scope ownership | Paseo/Codex: isolation (separate worktrees), no locks. Claude Code: sandbox allowlist scoped to the worktree. | Drop "declared write scope". Parallel writers get child tasks (own worktree); chats sharing a worktree keep ADR-0016's warning. |

## Sequencing

```
Step 0  land in-flight: mcp-server, agent-mcp-servers, task hierarchy
Step 1  (parallel, no ADR)  M1 request log | O1 run provenance + stop/resume | O2 MCP hierarchy + guards
Step 2  ADRs: ADR-M "smind runs through the proxy"  |  ADR-O "per-chat prompt queue"
Step 3  implement both ADRs → dogfood: lead + ≥2 peers on a non-smind project, via smind only
Step 4  demand-gated backlog, driven by the dogfood gap log
```

**Dependency on ADR-0019** (provider-native permission modes, branch
`refactor/provider-native-permission-modes`, in progress 2026-09-28). It
replaces `ApprovalPolicy` with `permissionMode` across `internal/taskrunner`,
`internal/runs`, `internal/acp`, `internal/codex`, `task.prompt`, and the
web composer. It also constrains MCP `task_send` (its resolved decision 6).
Anything touching those files waits for it to land on `develop`:

- **Wave 1 (now, no overlap with ADR-0019):**
  - M1;
  - task hierarchy, plus O2's MCP parent params and depth guard;
  - ADR-0017 steps 4, 5, and 7 (`task_wait`, `task_stop`, stdio e2e);
  - ADR-0018 step 2 (`mcp.*` wsapi + CLI);
  - ADR-M / ADR-O drafts (docs only).
- **Wave 2 (after ADR-0019 merges):**
  - O1;
  - ADR-0017 step 3 (`task_send`);
  - ADR-0018 steps 3–5, 7, 8;
  - O2's per-tree concurrency guard (touches `internal/runs`);
  - the O2 guide (needs ADR-0018 runner wiring);
  - ADR-M / ADR-O implementation once accepted.

## Acceptance Criteria

### Step 0 — tracked in existing plans
`docs/plans/active/mcp-server.md`, `docs/plans/active/agent-mcp-servers.md`,
and the task-hierarchy items of `docs/plans/active/smind-control-parity.md`.

### M1 — request log for proxied traffic
- `request_log` table, one row per proxied request, with these columns:
  - `id`, `started_at`, `provider`, `account_id` (NULL on route failure),
    `session_key`;
  - `model` and `stream`, from the request body;
  - `status`, `upstream_status`;
  - `outcome`: `ok` | `upstream_error` | `route_error` | `aborted` |
    `client_cancelled`;
  - `error` (short message, never a body);
  - `ttfb_ms`, `duration_ms`;
  - `input_tokens`, `output_tokens`, `cache_read_tokens`,
    `cache_write_tokens`, `reasoning_tokens`. NULL means unknown, which is
    different from 0.
  - Indexed on `started_at` and `account_id`.
- Usage extraction:
  - **Anthropic, non-stream:** `usage.*`, all four fields.
  - **Anthropic, stream:** input and cache tokens from
    `message_start.message.usage`; output from the last `message_delta`
    (cumulative). If a compatible upstream reports input or cache in
    `message_delta`, the last non-zero value per field wins.
  - **OpenAI, non-stream:** `prompt_tokens`, `completion_tokens`,
    `prompt_tokens_details.cached_tokens`,
    `completion_tokens_details.reasoning_tokens`.
  - **OpenAI, stream:** the final usage chunk. The proxy injects
    `stream_options.include_usage=true` when it is absent.
- The client receives byte-identical upstream bytes. Parsing tees the
  stream and never buffers it. Non-stream parsing is bounded; over the
  cap, tokens are NULL.
- Rows go through a bounded async writer. A full queue or a failed write
  never delays or fails the response; it bumps a counter and calls
  `log.Printf`.
- Never stored: prompt or response content, credential values, or any
  cost/price figure.
- `usage.list {since?, until?, accountId?, limit?}` and
  `usage.summary {since?, until?, groupBy: account|model|day}` over wsapi,
  plus `smind usage [--since] [--by account|model|day]`.

### O1 — run provenance + stop/resume
- `runs` gains nullable `base_sha`, `head_sha`, and `dirty` (worktree
  HEAD at start and at the terminal state). They are exposed on
  `run.list`, `task_status`, and the web run header. If git fails, they
  stay NULL and the run is never failed.
- Stopping a run persists the chat's session handle whenever the provider
  has already issued one, including on the first turn, so the next prompt
  resumes. If there is nothing to resume, the run says so explicitly
  (ADR-0016 §2 "never a silent fresh start").

### O2 — MCP hierarchy + guards
- `task_new` accepts `parentTaskId`; `task_list` accepts a
  `parentTaskId` filter.
- Daemon-enforced guards on `task.create` with a parent and on
  `task.prompt` into a child task, configured in `config.yaml`:
  - `orchestration.maxDepth`, default 2 (a root task's grandchildren are
    the deepest level);
  - `orchestration.maxConcurrentRunsPerTree`, default 4.
  - Exceeding either returns a model-readable error, e.g. "Task depth
    limit reached (2). Do this work yourself or ask the user."
- A guide in `docs/guides/` for registering `smind mcp serve` as an agent
  MCP server (ADR-0018), so a peer can read sibling tasks. Peers still
  get no approve/deny tool (ADR-0017 decision 1).

### ADR-M — smind runs through the proxy ([ADR-0020](../../decisions/0020-runs-through-proxy.md), Proposed)
- `task.prompt` gains an opt-in per-run `viaProxy` knob, default off,
  stored on `runs`. When on, the daemon mints a per-run bearer token
  (revoked when the run ends) and spawns:
  - claude-native with `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN`;
  - GLM with `ACP_GLM_BASE_URL` and `Z_AI_API_KEY`. This needs a
    `WithEnv` option in `internal/acp` that sets `cmd.Env` in `rpc.go`.
  - Codex rejects `viaProxy` with a clear "not supported yet" error
    (Responses API only).
- The proxy accepts the daemon token or a live run token. `request_log`
  gains nullable `workspace_id`, `task_id`, `chat_id`, and `run_id`,
  filled from the run token. `usage.summary` gains
  `groupBy: workspace|task|run`.
- With a workspace known, the proxy honors `workspace_accounts`.
- **Base-URL semantics.** `base_url` becomes a true base URL and the
  incoming path is appended. A stored value that ends in the legacy full
  endpoint (`/v1/messages` or `/chat/completions`) has that suffix
  stripped, so existing accounts keep working.
  `POST /v1/messages/count_tokens` is added as a pass-through (not
  metered).
- **Model-aware routing.** An account can declare `models` globs.
  Candidates are accounts of the provider whose globs match the requested
  model. If none match explicitly, fall back to accounts with no `models`
  list. If there is still no candidate, return a provider-shaped 400
  `model_not_found`. A GLM key declared `glm-*` is never picked for
  `claude-*`.
- Known trade-off, recorded in the ADR: `viaProxy` switches claude-native
  from the user's own login to smind-managed accounts. Claude Code also
  disables some claude.ai-only features when `ANTHROPIC_AUTH_TOKEN` is
  set.

### ADR-O — per-chat prompt queue ([ADR-0021](../../decisions/0021-per-chat-prompt-queue.md), Proposed)
- `task.prompt`/`run.start` to a chat with a running run takes
  `whenBusy`:
  - `reject` keeps today's behavior and stays the default for old
    clients;
  - `queue` delivers FIFO at the next turn boundary and auto-starts the
    next run;
  - `interrupt` stops the running run and starts the queued prompt on the
    resumed session (depends on O1).
- The queue is persisted in SQLite and survives a daemon restart.
  Paseo's restart data loss is the counter-example.
- MCP `task_send` defaults to `queue`. A lead that is a smind chat gets
  peer escalations as its next turn. An external lead sees peer results
  through `task_wait` on the peer's run; no `task_wait` change
  (ADR-0021 §10).
- After a daemon restart the queue is paused until a human resumes it,
  and each chat holds at most 20 queued items (ADR-0021 §5, §7).
- The web composer's client-side queue moves to the server queue.
  Queued items are visible, and cancellable, in every client.

## Test Scenarios

**M1** (httptest fake upstreams, through the real proxy handler):
- Anthropic non-stream: all four token fields recorded, `outcome=ok`.
- Anthropic stream: `message_start` (input 100, cache_read 40), then two
  `message_delta` events (output 5, then 12), records 100/40/12.
- Compatible stream with input tokens only in `message_delta`: input is
  recorded.
- OpenAI non-stream: prompt, completion, cached, and reasoning tokens
  recorded.
- OpenAI stream with the client's own `include_usage`: request body
  forwarded unchanged.
- OpenAI stream without it: the upstream receives it injected, and the
  client still gets every content chunk in order.
- Byte-for-byte: client body equals upstream body, stream and non-stream.
- Upstream 429: `upstream_error`, and the client gets the upstream body.
- No accounts: 503, `route_error`, `account_id` NULL.
- Mid-stream upstream break: `aborted` with partial tokens, and
  `panic(http.ErrAbortHandler)` is unchanged.
- Client disconnect: `client_cancelled`.
- Over the parse cap: tokens NULL, response intact.
- Writer queue full / closed DB: the response is unaffected and the
  counter increments.
- The raw row contains neither the API key nor any prompt text.
- `usage.summary groupBy=account` sums match the inserted rows;
  `since` is inclusive and `until` is exclusive.

**O1:**
- Migration: old rows read back NULL.
- A commit during the run gives `head_sha != base_sha`; an uncommitted
  edit sets `dirty`; a non-git dir gives NULL with no error event.
- Fake ACP: stop the first run after `session/new`; the next run resumes
  that session id.
- Fake Claude transport: stop after the SDK reported a session id; the
  next run passes `WithResume`.
- Stop before any session id exists: fresh start plus an explicit notice.
- Live smoke (GLM and claude-native): stop mid-turn, send a correction,
  and the transcript shows the earlier context.

**O2:**
- A child at depth 2 succeeds; depth 3 returns the depth error through
  MCP as a tool error.
- A fifth concurrent run in one tree is rejected; after one run ends it
  succeeds.
- A cross-workspace or nonexistent parent returns the daemon error text.

**ADR-M / ADR-O:** written with each ADR's plan section.

## Decisions

Resolved:
- **No cost/price computation** (user, 2026-09-28). Tokens only, in every
  step.

Proposed (research-backed; confirm or override):
1. Pull a slice of ROADMAP Phase 5 "Subagents" forward. Steps 0–1 are
   cheap and mostly finish in-flight work; Steps 2+ are gated.
2. Attribution uses a **per-run bearer token** (cliproxyapi's
   per-client-key pattern). It works with any client that only lets you
   set a base URL and key, and it can be revoked when the run ends.
3. Compatible upstreams use **model-aware routing, explicit-match-first**
   (cliproxyapi's model registry, plus a guard it lacks). No new provider
   ids.
4. **Inject `include_usage`** (cliproxyapi precedent).
5. **`viaProxy` is opt-in per run.** Codex is deferred until a
   `/v1/responses` route exists; revisit after the Codex quota reset
   (2026-10-07).
6. **One queue primitive** with `reject|queue|interrupt` replaces the
   separate "inbox" and "steering" ideas (Codex/dsh precedent;
   persisted, unlike Codex).
7. **Guards:** depth 2 and 4 concurrent runs per tree, in `config.yaml`
   (Codex defaults 1/4; Claude Code 1/20).
8. **Dropped:** a structured shared plan/brief (no ref has one; dsh
   rejects it on purpose) and declared write scope (isolation via child
   worktrees instead). A free-text `tasks.brief` stays in the Step 4
   backlog, built only if the dogfood shows the need.

## Non-goals

Cost/price; storing request or response bodies; role personas or
built-in Supervisor/Lead/Peer types; mandatory "challenge everything"
prompting; a workflow DSL (`docs/research/local/zcode-2026-09.md:210-214`);
orchestrator approve/deny tools (ADR-0017); mid-turn input injection
(no ACP v2 verb, no SDK primitive); file locks or declared write scopes.

## Step 4 backlog (demand-gated)

Free-text task brief visible to child tasks; per-task token budget (on
top of `request_log`, like Codex's `ThreadGoal.token_budget`);
composer usage pill; feeding the router's `TokensUsed` from locally
counted tokens instead of the stubbed `quota.Fetcher`; per-workspace audit
log; `/v1/responses` + Codex `viaProxy`; `/v1/models`; a bundled
orchestration skill (`paseo-skills-profiles-2026-09.md` §e #4);
`request_log` retention; push notifications for queued escalations.

## Progress

- [ ] Step 0: mcp-server, agent-mcp-servers, task hierarchy landed
- [x] M1: schema + async writer
- [x] M1: usage extraction (4 cases) + tee'd passthrough + include_usage
- [x] M1: wsapi `usage.*` + `smind usage`
- [ ] O1: run provenance
- [ ] O1: stop/resume keeps the session
- [ ] O2: MCP parent params + guards + guide
- [x] ADR-M drafted as ADR-0020 (Proposed)
- [ ] ADR-0020 accepted
- [x] ADR-O drafted as ADR-0021 (Proposed)
- [ ] ADR-0021 accepted
- [ ] ADR-M implemented
- [ ] ADR-O implemented
- [ ] Step 3 dogfood + gap log

## Validation

**M1** (`task test` + `task lint` green, 2026-09-28):

- `request_log` schema (nullable model/stream/token columns, account FK,
  `idx_request_log_started_at`/`idx_request_log_account_id`):
  `TestStore_CreateAndListRequestLog`, `TestStore_RequestLogNullableFields`
  (`internal/store/request_log_test.go`).
- Bounded async writer, full-queue/closed-DB never affects the response:
  `TestRequestLogWriter_QueueFullDropsAndCounts`,
  `TestRequestLogWriter_WriteFailureCounts`,
  `TestRequestLogWriter_CloseDrainsGoroutine`
  (`internal/server/requestlog_test.go`), plus the HTTP-layer
  `TestProxy_RequestLog_WriterFailureLeavesResponseUnaffected`.
- Usage extraction, all four cases + tee'd byte-identical passthrough:
  - Anthropic non-stream — `TestProxy_RequestLog_AnthropicNonStream`
    (also asserts client body == upstream body byte-for-byte).
  - Anthropic stream (message_start 100/40, deltas 5→12) —
    `TestProxy_RequestLog_AnthropicStream` (stream passthrough asserted
    byte-identical).
  - Compatible upstream, input only in `message_delta` —
    `TestProxy_RequestLog_AnthropicStream_CompatibleUpstreamInputInDelta`.
  - OpenAI non-stream (prompt/completion/cached/reasoning) —
    `TestProxy_RequestLog_OpenAINonStream`.
  - OpenAI stream, client's own `include_usage` forwarded unchanged —
    `TestProxy_RequestLog_OpenAIStream_ClientIncludeUsageUnchanged`.
  - OpenAI stream without it: injected upstream, client gets every chunk
    in order — `TestProxy_RequestLog_OpenAIStream_InjectsIncludeUsage`.
  (all `internal/server/proxy_requestlog_test.go`)
- Outcomes: upstream 429 → `upstream_error` + verbatim upstream body
  (`TestProxy_RequestLog_UpstreamErrorStatus`); no accounts → 503
  `route_error`, `account_id` NULL
  (`TestProxy_RequestLog_NoAccountsIsRouteError`); mid-stream break →
  `aborted` with partial tokens
  (`TestProxy_RequestLog_MidStreamAbortRecordsPartialTokens`, abort
  semantics already covered by
  `TestProxy_UpstreamStreamBreakAbortsDownstream`); client disconnect →
  `client_cancelled`
  (`TestProxy_RequestLog_ClientDisconnectRecordsClientCancelled`).
- Over the parse cap → tokens NULL, response intact:
  `TestProxy_RequestLog_OverResponseParseCap`.
- No API key / credential / prompt text in the raw row:
  `TestProxy_RequestLog_NoSensitiveData`.
- `usage.summary groupBy=account` sums, since-inclusive/until-exclusive:
  `TestStore_SummarizeRequestLogs_GroupByAccount` (store),
  `TestUsage_ListAndSummaryRoundTrip` (wsapi RPC layer), plus
  `TestUsage_SummaryRequiresGroupBy` and CLI coverage
  `TestRunUsagePrintsSummaryRows`/`TestRunUsageEmpty`/`TestRunUsageRejectsBadGroupBy`
  (`cmd/smind/usage_test.go`).

Not started: O1, O2, ADR-M, ADR-O.
