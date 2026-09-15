# 0010: Preserve unknown ACP session-update kinds

## Status

Accepted

## Context

`docs/decisions/0008-structured-run-events.md` normalized ACP's
`session/update` notifications into `taskrunner.Event`'s typed vocabulary
via `internal/taskrunner/runner.go`'s `acpEvent`. That function only
forwards an update whose `Type` its `Text()` helper accepts (a
user/agent/thought text chunk) or whose `Type` is `tool_call`/
`tool_call_update`. Every other update `Type` -- `plan` today, and
whatever kind a future ACP revision or provider adds -- falls through
`acpEvent`'s final `return Event{}, false` and is dropped before it ever
reaches `internal/wsapi`, `internal/runs` persistence, the CLI, or the web
UI. This was flagged as a known, deliberate gap during Item 7's adversarial
review (`docs/plans/active/ui-redesign-parity.md`'s "Known gaps left open
for Items 8/9 / a follow-up") and left open because preserving it meant a
new wire event name -- a wire-contract decision, per AGENTS.md rule (d).

The gap is narrowly about the update's `Type` discriminator, not about
every way an update's payload can fail to normalize: a recognized
text-chunk `Type` whose content block isn't a text block (e.g. an ACP
`image`/`audio`/`resource` chunk) is a separate, narrower, out-of-scope
gap that `acp.SessionUpdate`'s own doc comment already documents and this
ADR does not change.

## Decision

`acpEvent` gains a fallback branch: an update whose `Type` is not one of
the six it already recognizes now produces a new event type,
`taskrunner.EventTypeRaw`, instead of `(Event{}, false)`. The event
carries the original wire kind and the original wire payload verbatim:

- `Event.RawKind string` -- the ACP `sessionUpdate` discriminator value
  the normalizer didn't recognize (e.g. `"plan"`).
- `Event.RawPayload json.RawMessage` -- the update's full original JSON
  (`acp.SessionUpdate.Raw`), so nothing about it is lost even though
  `taskrunner.Event` has no typed field for it.

`EventTypeRaw` is appended after the existing `EventTypeToolCall` (the
last constant today), preserving the append-only ordering
`docs/decisions/0008-structured-run-events.md`'s Compatibility section
requires for `EventType`'s persisted integer encoding.

### Wire shape

A new event name, `raw`, additive to the seven `docs/decisions/
0008-structured-run-events.md` already defined, on both `run.attach`/
`task.prompt` (streamed) and `run.logs` (batched):

```json
{"kind": "plan", "payload": {"sessionUpdate": "plan", "entries": [...]}}
```

`kind` is `Event.RawKind`; `payload` is `Event.RawPayload`, forwarded
as-is (the daemon does not attempt to interpret it).

### CLI (`smind task logs` / `task attach`)

Prints a one-line summary rather than silently advancing past the event:
`[raw] plan: {"sessionUpdate":"plan","entries":[...]}\n` -- the kind, a
colon, and the raw payload's compact JSON. This mirrors the existing
`tool_call` line's shape (`[tool] <name>: <detail>`) closely enough to
read like the same family of annotation, without inventing new
CLI-specific formatting logic for a payload the CLI can't interpret.

### Web UI

No new component. `web/packages/ui/src/hooks/use-run-timeline.ts`'s
`appendTimelineEvent` already renders any event whose wire `type` isn't
one of the names it knows as a generic `TimelineUnknownItem` row (`{kind:
"unknown", eventType: type}`), rendered by `timeline-row.tsx`'s `default`
case as a dashed, muted "Unrecognised event: {type}" card -- built during
Item 8 specifically so a client is never surprised by a daemon-side
`EventType` addition (`ui-redesign-parity.md`'s Item 8 test scenario: "An
event with an unrecognised `type` renders a fallback row and does not
throw"). A `raw` event falls through the same path automatically, with no
UI code change required. This ADR only adds a regression test pinning
that behavior for `raw` specifically, rather than relying on the existing
generic-string test alone.

## Alternatives considered

- **Name the fallback kind `unknown`.** Rejected in favor of `raw`: the
  event's defining property is that it carries the original payload
  untouched (already the vocabulary `Event.Raw`/`acp.SessionUpdate.Raw`
  use for "the untouched original data" elsewhere in this codebase), not
  that its meaning is unknown -- a future client that *does* understand
  `plan` updates would still receive them as `raw` until the daemon grows
  a dedicated `plan` event type, at which point "raw" (a shape statement)
  ages better than "unknown" (a claim about interpretability that becomes
  false).
- **Give `plan` its own dedicated event type/wire name now**, since it's
  the one concrete kind ACP emits today. Rejected: nothing in
  `internal/taskrunner`, the CLI, or the web UI has a use for a plan's
  structured shape yet (Item 11's plan-review permission variant is a
  different code path, permission requests, not session updates), so a
  dedicated `plan` type would be speculative surface with no consumer --
  the same reasoning ADR 0008's Decision section already gives for not
  chasing every provider variant into a typed field. A generic fallback
  gets the data to any consumer that wants it (today: a human reading CLI
  output or an "Unrecognised event" card) without guessing at a schema
  nothing needs yet.
- **Keep dropping unknown kinds, note it in a doc comment.** The status
  quo, and the thing this ADR exists to change: a provider-added kind is
  silent data loss with no signal to a human debugging a run, which is a
  worse failure mode than a slightly-ugly generic card.

## Compatibility

Purely additive, the same shape ADR 0008 and ADR 0009 both establish:

- **Old client, new daemon.** A `run.attach`/`run.logs` consumer that
  doesn't recognize the `raw` event name ignores it exactly as it already
  ignores any other event name it doesn't handle -- no new code path, no
  error, just less information shown, matching the "old client, new
  daemon" story for every event name added by ADR 0008.
- **New client, old persisted run.** A run recorded before this change
  has no `raw` rows in its history (there was nothing to record); a new
  client's timeline renders exactly what it always would have.
- **No `store` schema change.** `run_events.event_data` is the same
  free-form JSON column ADR 0008 already established as accommodating new
  fields; `internal/runs/persist.go`'s `persistedEvent` gains `rawKind`/
  `rawPayload` as two more optional keys.
- **`EventType` stays append-only.** `EventTypeRaw` is added after
  `EventTypeToolCall`, not interleaved, so no already-persisted event's
  integer type is reinterpreted.
