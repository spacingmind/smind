# 0016: Multiple chats per task

## Status

Accepted (2026-09-27). The user approved all 8 sub-decisions.
protocol, not yet decided by the user. Each sub-decision gives a
recommendation; none is final.

## Context

**Today a task is one sequential conversation.** `runs.task_id` is a plain
FK with no grouping above it (`internal/store/schema.sql:65-76`); `store.Run`'s
doc comment frames one row as "one `task.prompt`/`run.start` turn"
(`internal/store/types.go:76-84`). The web chat tab is keyed `${taskId}:task`
(`tab-registry.tsx:72-75,64`); `use-run-timeline.ts:503` filters `run.list`
by `TaskID`, and `task-detail.tsx:251-265` concatenates every run into one
`<ul>`. Mobile shows only the latest run (`TaskDetailScreen.tsx:97`). The CLI's
`smind task send` (`cmdTaskSend:290-363`) requires a `provider` argument on
every call — provider isn't even bound per task today, only chosen per turn.

**No runner resumes a session today, and no server-side guard stops two runs
on one task.** claude-native builds a fresh SDK client per call, no
`WithResume`/`WithSessionID` used anywhere (`runner.go:402-490,459`). ACP
always calls `session/new` (`internal/acp/client.go:268`); the session id
lives only in an in-memory map keyed by **task id**, `Runner.acpSessions`
(`runner.go:135-141`), overwritten by the next run and lost on restart — its
doc comment: "a task's turns are strictly one-session-at-a-time ... a second
Start on the same task spawns its own client" (`config_options.go:30-34`).
Codex always calls `thread/start` (`internal/codex/client.go:188`) and never
persists `threadID` past the call (`runner.go:608`). `runs.Registry.Start`
(`registry.go:375-433`) validates only that the task exists; the "one active
run per task" invariant (`task-detail.tsx:127-130`, `codex/client.go:98-110`)
is enforced **only** client-side, by the web composer's queue
(`composer.tsx:251-258`) — if violated for ACP, one run's teardown could
null out another run's live client via the task-keyed map (`runner.go:283`),
a latent hazard this ADR's chat-scoped state incidentally fixes (§4).

**Paseo** (`refs/paseo/packages`, read-only) already has this shape. A
workspace is one record with a single `cwd` (`workspace-registry.ts:51-100`);
N agents reference it via an optional `workspaceId` FK
(`agent-storage.ts:43-73,146-151`), all resolving to the same directory
(`create-agent/intent.ts:47-63`) — a new directory only appears as a whole
new workspace (`worktree-session.ts:264`). Agents are tabbed by `agentId`
(`workspace-tabs/model.ts:33-45`), created via "New agent", named from the
initial prompt (`create-agent-title.ts:5-35`), renamed via
`update_agent_request {agentId, name?}` (`messages.ts:930-936`). Provider is
fixed for an agent's life; model/mode/thinking-option change mid-session via
dedicated RPCs (`agent-manager.ts:1930-1944`). Session continuity is a
persisted `AgentPersistenceHandle {provider, sessionId, nativeHandle,
metadata}` on the agent's own record (`agent-types.ts:168-174`,
`agent-storage.ts:33-40,66`). Paseo enforces **no coordination** between
agents sharing a workspace (confirmed by grep, zero hits) beyond
file-explorer optimistic-concurrency unrelated to agents
(`file-explorer/service.ts:20-39`).

## Decision (overview)

Add a `chats` table between `tasks` and `runs`: the unit of conversation
identity — provider binds to it once, its agent session persists across
turns, it gets its own tab. A task keeps owning the shared worktree/branch
(`tasks.worktree_path`/`branch`, unchanged); chats are threads inside that
shared workspace, matching Paseo's workspace/agent split folded into
smind's existing task entity. Migration: every existing task's runs become
one default chat. All RPC changes are additive.

## Sub-decisions

### 1. Data model

**Recommendation: new `chats` table + `runs.chat_id`.**

```sql
CREATE TABLE IF NOT EXISTS chats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    title TEXT NOT NULL DEFAULT '',
    provider TEXT,              -- NULL until bound (see §3)
    agent_session_id TEXT,      -- provider-native session/thread id
    agent_session_meta TEXT,    -- opaque JSON (e.g. Codex nativeHandle)
    created_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);
```

`runs.chat_id INTEGER NOT NULL REFERENCES chats(id)` is added via
`internal/store/migrate.go`'s `addColumnIfMissing` (the pattern used for
`approval_policy`, `migrate.go:37-40`). Migration inserts one `chats` row per
existing task (`title=''`, `provider=NULL`) and repoints its runs — this
default chat's `provider` stays `NULL` permanently (§3), so an untouched
task keeps mixing providers turn-to-turn exactly as today.

