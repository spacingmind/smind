# 0008: Structured run events

## Status

Accepted

## Decision

`taskrunner.Event` grows from four plain-text types
(`EventTypeText`/`EventTypeDone`/`EventTypePermissionRequest`/
`EventTypePermissionResolved`, `internal/taskrunner/event.go:7-29`) to a
typed event model rich enough for the web UI to render tool-call cards,
assistant text, reasoning, and user-turn text as distinct timeline items,
per `docs/plans/active/ui-redesign-parity.md` Item 7 (gap A/D in
`gap-matrix.md`: *"4 wire events, `text` only"* vs. Paseo's 7-kind
`user_message | assistant_message | thought | tool_call | todo_list |
activity_log | compaction` union and deepseek-harness's open registry of
chat nodes keyed by wire tool name — see `audit-paseo.md` §2 "Streaming
timeline"/"Tool-call rendering" and `audit-deepseek-harness.md` §2 same
sections).

Three new `EventType` values are added, appended after the existing four
(append-only — see Compatibility below):

- `EventTypeUserMessage` — a user-turn text chunk. Populated by the ACP
  path (GLM/Kimi's `user_message_chunk`); Claude Code native does not emit
  it, since the CLI's `UserMessage` echoes back the client's own prompt
  (nothing new to the UI, which already rendered what it sent).
- `EventTypeThinking` — a reasoning/thinking text chunk. Populated by
  Claude Code native (`ThinkingBlock`) and ACP (`agent_thought_chunk`).
- `EventTypeToolCall` — one tool call's identity, arguments, lifecycle
  status, and result, correlated by `ToolCallID` across multiple events
  (a "running" event when the call starts, a later "success"/"failure"
  event with the same ID when it completes) so a UI can update one card in
  place rather than creating a new one per status change.

`Event` gains fields for the tool-call case: `ToolCallID`, `ToolName` (the
wire tool name — Claude/Codex's explicit tool name, e.g. `Bash`/`Read`; for
ACP, which has no separate name field, the `ToolKind` string doubles as
the name, e.g. `execute`/`read`), `ToolTitle` (optional human-readable
summary — ACP's `title`; empty for Claude), `ToolStatus`
(`running`/`success`/`failure`), `ToolInput` and `ToolResult` (each a raw
`json.RawMessage` in the producing provider's own shape — deliberately not
normalized further, same reasoning `Event.Raw` already documents: chasing
every provider's argument/result schema into typed Go fields is
speculative surface with no caller that needs it yet).

The data these fields carry already exists upstream — `Event.Raw` holds
the full provider message (`claudecode.Message` / the ACP
`SessionUpdate`) and is discarded at the wire boundary today; this ADR is
about promoting the parts of it a UI needs (tool identity, arguments,
status, result) into the typed, provider-agnostic `Event` shape, not about
sourcing new data.

**Scope: Codex-native is not covered for tool calls in this pass.**
`internal/codex.Update` only models `item/agentMessage/delta` text
(`internal/codex/client.go:16-23`); Codex's tool-call-equivalent
notifications are a distinct item-lifecycle protocol
(`item/commandExecution/requestApproval`,
`item/fileChange/requestApproval`, and presumably `item/started`/
`item/completed` notifications this package doesn't parse at all) that
would need its own reverse-engineering pass against `refs/codex`. Codex
turns still produce `EventTypeText`/`EventTypeDone` and (via the existing
`codexDeciderAdapter`) permission events exactly as before; they simply
don't yet produce `EventTypeToolCall`. This is an explicit, additive gap,
not a regression — a later ADR-free change (Codex tool-call events are
additive, same as this one) can close it once something needs it.

## Wire shape (`run.attach`/`task.prompt`/`run.logs`)

Three new event names, additive to the existing `chunk` / `done` /
`permission_request` / `permission_resolved`:

- `user_message` — `{"text": "..."}` (same shape as `chunk`'s params).
- `thinking` — `{"text": "..."}` (same shape).
- `tool_call` —
  ```json
  {
    "toolCallId": "tool-1",
    "toolName": "Bash",
    "title": "",
    "status": "running",
    "input": {"command": "echo hi"},
    "result": null
  }
  ```
  A later event with the same `toolCallId` and `status: "success"` (or
  `"failure"`) carries `result` (and, for ACP, may omit `title`/`input` if
  the wire update didn't repeat them — a partial update, not a reset; a
  client merges by `toolCallId` rather than treating each event as the
  full current state).

`run.logs`' batched `runLogEvent.Type` gets the same three new values,
with the same fields (`toolCallId`, `toolName`, `title`, `status`, `input`,
`result`), alongside the existing `text`/`stopReason`/permission fields.

## Persistence (`internal/runs`/`internal/store`)

No `store` schema migration is needed: `run_events.event_data` is already
an opaque JSON blob (`internal/store/schema.sql`), so the new fields are
just additional keys in `internal/runs/persist.go`'s `persistedEvent`
JSON, written and read the same way the four existing fields already are.
An event recorded before this change decodes exactly as before (the new
fields are absent, hence zero-valued); an event recorded after this
change, read by the code that existed before it, would only be reachable
by rolling the daemon binary backward, which is out of scope the same way
it always has been for this store.

## Alternatives considered

- **Keep four types; parse `Event.Raw` client-side.** Rejected: `Raw` is a
  Go value (`claudecode.Message`/`acp.SessionUpdate`), not a wire value —
  it never reaches the web UI at all (`internal/wsapi` doesn't serialize
  it), so this would mean adding a *second*, ad hoc serialization of
  provider-native shapes at the wire boundary, one per provider, with no
  normalization. It would also leak Claude/ACP wire vocabulary into the
  web UI, defeating the point of `taskrunner.Event` being
  provider-agnostic in the first place.
- **Serialize `Event.Raw` untyped and pass it through the wire as-is.**
  Rejected for the same reason: a UI tool-call renderer keyed by wire tool
  name (`audit-deepseek-harness.md` §2's registry shape, the one this plan
  adopts) needs a consistent field name for "the tool name" across
  providers; Claude's `ToolUseBlock.Name` and ACP's `SessionUpdate.Kind`
  don't share a JSON shape today, so "pass it through" would just move the
  normalization problem into every consumer (the web UI *and* the CLI)
  instead of solving it once at the boundary that already exists for this
  exact purpose.
- **Typed normalized events** (chosen). Normalizing once, in
  `internal/taskrunner`, means `internal/wsapi`, `internal/runs`
  persistence, and `cmd/smind`'s CLI rendering all consume the same
  provider-agnostic shape — which is also the shape the plan's Item 8/9
  (timeline renderer, tool-call cards) are written against.

## Rationale

The goal (`docs/plans/active/ui-redesign-parity.md`, itself gated on
`docs/plans/active/smind-dogfood.md`'s "use smind, not Paseo" bar) is
1-to-1 UX parity with Paseo's agent timeline, which the gap matrix ranks
the single highest-impact gap: *"A `<pre>` of text vs typed items with
tool-call cards. Everything about reviewing what an agent did depends on
this."* That mandates structured events; there is no parity-preserving
version of "keep it text." This ADR's job is the schema and the migration
story, not re-litigating whether to do it.

## Compatibility strategy

**Additive for every wire name and every persisted row; see the ACP
caveat under "Old client, new daemon" for the one place it is a re-typing
rather than a pure addition.**

- **Old client, new daemon.** A client that only recognizes
  `chunk`/`done`/`permission_request`/`permission_resolved` ignores any
  event name it doesn't handle -- this is already how
  `cmd/smind/task.go`'s `streamRun` behaves (`if event != "chunk" {
  return }`) and how Go's `encoding/json` behaves when unmarshaling a
  `runLogEvent` with unrecognized extra keys into an older, narrower local
  struct (silently dropped, not an error). No old client *breaks*.

  It is not, however, purely additive on the ACP path, and this ADR
  originally overstated that. An ACP provider's `agent_thought_chunk` and
  `user_message_chunk` used to reach the wire as `chunk` (everything
  `SessionUpdate.Text()` accepted did); they now reach it as `thinking`
  and `user_message`. So a client that renders only `chunk` -- which
  `web/packages/ui`'s `use-run-timeline.ts` `collectText` is, until Item 8
  lands -- shows *less* text for a GLM/Kimi run than it did before,
  without erroring. That is the intended end state (reasoning belongs in
  its own timeline item, not concatenated into assistant prose), but it is
  a visible interim regression for the unmodified UI, not a no-op. Claude
  Code native is unaffected: its thinking blocks were dropped entirely
  before this change, so `thinking` there is genuinely new data.

- **New client, old persisted run.** A run recorded before this change has
  only the four old event types in its `run_events` history; a new
  client's timeline renders exactly what it always would have (text +
  lifecycle), because there is nothing new to render — not an error path,
  the honest absence of data that was never captured.
- **`EventType` is an append-only enum.** It is persisted as its bare
  integer value (`persistedEvent.Type taskrunner.EventType`), so the three
  new constants are appended after the existing four rather than
  interleaved — inserting one earlier in the sequence would silently
  reinterpret every already-persisted event's type on the next decode.
- **No `store` schema change**, per the Decision's persistence section —
  the existing free-form `event_data` column already accommodates new
  fields.
