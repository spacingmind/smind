# 0009: Lifecycle event topics for workspace/space/task

## Status

Accepted

## Context

ADR 0005 established `/ws` event subscriptions with three topics
(`task.status`, `run.status`, `permission.pending`) and left the topic
set deliberately open-ended. None of those three covers *entity
membership*: a workspace, space or task appearing or disappearing. So
every client re-derives the tree by calling `workspace.list` /
`space.list` / `task.list` after its own mutation
(`web/packages/ui/src/components/app-sidebar.tsx`'s `refresh()`), and a
mutation made anywhere else — a second browser tab, or the CLI, which
drives the same daemon over the same `/ws` — is invisible until a manual
reload. That is gap 8 ("cross-client staleness") of
`docs/plans/active/ui-redesign-parity/gap-matrix.md`, and Item 16 of
`docs/plans/active/ui-redesign-parity.md`.

This is a purely additive wire change: no existing topic, payload, or
RPC result changes shape.

## Decision

### Topics

Eight new topics, following ADR 0005's `domain.verb` dotted-lowercase
naming, registered in `knownTopics` so `events.subscribe` accepts them
(and still rejects anything else):

| Topic | Emitted when |
| --- | --- |
| `workspace.created` | a workspace row is created (`workspace.create`, incl. the folder picker's path) |
| `workspace.deleted` | a workspace row is deleted (`workspace.delete`) |
| `space.created` | a space row is created (`space.create`) |
| `space.deleted` | a space row is deleted (`space.delete`) |
| `task.created` | a task row is created (`task.create`) |
| `task.updated` | a task row changes without being created, archived or deleted (today: `Manager.RunTask`'s `created` → `running`) |
| `task.archived` | a task is archived (`task.archive`) |
| `task.deleted` | a task row is deleted |

No `workspace.updated` / `space.updated` topic ships: the daemon has no
workspace- or space-mutating operation at all (no rename, no
re-parenting — see gap matrix C's "Rename / labels: missing"). Registering
a topic nothing can ever publish would be a wire promise with no
implementation behind it; the two names are reserved here so that
whichever change adds renaming adds the topic with the same
`{workspace}` / `{space}` snapshot payload as its `created` twin.

No new run topics: `run.status` (ADR 0005) already carries
`running`/`done`/`error`/`stopped` for every run, which is exactly what
a `run.started`/`run.finished` pair would carry.

### Payload shape: full entity snapshot

Create/update/archive payloads carry the **whole entity**, not an id
plus a change kind:

```
workspace.created  {"workspace": Workspace}
space.created      {"space": Space}
task.created       {"task": Task}
task.updated       {"task": Task}
task.archived      {"task": Task}
```

`Workspace`, `Space` and `Task` are `internal/store`'s structs marshalled
exactly as `workspace.get`, `space.get` and `task.get` already return
them — same PascalCase keys, same fields, byte for byte (those structs
carry no `json:` tags; `web/packages/ui/src/lib/types.ts` already mirrors
them). A client can therefore splice an event payload straight into the
list it fetched from the corresponding `*.list` RPC with no second
mapping layer and no divergence risk between the two shapes.

Delete payloads cannot carry a snapshot — the row is gone — so they carry
identity plus enough parent scope to prune the right subtree without a
refetch:

```
workspace.deleted  {"id": 1}
space.deleted      {"id": 2, "workspaceId": 1}
task.deleted       {"id": 3, "workspaceId": 1, "spaceId": 2 | null}
```

Every payload is a JSON object with named fields rather than a bare
entity, so the deleted variants are the same *kind* of thing as the rest
(an object you read fields off) and so a payload can gain a field later
without changing its type. `task.deleted`'s `spaceId` is always *present*
— `null` for an ungrouped task, never omitted — matching how the same
task's `store.Task.SpaceID` marshals in `task.created`/`task.archived`, so
a client can compare the two directly instead of having to normalise
`undefined` and `null` to each other.

Alternative considered: **id + change kind, client refetches**
(`{"id": 3, "kind": "created"}`). Smaller frames and no risk of a stale
snapshot, but it turns every event into a round trip — the exact
`refresh()`-after-mutation pattern this replaces — and N tasks created by
a CLI loop become N `task.get` calls per connected tab. Rejected.
`refs/paseo` also pushes full snapshots for the same reason (its host
events carry the workspace/session object, not a poke).

Alternative considered: **one `tree.changed` poke topic**. One topic, no
payload design at all, but it forces a full three-call refetch on every
keystroke-level change and throws away the ability to animate a single
row in or out.

### Relationship to `task.status`

`task.status` is unchanged and still fires on create, run and archive.
It is now the status-only projection of `task.created`/`task.updated`/
`task.archived`: a client subscribed to both receives both, in that order
(lifecycle first, so the row exists before its status lands). Clients
tracking tree membership should use the lifecycle topics; `task.status`
remains the cheaper subscription for consumers that only want the status
field (`hooks/use-task-attention.ts`). Retiring it is a separate,
subtractive change and not in scope here.

### Cascades emit only the root event

`workspace.delete` cascades to every space and task under it;
`space.delete` cascades to its tasks. Only the root event is published —
one `workspace.deleted`, not one per descendant. Deleting a container
implies its contents are gone, a client pruning the subtree needs nothing
more, and the per-descendant alternative makes a large workspace's
deletion a burst big enough to trip ADR 0005's 256-event queue bound and
cost the client a full refetch anyway.

### Ordering and delivery

Unchanged from ADR 0005, which governs these topics as it does the
existing three: live-only, no replay, per-connection monotonic `seq`
across all of the connection's topics in delivery order, queue overflow
surfacing as a synthetic `event.dropped`. Two additions specific to
lifecycle events:

- **Publish happens after the store write commits**, from the goroutine
  that performed it, so a delivered event always describes state a
  subsequent RPC would agree with. Note this is "after the write", not
  "only on the method's success return": `CreateWorkspace` publishes
  `workspace.created` as soon as the workspace row commits, before the
  `AddWorkspaceAccount` loop that can still fail the call. That loop's
  failure deliberately leaves the workspace row in place (see
  `CreateWorkspace`'s doc comment), so the event still describes a row
  `workspace.list` returns; suppressing it would hide a real workspace
  from every other client until a manual reload, which is exactly the
  staleness this ADR exists to remove. The client that made the failing
  call sees an error *and* the event, and its upsert-by-`ID` rule (below)
  makes that consistent.
- **An event is not ordered against the RPC response that caused it.**
  The bus hands the event to a per-connection pump goroutine while the
  handler is still returning, so the connection that issued
  `task.create` may see `task.created` *before* its own response.
  Clients must treat an insert as an upsert keyed on `ID` and a delete of
  something absent as a no-op. This is not new — it already holds for
  `task.status` — but it is the first time a client would plausibly try
  to insert a row from both sides.

### Reconciliation

Unchanged and deliberate: there is no cursor and no replay, so a client
re-derives the tree with `workspace.list` / `space.list` / `task.list`
(a) after (re)connecting and (b) on `event.dropped`. The web UI already
gets (a) for free — `docs/plans/completed/daemon-restart-resync.md`
establishes that a successful reconnect installs a genuinely new
`WsClient`, which remounts every consumer's fetch. Lifecycle events are
an optimization over that refetch, never the sole source of truth.

### Where events are emitted

Not in the wsapi handlers. `internal/workspace.Manager` is the single
layer every workspace/space/task mutation passes through — the web UI and
the CLI both reach it through the same `/ws` handlers (`cmd/smind`
dials the daemon; it has no direct store access), and `internal/store` is
below the level where "a task was archived" is a meaningful statement
(`store.DeleteWorkspace` cascades by calling `store.DeleteTask`, which
would publish a `task.deleted` per descendant). So `Manager` grows a
`Notifier` interface and a nil-safe `SetNotifier`, mirroring
`runs.Registry.SetNotifier` (ADR 0005's other publish site), which
`internal/wsapi` adapts onto the bus. Emitting in the handlers instead
would leave any future non-wsapi caller silent and would duplicate the
emit across the four handlers that already share one Manager method.

## Consequences

- `task.commit` and `task.createPr` publish nothing. They change git
  state (a new commit, a pushed branch, an open PR) but not the task
  row, and a full-entity snapshot of an unchanged row is noise. The
  sidebar signal these would feed — changed-file count, PR state (Item 12)
  — is derived git state and needs its own topic with its own payload,
  not a lifecycle event. Out of scope here; noted for the plan item that
  adds it.
- Archiving emits `task.archived` and not `task.deleted`, even though
  `ListTasks` filters archived tasks out and the UI treats archive as
  removal from the tree. The row still exists and `task.get` still
  returns it; a client that wants archive to look like removal decides
  that itself.