**Alternatives rejected**: implicit grouping by `(task_id, provider)` — a
user can want two same-provider chats, and the tab/RPC surface needs a
nameable, archivable entity; chats as JSON on the `tasks` row — breaks the
FK/index pattern every other growing list uses (`accounts`, `workspaces`,
`agent_profiles` — ADR-0014) and `run.list`'s chat filter (§5) needs a real
column.

### 2. Per-chat agent session continuity

**Recommendation: persist `agent_session_id`/`agent_session_meta` on the
`chats` row**, written after a run completes and read back at the next run —
mirroring Paseo's persisted `AgentPersistenceHandle`, not smind's current
in-memory `acpSessions` map. This requires each runner to actually resume,
which none does today: claude-native must wire the vendored SDK's unused
`WithResume`/`WithSessionID` options (`client.go:515-552,916-933`); ACP
implements only `session/new` (`internal/acp/client.go:268`) and Codex only
`thread/start` (`internal/codex/client.go:188`) — both need a load/resume
call added if the upstream agent supports one, an **open verification item
not resolved here**.

Migrated default chats start with `provider=NULL` and no session id. Their
**next** run binds the provider and stores a session handle, and every
follow-up after that resumes. Existing tasks start getting continuity from
their next prompt on, because the user needs follow-ups to keep context
now.

**Resume mechanics, modeled on Paseo (user-reviewed 2026-09-27):**
- Session state is persisted as a Paseo-style handle
  `{provider, sessionId, nativeHandle, metadata}`
  (`refs/paseo/.../agent/agent-types.ts:168-174`).
- **claude-native:** keep the SDK session id. The next run passes it via
  `WithResume`, which Paseo does as `resume: sessionId` in
  `providers/claude/agent.ts:3153`.
- **Codex:** `thread/resume` with the stored thread id. Check
  `thread/loaded/list` first, and `thread/unarchive` then retry if the
  thread was archived (`providers/codex-app-server-agent.ts:3963-3980`).
- **ACP (GLM/Kimi):** capability-driven, in order:
  1. `session/load` if `agentCapabilities.loadSession`;
  2. else `unstable_resumeSession` if `sessionCapabilities.resume`;
  3. else a clear, surfaced error: the agent doesn't support resume, so
     the chat starts a new session and the UI says so. Never a silent
     fresh start.

  Paseo does this in `providers/acp-agent.ts:1777-1800`. Which
  capability GLM's ACP agent actually advertises must be verified
  against the real binary during implementation.

### 3. Per-chat run config: provider binds at first run

**Recommendation: `chats.provider` is set on first run, immutable after**,
matching Paseo (provider fixed for an agent's life; only model/mode/thinking
mutable, `agent-manager.ts:1930-1944`). `approvalPolicy`/`thinkingLevel` stay
per-run knobs, unchanged. `RunConfigToolbar`'s persistence key moves from
`smind:run-config:${taskId}` to `smind:run-config:${taskId}:${chatId}`
(`run-config-preference.ts:5-7`), and its provider selector becomes read-only
once `chats.provider` is set (read from `chat.get`, not client state, so it's
consistent across tabs). **Alternative rejected**: provider switchable per
turn — defeats §2, since a stored session id is provider-native and
switching mid-chat would silently drop continuity.

### 4. Concurrency

**Recommendation: one running run per chat, parallel across chats in the
same task/worktree, with a UI warning.** Matches the motivating example (one
chat codes while another reviews, same tree) and Paseo's model (no
cross-agent coordination). `runs.Registry.Start` (`registry.go:375-433`)
gains a real guard — reject a new run if the **chat** already has one
`status='running'` — enforced per chat, replacing today's unenforced
per-task assumption, not weakening it. `Runner.acpSessions` becomes keyed by
`chatID` (a chat has at most one in-flight run), removing the cross-run
clobber hazard from Context. The web UI shows a banner ("N chats running in
this task") whenever more than one chat has an active run — nothing stops
two chats' tool calls from editing the same file, and that risk should be
surfaced, not hidden or silently blocked.

**Alternatives rejected**: serialize to one running run per task — defeats
the motivating scenario directly (reviewer could never run concurrently);
sub-worktree/branch per chat — the point of a task is one shared
worktree/branch, per-chat branches turn "chats in a task" into "tasks in a
task" and reintroduce the merge problem one-worktree-per-task avoids. Noted
as a future v2 escape hatch for workloads wanting real isolation, not
required now.

### 5. Wire changes (additive, backward compatible)

New RPCs, following `profile.*`'s convention (ADR-0014):

| Method | Params | Result |
| --- | --- | --- |
| `chat.create` | `{taskId, title?}` | created `Chat` (`provider` null) |
| `chat.list` | `{taskId}` | `[]Chat`, ordered by `id` |
| `chat.rename` | `{chatId, title}` | updated `Chat` |
| `chat.archive` | `{chatId}` | updated `Chat` (`archivedAt` set) |

`task.prompt`/`run.start` gain an **optional** `chatId`
(`handlers.go:789-799,819-870`); omitted, it defaults to the task's default
chat, so old clients (mobile, CLI, a stale tab) keep working unchanged.
`run.list` gains an optional `chatId` filter, additive to today's no-filter
behavior (`handlers.go:872-876`).

`run.status` and `permission.pending` payloads gain `chatId` alongside
`taskId` (`events.go:71-79,81-89`), the same additive-field pattern
ADR-0009 used. New lifecycle topics, ADR-0009's shape (full snapshot on
create/update, no delete — a chat is archived, never destroyed, matching
`task.archive`):

