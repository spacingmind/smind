# 0041: In-smind orchestration — seats for Supervisor → Lead → Peer, parent notifications, and rate-limit resilience

## Status

**Proposed** (2026-10-23, docs-only). Nothing here is built. Spec and PR split:
[`docs/plans/active/in-smind-orchestration.md`](../plans/active/in-smind-orchestration.md).
Closes the first "biggest gap" in `docs/ROADMAP.md` (Phase 2's original DoD: smind orchestrates its own work instead of Paseo).

**Relationship to ADR-0027 (Profile as role contract): this ADR amends it.** ADR-0027 stays Proposed. This ADR takes a thin, enforceable slice of it
(items 1–3 and 5 of section A), re-sequences it, and leaves 0027's handoff tools, budgets, sizing and tier-scoped learning parked behind their original
trigger. Why amend and not supersede or fold is in [Alternatives](#alternatives-considered). Section B (resilience) is new and has no 0027 counterpart.

Inputs: the dogfood logs of 2026-10-22/23 (`docs/research/local/dogfood-2026-10-22.md`, `dogfood-ui-2026-10-22.md`, local-only), ADR-0016/0017/0018/0019/
0020/0021/0025/0027/0028/0039/0040, and Paseo's parent-notification and tool-policy code (cited inline, `refs/paseo` at `8d9e7d3a`).

## Context

On 2026-10-22 the Peers of three plans ran as smind tasks: ADR-0040 PRs 2–6 and the dogfood fixes went through `smind task send`, with step-by-step briefs, `--detach` and
`task wait`. The Leads and the Supervisor still lived in Paseo. Two things stopped the Leads from moving in.

**What the dogfood showed works** (so this ADR does not redesign it): task → send → follow-up keeps the chat context; worktrees and branches survive failures and
restarts and compose with other orchestrators; `claude-native --model claude-sonnet-5-5 --mode auto` runs unattended after #502; concurrent tasks share one daemon;
`task wait`, `--detach` and the cost columns (#507) replace polling; a Sonnet step costs about $0.3–0.9 (PR 6: $0.29, $0.53, $0.89, ≈$0.6; PR 4: ≈$0.4 per send),
and GLM steps took 5–15 minutes against 1–6 for Sonnet.

**What is missing, in the order the dogfood hit it:**

1. **A rate limit stalls the whole tree and nobody is told.** At ≈15:00 +07 GLM answered `429 Usage limit reached for 5 hour. Your limit will reset at 17:35:18`.
   Three runs ended as `error`; the CLI printed only `acp: rpc error -32603: Internal error`; the cause was an `[error]` chunk in the stream (since #505 it leads the
   run's error text). `task runs` said `error`. A Lead learns about a failed child only if it is blocked in `task_wait`. Paseo pushes finish/error notifications to the
   parent agent (below), so a Lead there wakes up on its own. The recovery that worked was manual: the Supervisor decided "GLM 429 → claude-native Sonnet pinned"
   (entry 23), which needed `task chat new` because a chat is bound to its first provider (ADR-0016; entry 24).
2. **Peers are not distinguishable from Leads.** The `smind` MCP server is added by hand per workspace (ADR-0018, `smind` skill), so every agent in that workspace
   has it or none does. Paseo's `glm-peer` / `claude-s-peer` have tools disabled per provider; smind has no per-profile tool policy. Identity is self-reported
   (`fromTaskId`/`fromChatId`/`parentTaskId` are optional MCP arguments), so the daemon cannot tell who is a Lead.
3. **Brief and judgment stay with the Lead**, which is fine (dogfood entry 33: the defects that mattered were found by reading code, not by the Peers).

**Facts from the code that shape the design:**

- Agent runs **never go through smind's proxy** (ADR-0020 decision 1, the Human's own ruling: Claude Code keeps its login and its claude.ai features). So the Phase 1
  account routing and failover (`internal/routing`, ADR-0028) serves proxy traffic only. For an agent run the available "other account" is another **provider**:
  claude-native uses the CLI's own login, GLM uses `glm-acp-agent`'s own key, Codex its own login.
- A run that ends is recorded once in `Registry.finish` (`internal/runs/registry.go:1263`), which already calls `notifyObservers(RunFinished{…})`
  (`internal/runs/observer.go`) and then delivers the chat's next queued item. A daemon restart does **not** go through `finish`: `MarkRunningRunsInterrupted` rewrites
  rows to `interrupted` with no `finished_at` (`internal/store/runs.go:114`; #529 adds a `daemon_restart` stop reason). Any notification mechanism that hangs only on
  `finish` loses exactly the runs that most need telling.
- The queue (ADR-0021) is persisted per chat and keeps delivering after a restart; an `agent`-sourced item gets a `[message from task #T, chat #C]` header. Mid-turn injection
  is impossible (ADR-0021 context), so a message to a busy chat is delivered at the next turn boundary.
- The ACP error carries the cause twice (`error.data.details` and an `[error]` chunk); claude-native's Go SDK (`claude-agent-sdk-go` v0.4.0) already parses
  `rate_limit_event` (`status`, `resetsAt`, `rateLimitType`) and smind **ignores it** (`grep RateLimitEvent internal` is empty); Codex has `CodexErrorInfo::UsageLimitExceeded`
  (`refs/codex/codex-rs/…`, with `resetsAt` on the image-generation variant, not verified for turn errors).
- The tree already has: `tasks.parent_task_id`, `orchestration.maxDepth` (2: Supervisor → Lead → Peer is exactly the budget), the per-tree concurrency guard
  (`internal/runs/tree_guard.go`, default 4), `task.tree` with usage rollups, derived tiers, the web sidebar nesting, attention rollup, lineage strip and tree actions.
  **Mobile has no tree** (no `parentTaskId` anywhere in `mobile/src`). The push notifier (ADR-0040) notifies for root tasks only.

### What Paseo does (the port reference)

- **Parent link** is a label: `PARENT_AGENT_ID_LABEL = "paseo.parent-agent-id"` (`refs/paseo/packages/protocol/src/agent-labels.ts:1`).
- **Notification** is `setupFinishNotification` (`refs/paseo/packages/server/src/server/agent/agent-prompt.ts:467`): armed when the parent creates or prompts a child
  (`notifyOnFinish`, default **true** for an agent caller, false for a top-level caller: `agent/tools/paseo-tools.ts:1045-1072`, `:1868`). It watches the child's lifecycle and, on
  `finished` / `errored` / `needs permission` / `closed`, sends the parent a prompt through `sendPromptToAgent` with `source.kind = "agent-notification"` and
  `activeTurnBehavior: "steer"` (it **injects into the parent's running turn**). One armed notification per (child, caller) pair; re-arming replaces the old one; an
  archived caller is skipped; the child's last message is truncated at 4000 characters with a pointer to `get_agent_activity`
  (`FINISH_NOTIFICATION_MESSAGE_LIMIT`, `formatFinishNotificationBody`). The envelope is an XML-ish `<paseo-system … kind="agent-notification" event="errored">`
  (`agent/agent-messages/index.ts`).
- **What it does not have:** persistence (the subscription is in memory, so a daemon restart forgets every armed notification), coalescing, a cost rule, or any notion of
  "the child's subtree is done". Smind's design below keeps the good parts and adds those.
- **Tool policy** is per **provider**, not per profile: `providers.<id>.paseoTools = { enabled, disabledTools }`
  (`protocol/src/provider-config.ts:31-34`, `server/agent/paseo-tool-policy.ts`). A "Peer" is a second provider id (`glm-peer`) that extends the real one with tools off.

## Decision

Two independent parts. Part B (resilience) is shippable first and does not need Part A.

---

## Part A: seats — running Supervisor → Lead → Peer inside smind

### A1. A seat is a human-authored profile plus a small contract

A seat is an `agent_profiles` row (ADR-0014/0025: provider, permission mode, thinking, model, notes) with **three** optional contract fields from ADR-0027, added as
nullable columns (a profile without them behaves exactly as today):

| Field | Meaning | From |
|---|---|---|
| `orchestrationTools` (bool, default **true** for backward compatibility with hand-added servers) | whether the run gets the built-in `smind` MCP server | ADR-0027 §1 |
| `instructions` (text ≤ 16 KiB) + `skills` (names of bundled skills, ≤ 32 KiB total) | role text delivered on every spawn, e.g. `smind-leader` for a Lead | ADR-0027 §1–2 |
| `childPolicy`: `none` \| `leaf` \| `profiles` + `childProfileIds` | which profiles this seat may launch; `orchestrationTools=false` forces `none` | ADR-0027 §1 |

Plus, from Part B, `onRateLimit` and `fallbackProfileId` (B5). All fields are **human-authored only** (ADR-0019 #6): `profile.create/update` reject agent callers and no MCP
tool writes profiles. Seats are created by a human command, `smind profile seed roles`, idempotent, which writes this pack (a Human can edit every value):

| Seat | Provider / model | Mode | `orchestrationTools` | `childPolicy` | `onRateLimit` | Notes |
|---|---|---|---|---|---|---|
| `supervisor` | claude-native, model left to the Human | default (asks) | yes | `profiles` → `lead` | none | root task, launched by the Human only (it is in no `childPolicy`) |
| `lead` | claude-native `claude-sonnet-5-5` | `auto` | yes | `leaf` | none | instructions: `smind-leader` + pointer to `docs/WORKSPACE_PROTOCOL.md` |
| `peer-glm` | glm | autoAccept | **no** | none | `fallback` → `peer-sonnet` | default coder; detailed step-by-step briefs (dogfood memory: GLM runs low thinking) |
| `peer-sonnet` | claude-native `claude-sonnet-5-5` | `auto` | **no** | none | none | hard outcomes, and the GLM fallback |
| `reviewer` | claude-native `claude-sonnet-5-5` | `plan` | **no** | none | none | read-only |

No Opus anywhere in the pack except where the Human sets the Supervisor's model. The numbers above are the stopgap's own (Paseo profile names, `docs/WORKSPACE_PROTOCOL.md`).

The profile contract replaces Paseo's per-provider variants (`glm-peer`) with one profile concept smind already has; the cost is the daemon-side resolution in A2.

### A2. The daemon resolves the profile and builds the MCP entry (ADR-0027 R3 + R4, minus the token)

- `run.start` / `task.prompt` / `chat.queue*` accept `profileId`; the daemon stores `runs.profile_id` and applies the contract. Today `profileId` is resolved in the
  `smind mcp serve` process and copied (`cmd/smind/mcp_task_send.go`), so a run launched from the web composer never has a contract. Plain profiles keep copy semantics.
- For a run whose profile has `orchestrationTools=true` the daemon **builds the `smind` MCP entry itself** (name reserved) and hands it to the agent exactly as ADR-0018
  already does for other servers (Claude `--mcp-config`, ACP `session/new` `mcpServers`). The entry's env carries the run's identity: `SMIND_TASK_ID`, `SMIND_CHAT_ID`,
  `SMIND_RUN_ID`, and `SMIND_HOME` (so an agent of a sandbox daemon talks to that daemon and not to the default one — a real hazard with the dogfood's several daemons).
  For `orchestrationTools=false` the entry is omitted; on claude-native the daemon also passes strict MCP config so a user-level `smind` entry cannot re-add it.
  Codex cannot be enforced (process-wide MCP config, ADR-0018): it is reported "not enforced" on the profile and only A3's check applies.
- `smind mcp serve` reads those env vars and **fills in** `parentTaskId` (for `task_new`), `fromTaskId` and `fromChatId` (for `task_send`), so no agent has to remember its own
  ids. An explicit value that disagrees with the env is rejected. This also fixes the dogfood's most fragile dependency: provenance headers and (below) parent
  notifications depend on those ids being present.

This reverses parity decision D8 ("opt-in injection only") for profiles that say `orchestrationTools=true`, now that the depth and concurrency guards exist, as ADR-0027 proposed.

### A3. Admission: a Peer cannot spawn, a Lead spawns only seats

On `task.create`, `run.start`, `chat.create` whose caller presents the run identity of A2: `parentTaskId` is forced to the caller's task and `source` to `agent`; `childPolicy`
is checked against the target `profileId` (a raw `provider`, `permissionMode` or `model` override is rejected, the ADR-0019 #6 pattern applied to spawning); the target task must be in
the caller's own subtree; refusals are model-readable like `TreeBusyError`. **Honest limit** (as in ADR-0027): this is a drift guard, not a sandbox. Identity is an env var, and
a Peer with a shell can read the daemon token and speak wsapi as a human; the real boundary there stays the provider's permission mode (ADR-0019). The stronger design
(a run-scoped token bound to `{runId, profileId, taskId}` with a fail-closed method allowlist, ADR-0027 §3.2) is deferred until the trigger recurs (Q3).

### A4. Write scope

Each Peer already has its own worktree and branch, so two Peers cannot overwrite each other's files; overlap shows up at merge. v1 keeps ownership in the brief
(`docs/WORKSPACE_PROTOCOL.md`: one owner per write scope) and the Lead's accept/reject. **Not built now:** `task_new {writeScope}` with an advisory overlap warning among live
tasks of one tree (ADR-0027 R8). Trigger to build: two Peers edited the same scope in one tree (it did happen once at struct level, dogfood entry 34: `runSummaryRow`). Recommendation: defer.

### A5. Budget and cost caps per tree

Today: usage is measured per run (ADR-0020), rolled up per tree by `task.tree`, concurrency is capped at 4 per tree, depth at 2. There is **no cap on spend**. Decision: add one
opt-in cap, a **tree budget on the root task** (`tasks.budget_usd`, set by a human at root creation or from the tree header; an agent cannot set or raise it). The daemon checks it at
**admission** (`task.create`/`run.start` with `source=agent`): when the tree's known cost has reached the cap, new agent-launched runs are refused with a model-readable error and the
root shows "over budget". Running runs are not killed in v1. Costs a provider reports as nil (GLM often reports none) are never estimated (ADR-0020), so the tree header shows
"N runs reported no cost" next to the figure and the cap is documented as a bound on **reported** cost only. No default number (ADR-0027 D4): measured Sonnet steps are $0.3–0.9, so a 10-step
item is $3–9, but a default written now would be a guess. Per-run `maxTurns`/budget flags (ADR-0027 R5) are deferred.

### A6. Archive on done

No new mechanism. `task_archive` (checkpoint-commits uncommitted work, refuses while a run is active) exists, and the web already has "Archive finished sub-tasks" and "Stop all in tree"
(H11, `tree-action-dialogs.tsx`). Archive **never cascades** (task-hierarchy decision 3). The change is behavioural: the `smind-leader` skill (bundled) tells a Lead to archive each Peer task after
it has accepted the candidate and its commits are on the PR branch, and the Supervisor skill to archive finished Leads. The user's rule "archive finished agents promptly to save RAM" is thus in the
role text, not in a daemon timer. A daemon auto-archive timer is rejected: it could remove a worktree a Human is reading.

### A7. How the Human sees and steers the tree

Web already has nesting, tier labels, attention rollup, lineage strip and tree actions; this ADR adds only what the new events need:

- A **child event row** in the parent's timeline (B3's message, rendered as a compact system row, not a user bubble): `task #12 "title" — error · rate limited until 17:35 · [Open]`.
- On a task row and header, a `failure` badge with a countdown to `resetAt` for a rate-limited latest run, and, when a failover happened, "continued on Claude Sonnet · chat #N".
- Steering is unchanged and always available: the Human can send to any node, `interrupt` a Lead, stop a subtree, and answers permissions (a Lead can never answer them, ADR-0017).
- **Mobile gets a read-only tree** (nested rows, attention rollup, the failure badge) because there is none today; it is a later PR (P-M) and does not block anything. Push (ADR-0040) is root-only
  today; a rate-limited tree with no failover available **pushes the root** (one kind, `needs_continue`-style), also a later PR.

### A8. Supervisor liveness

Paseo's supervisor heartbeat (hourly poll) is replaced by B3: the Supervisor's chat is woken when a Lead's subtree goes quiet. A *stall* detector (a run alive with no events for N
minutes) is not designed here; the dogfood did not show a stall, only quota stops and a model that ended its turn early (entry 32).

---

## Part B: run resilience to provider rate limits

### B1. A typed terminal reason: `failureKind = rate_limited` with `resetAt`

- **Status does not change.** A rate-limited run ends `error` (the five statuses are untouched, same discipline as D13/`disposition`: a new status ripples through `run.status`, the
  queue's "finished" trigger, the CLI and the UI).
- **New nullable columns on `runs`:** `failure_kind TEXT NOT NULL DEFAULT ''` (`rate_limited` now; `auth` and `overloaded` are the next obvious values and are out of scope) and
  `failure_reset_at TIMESTAMP NULL`. They are **stored, not derived** (unlike `disposition`) because the reset time comes from free text with a local-time quirk (GLM prints `17:35:18` with no date or
  zone), and the text can be clipped later (`clipErrMsg`); freezing the interpretation at finish time, with the daemon's `now`, keeps `run.list` stable across restarts and parser changes.
- **Classification** happens in `Registry.finish`, from, in this order: (1) a structured signal when the backend has one — claude-native's `rate_limit_event` with `status = rejected`
  (`resetsAt` epoch, `rateLimitType` `five_hour|seven_day|…`), which smind must start recording; Codex's `UsageLimitExceeded` error info; (2) a per-provider text matcher on the agent's cause
  text and error chain — GLM's `429 … Usage limit reached for 5 hour … reset at HH:MM:SS` (reset parsed in the daemon's local zone, rolled to tomorrow when already past; a failed parse leaves
  `resetAt` NULL, never a guess); (3) otherwise not rate-limited. Unknown never classifies as rate-limited. The matchers are pure functions with the real strings from the dogfood log as fixtures.
- **Window:** `failure_window` ("5h", "weekly", "") alongside, for the message; informational.
- **Exposed** additively (old clients ignore it): `run.list`, `run.status`, `run.logs`, `task.tree` nodes (latest-run detail), MCP `task_status`, `task_wait`, `run_list`, `task_tree`
  (`failureKind`, `resetAt`, `window`), the CLI (`task runs` STATUS shows `error: rate limited, resets 17:35 (in 2h 3m)`; `task wait` prints the same line and exits **6**, a new code next to
  0 done, 1 error, 3 stopped, 4 needs-continue, 5 timeout), the web run footer and task row, and mobile's run footer. `disposition` is unchanged and is not overloaded.
- **Provider cooldown (cheap, optional):** a classified `resetAt` also writes a `provider_cooldowns(provider, reset_at)` row (capped at 6 h, expires by itself). An agent-sourced `task_send`
  to that provider before `resetAt` fails fast with `provider glm is rate-limited until 17:35; configured fallback: profile 4 (peer-sonnet)` instead of launching a process that will burn a turn on a 429.
  A human send always goes through with a warning (the parse may be wrong), and `provider_list` shows `cooldownUntil`.

### B2. Which runs and states notify (summary; details in B3)

Terminal states of a **child task's** run that the parent asked about, once its subtree is quiet.

### B3. Parent notification: the daemon enqueues into the parent's chat

Ported from Paseo's `setupFinishNotification`, with persistence, coalescing and a quiescence rule added.

**Which chat.** A run records `reply_chat_id`: the chat that sent the prompt that started it, when that prompt came from an agent (`source=agent`, `fromTaskId` = the child task's own
`parent_task_id`, chat belongs to that parent). A2 makes those ids reliable. Otherwise the run notifies nothing: a human-sent prompt in a Peer's chat (the Human working in the UI), an
`orchestrator` send (an external Claude Code session; it uses `task_wait`, ADR-0021 decision 10), a root task (**no parent: nothing is enqueued**; the Human is told by attention and push, ADR-0040).
`task_send {notifyOnFinish:false}` is the per-send opt-out (Paseo's `notifyOnFinish`), and `orchestration.notifyParent: false` the global kill switch (default true).
If the parent chat or task is archived, the notification is dropped (Paseo skips an archived caller).

**Which states.** `done`, `error`, and `stopped`/`interrupted` only when the stop reason is `daemon_restart` (the run is resumable: #529). **Not** a plain `stopped` (the stopper knows),
not `waiting_permission` (Paseo notifies; smind's Lead cannot act on one, and the Human already sees it; ADR-0017), not system/purpose runs. A `done` run with disposition `needs-continue` notifies
with that disposition. Only the **direct** parent is notified (a Peer's end reaches its Lead, not the Supervisor).

**Quiescence rule (not in Paseo).** A child run's end notifies the parent only when that child **task's subtree is quiet**: no running run, no queued item on any of its chats, and no running run
in any descendant. Without it the Supervisor would be woken at the end of every Lead *turn*: a Lead ends its turn after dispatching Peers and again after each Peer notification, while its work item is
not finished. With it, the Supervisor hears once, when the last Peer has finished, the Lead has been woken, handled it and gone idle with nothing queued. A rate-limit failover (B5) enqueues its
successor before this check runs, so a failed-over run is not "quiet" and the failed run does not notify; the successor does, tagged "after failover from glm".

**Delivery.** One `chat_queue` item, `source = child_event` (new value), `whenBusy = queue` semantics (ADR-0021: delivered at the next turn boundary, bounded at 20, survives restart). Not `interrupt`: unlike
Paseo's `steer` it never cuts a running Lead turn. A run started from a `child_event` item has `origin = child_event`, carries **no `reply_chat_id`**, and so never notifies upward by itself; the Lead's
*work item* is reported by the quiescence rule on its original run chain.

**Idempotency across restarts (outbox).** `runs.parent_notified_at` (nullable). `finish` sets it in the same transaction that enqueues the item. At startup, after reconciliation, the daemon scans
`reply_chat_id IS NOT NULL AND parent_notified_at IS NULL AND status != 'running' AND coalesce(finished_at, started_at) > now - 24h` and notifies those (covers `interrupted` runs, which never pass
through `finish`, and a crash between `UpdateRunStatus` and the enqueue). The 24 h window stops a migration from flooding a Lead with old history. A second notifier pass over the same run is a no-op.

**Coalescing and debounce (cost control).** Each delivered item starts a parent run, which costs a model turn (a Sonnet step measured $0.3–0.9; the cost of a Lead *wake* is not measured yet and is
the first thing PR N3 records). So: **at most one pending `child_event` item per target chat**: while one is still `queued`, further child events are appended to its text (`UPDATE … prompt = prompt || …`); and a
new nullable `chat_queue.not_before` holds the item for `orchestration.notifyDebounceSeconds` (default 20) after the last append, capped at 60 s. Three Peers finishing within seconds wake the Lead once.
The same `not_before` column serves B5's `wait` policy.

**Content.** Bounded and typed, one block per child: task id, title, run id, state, `failureKind`/`resetAt`, `disposition`, commits ahead of base and head SHA (O1 provenance), cost (nil shown as "not
reported"), failover successor if any, and the child's last assistant text truncated at 1 KiB (Paseo: 4000) with "use `task_logs {runId}` for the rest". Wrapped in an envelope the receiving agent can
recognise, and the child text is marked **untrusted data**: it is a prompt-injection channel from a Peer into a Lead (Paseo sends it unmarked). Example:

```
[child event] 2 children of task #8 need you
- task #12 "Seal and vector" run r-4f2: error · rate_limited (5h window), resets 2026-10-22 17:35 +07 · cost not reported · 1 commit ahead (a1b2c3d)
  fallback configured: profile 4 (peer-sonnet). No retry was started.
- task #13 "Registry" run r-4f9: done · 3 commits · $0.41
<child-response task="12" untrusted="true">…last 1 KiB…</child-response>
```

**Cost bounds, stated plainly.** Worst case is one Lead turn per debounce window per tree while children keep finishing, bounded above by the per-tree concurrency guard (4 running runs) and, if set,
the tree budget (A5). The kill switch and per-send opt-out exist; an operator who wants no extra wake-ups sets `notifyParent: false` and keeps `task_wait`.

### B4. Surfaces for the Lead who *is* blocked in `task_wait`

Unchanged semantics: `task_wait` returns on `done/error/stopped` as today, now with `failureKind`/`resetAt`, so a Lead sees "rate limited until 17:35" without parsing text. The queued
notification that arrives later is redundant for that run. v1 accepts the redundancy (it is coalesced and bounded); a later optimisation drops a `child_event` entry at delivery when the target's own
runs already read that child run (`task_wait`/`task_status`/`task_logs` for that run id in its history). That filter is optional (N5) and needs the cost numbers from N3 to justify it.

### B5. Failover: what an agent run can fail over to

Constraints: no account routing for agent runs (ADR-0020), a chat is bound to its provider (ADR-0016), a failover therefore means **a new chat in the same task, bound to another provider, with
none of the old chat's agent context**. What survives is the **worktree** (every edit and commit of the failed run is on disk and on the branch) and the **text**: the original prompt, which the daemon carries
into the new chat with a preface and a bounded tail of the failed run's transcript.

**Policy lives on the profile** (human-authored, so it satisfies ADR-0019 #6 even when the fallback profile auto-approves): `onRateLimit` = `none` (default) | `wait` | `fallback`, and
`fallbackProfileId`.

| Policy | On a classified `rate_limited` run | Context | Cost risk |
|---|---|---|---|
| `none` | nothing automatic; B3 tells the parent, the web shows "Retry on <fallback>" / "Wait" buttons when a `fallbackProfileId` exists (a suggestion, one click) | n/a | none |
| `wait` | re-enqueue the same prompt on the same chat with `not_before = resetAt + 30 s`; the queue strip shows "starts 17:35" | **kept** (same provider, same chat) | none; runs unattended later |
| `fallback` | the daemon creates a new chat in the task, starts the failed run's prompt on `fallbackProfileId`'s provider/model/mode, preface: *"The previous attempt on <provider> stopped at a rate limit. The task's worktree holds its changes; inspect `git status` and `git log`, then continue the work below."* + the original prompt + a ≤ 4 KiB transcript tail. Record `runs.failover_of`. | **lost** (explicit), worktree kept | the fallback's price; a human chose it by writing the profile |

Guards: **one hop** per original run (a failed fallback does not chain; it notifies); the fallback profile must be a different provider and human-authored; `fallback` never fires for a run the
user stopped or for a non-`rate_limited` error; the per-tree guard and tree budget still apply; the original run's `reply_chat_id` is copied to the successor, so the parent hears about the **successor** (B3
quiescence). **UI and notification on failover:** the failed run's footer reads "Rate limited (5h window) · continued on Claude Sonnet in chat #N · [Open]"; the task header shows the same badge; the
parent gets the B3 line with "failover"; the Human gets a toast and the attention dot; a root tree with `onRateLimit=none` and no fallback pushes the root (A7). Events: `run.failover {fromRunId, toRunId, toChatId}`
on the existing lifecycle topic (ADR-0009).

**Rejected defaults.** `fallback` for every profile (a Human on a Max plan may prefer to wait); automatic failover *to Opus*; same-provider "other account" failover (nothing to fail over to); routing agent
runs through the proxy so 429s fail over transparently — ADR-0020 decision 1 ruled that out for good reasons (the CLI login and claude.ai features), and this ADR does not reopen it.

### B6. Interaction summary

`failureKind` (B1) → provider cooldown (B1) → failover/wait (B5) → parent notification with quiescence (B3) → web/mobile badges and push (A7). Each step is useful alone, which is why the plan ships them
as separate small PRs, B1 first.

---

## Alternatives considered

- **Supersede ADR-0027.** Rejected: 0027's handoff tools, per-run budgets, sizing and tier-scoped learned skills are still the right target if the stopgap's drift triggers fire; this ADR only needs the part
  that makes a seat enforceable. Superseding would orphan them or force copying them here.
- **Fold ADR-0027 into this one.** Rejected: it would make the new ADR carry 0027's whole contract design (run token, allowlist, `run_handoffs`) that nothing yet demands, in the same breath as two
  independent resilience mechanisms.
- **Amend (chosen).** 0027 gets a Status note pointing here; R1 is already built (#373); R2–R4 are pulled forward in reduced form (A1–A3); R5 (limits), R6 (handoff), R8 (sizing/brief) stay parked with their trigger; R7's
  tier label already exists structurally. The plan `orchestration-roles.md` keeps its parked status, with a progress line.
- **Paseo-style provider variants** (`glm-peer` = a second provider id with tools off). Simple, and it is what the stopgap does; rejected because smind profiles already carry the same fields, a profile
  also carries role text and a child policy, and a variant provider would show up in every provider picker and chat-binding check.
- **Notes-only** (keep `notes` and `WORKSPACE_PROTOCOL.md`, add nothing). The status quo for Peers; rejected for tool policy, because "the Peer has no orchestration tools" cannot be a convention when the MCP server is
  per workspace, and the notification and failover need run identity that today does not exist.
- **Notify with Paseo's `steer`** (inject into the parent's running turn). Impossible in smind (ADR-0021: no mid-turn input injection in ACP v2 or the Claude SDK).
- **Notify on every child state change, or on every child run end.** Rejected for cost (a Supervisor woken on every Lead turn); quiescence + coalescing instead.
- **Notify an in-memory subscription like Paseo.** Rejected: restart loses it, and ADR-0021 decision 5 made "nothing queued is lost across a restart" a smind rule. The outbox (`parent_notified_at`) is the fix.
- **A new run status `rate_limited`.** Rejected, same argument as D13: five statuses are a contract for the queue's "finished" trigger, the CLI exit codes, the tree and every client.
- **Derive `failureKind` on read** (like `disposition`). Rejected for the free-text and time-zone reasons in B1.
- **Per-workspace fallback provider list** instead of per profile. Possible later as a default for sends with no profile; per profile is where the policy and its human authorship already live.
- **Failover in the Lead only** (the `smind-leader` skill already says "relaunch with another provider"). It stays the behaviour for `none`; it is not enough alone because it needs the Lead awake, and the Lead only
  wakes with B3. Automation is opt-in per profile.

## Open questions for the Human

Each has a recommendation; none is decided until you answer.

1. **ADR-0027: amend, supersede or fold?** Recommend **amend** (this ADR), as above. `orchestration-roles.md` stays parked; R6 and R8 keep their trigger.
2. **Seat model: profile contract (three new columns + daemon-built MCP entry) vs Paseo-style provider variants vs notes only?** Recommend the profile contract. It is the smallest change that gives
   a Peer no orchestration tools on every launch path (CLI, MCP, web, mobile) and gives the daemon run identity.
3. **Enforcement strength: identity by env var plus admission checks now, run token and method allowlist later?** Recommend env identity now. State plainly that it is a drift guard and not a sandbox. Build
   the token only if a Peer is seen spawning or approving, the ADR-0027 trigger list.
4. **The seeded role pack** (table in A1): Lead = claude-native Sonnet `auto`, Peers = GLM and Sonnet `auto`, reviewer = Sonnet `plan`, Supervisor model yours. Recommend yes, with `peer-glm → peer-sonnet` `fallback`.
   Confirm no Opus unless you set it, and that a `smind profile seed roles` human command (not an agent) is how they are created.
5. **Parent notification defaults:** on by default for agent-stamped sends; direct parent only; terminal states `done|error|daemon_restart-stopped`; quiescence rule; coalesced with a 20 s debounce; child text truncated
   at 1 KiB and marked untrusted. Recommend exactly this. Decide separately whether the `child_event` should *also* appear to the Human as a toast (recommend no: attention dots already exist).
6. **Notification cost:** accept "one Lead turn per debounce window while children keep finishing", bounded by the tree guard, with the kill switch `orchestration.notifyParent`? Recommend yes, and measure the first week
   (PR N3 records the cost of notification-started runs in `task.tree`). If a Lead wake costs more than a Peer step, tighten the debounce to 60 s.
7. **Failover policy:** per-profile `onRateLimit` = `none` | `wait` | `fallback`; `peer-glm` ships `fallback → peer-sonnet`; everything else `none`; one hop; context loss accepted and made explicit in the preface.
   Recommend yes. The alternative is `none` everywhere plus the Lead deciding, at the price of every 429 needing a woken Lead.
8. **Provider cooldown fast-fail** for agent-sourced sends (humans only get a warning). Recommend yes: otherwise every Peer start after a 429 burns a turn.
9. **Tree budget:** opt-in `budgetUsd` on the root, checked at admission, no default number, documented as a bound on reported cost only. Recommend yes, later (PR T-B); no number until two weeks of
   `task.tree` cost data.
10. **Write-scope warnings and sizing fields** (ADR-0027 R8): recommend keep parked; revisit when two Peers collide in one tree again.
11. **Mobile tree + push on a stuck tree:** recommend yes, as the last two PRs; they depend on nothing else.
12. **May a Lead run claude-native Sonnet when GLM is out?** Dogfood item 20 asked it as a policy question; with `fallback` on the profile the Human has answered it once, in the profile, and the Lead's classifier problem
    disappears (the fallback is launched by the daemon from a human-authored profile, not by the Lead choosing an auto-approving mode). Recommend yes.

## Consequences

- If built: nullable columns on `agent_profiles` (`orchestration_tools`, `instructions`, `skills`, `child_policy`, `child_profile_ids`, `on_rate_limit`, `fallback_profile_id`); on `runs`
  (`profile_id`, `reply_chat_id`, `failure_kind`, `failure_window`, `failure_reset_at`, `parent_notified_at`, `failover_of`); on `chat_queue` (`not_before`; `source = child_event`); a `provider_cooldowns` table;
  optionally `tasks.budget_usd`. All additive; no installed base to migrate (memory: breaking changes are fine, but none are needed).
- A daemon-built `smind` MCP entry per orchestration run, with env identity; `smind mcp serve` autofills ids.
- New wire fields are additive; new CLI exit code 6; a new queue source and run origin `child_event`; a new lifecycle event `run.failover`.
- Amends ADR-0014 (client-side apply of profile fields → daemon-side for contract fields), reverses parity D8 for `orchestrationTools=true` profiles, leaves ADR-0017 #1 (no approval tools) and ADR-0019 #6 intact, and
  leaves ADR-0020's "runs stay off the proxy" intact (agent-run failover is cross-provider by construction).
- Depends on #529 (`daemon_restart` stop reason) for the interrupted-run rule; N-PRs are written so they still work without it (they then skip interrupted runs).
- Risks, named: a wrong `resetAt` parse (mitigated: NULL on failure, cooldown capped at 6 h and advisory for humans); a notification storm (debounce, coalescing, tree guard, kill switch); prompt injection from a
  Peer's last message into a Lead (untrusted envelope; a Lead in `auto` mode still ends at the provider's own classifier, the real boundary); a failover that re-runs side effects (the preface tells the successor to inspect
  `git status`/`git log` first; one hop only).
- Not decided here: heartbeats/schedules (Phase 5), a stall detector, account-level failover, `handoff_submit`/`handoff_review`, tier-scoped learned skills.