```
chat.created   {"chat": Chat}
chat.renamed   {"chat": Chat}
chat.archived  {"chat": Chat}
```

### 6. Web

Tab key changes `${taskId}:task` → `${taskId}:chat:${chatId}`
(`tab-registry.tsx:72-75`); the migrated default chat opens as the task's
first tab automatically, and "+" gains "New chat" → `chat.create` + open tab,
mirroring Paseo's "New agent" launcher entry. Rename edits the tab label via
`chat.rename`. Closing a tab is client-only — detaches (matching
`run.attach`'s `stopOnDetach=false`, `handlers.go:892`), does not archive;
the chat keeps running and reappears if still active. Archiving is a
separate, explicit action (tab menu → "Archive chat") → `chat.archive`,
matching `task.archive`'s own separation from tab-close. `RunConfigToolbar`
and its header pill (`task-detail.tsx:28-38,191-200`) become per-chat-tab
instances (§3's key). Task-level attention/unread stays aggregated
client-side, the way `hooks/use-task-attention.ts` aggregates from
`run.list` today, extended to a two-level group-by (`TaskID` then `ChatID`,
both on every `Run` row) — no new daemon RPC needed.

### 7. Mobile and CLI

Mobile's `TaskDetailScreen` keeps showing only the task's default chat for
v1 (a chat picker is a follow-up); its follow-up path
(`followUpPrompt.ts:33-81`) sends no `chatId`, implicitly targeting the
default chat via §5's default. CLI: `smind task send` gains an optional
`--chat <id>`, defaulting to the default chat; add `smind task chat
ls|new|rename|archive <taskId>`, matching `cmdTask`'s dispatch shape
(`cmd/smind/task.go:145-175`). `task attach`/`logs`/`stop` are unaffected —
already addressed by `runId`, which unambiguously identifies a chat via
`runs.chat_id`.

### 8. Permission requests and notifications

`permission.pending` gains `chatId` (§5). Any future push/attention
notification (Phase 3 relay/mobile, ADR-0007) should carry `chatId`
alongside `taskId`, the way Paseo's attention notification carries
`workspaceId` alongside its agent fields
(`agent-attention-notification.ts:8,22,208`) — noted so that surface
doesn't retrofit chat-scoping later.

## Alternatives considered (global) and consequences

Rejected: a second task for the reviewer instead of a second chat — a task
owns exactly one `worktree_path`/`branch`, so a second task means a
*different* worktree, breaking the shared-tree requirement this ADR exists
to satisfy; flattening `tasks` and `chats` into one entity (Paseo has no
`tasks` layer) — `tasks` already owns git state unrelated to conversation
identity, a much larger migration for no benefit over one FK layer.

Consequences: purely additive on the wire, no existing RPC param or event
field changes meaning; bundles a real fix for session continuity, which
exists for no provider today (§Context) — a chat's whole reason for existing
is a stable, resumable agent identity, and this incidentally also fixes the
ACP task-keyed session-map hazard (§4); legacy default chats
(`provider=NULL`) never gain continuity retroactively, deliberately.

## Cross-references

ADR-0005 (event envelope/topics this extends), ADR-0008 (per-run event
vocabulary, unchanged — a chat's transcript is its runs' events
concatenated), ADR-0009 (the snapshot pattern `chat.*` follows), ADR-0014
(precedent for a daemon-stored CRUD entity with wsapi methods + lifecycle
events; profiles stay orthogonal). `refs/paseo/packages` (read-only) —
`server/src/server/agent/agent-storage.ts`, `agent-manager.ts`,
`protocol/src/agent-types.ts`, `protocol/src/messages.ts`,
`app/src/workspace-tabs/model.ts`, `app/src/panels/agent-panel.tsx`.
