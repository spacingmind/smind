# Permission prompts: human-in-the-loop tool approval

## Acceptance Criteria

- When an agent (GLM via ACP, or Claude Code native) requests permission to
  run a tool, the daemon can present that request to a human via the web
  UI and block the agent's turn until a human responds — not just
  auto-approve/auto-deny (`acp.AutoApprovePolicy`/`AutoDenyPolicy`,
  `claudecode`'s equivalents), which remain the default for callers that
  don't wire in a human-in-the-loop decider (existing tests/behavior must
  be unaffected when nothing opts into this).
- A pending permission request appears in a running run's event stream
  (visible via `run.attach`'s live tail and `run.logs`'s history, the same
  two paths every other run event already reaches) with enough
  information for a human to decide: what's being requested, and the
  available options.
- A new `run.respondPermission {runId, requestId, optionId}` method lets
  any connection (not just the one that's watching, mirroring
  `run.stop`'s cross-connection reasoning) answer a pending request. The
  agent's blocked `Decide()` call unblocks with that answer and the turn
  continues.
- Both providers' genuinely different request/response shapes (ACP's
  `RequestPermissionParams{Options []PermissionOption}` /
  optionId-string response; Claude Code's `CanUseToolRequest{ToolName,
  Input, ...}` / `(allow bool, updatedInput, denyMessage, ...)` tuple
  response — see Decisions for exact types) are unified behind one
  provider-agnostic shape at the `internal/taskrunner` level, so
  `internal/runs`/`internal/wsapi`/the frontend only ever deal with one
  request/response shape regardless of which provider is running.
- If the run is stopped (`run.stop`) while a permission request is still
  pending, the blocked `Decide()` call unblocks with an error/cancellation
  rather than hanging forever — this needs explicit handling since (per
  investigation) ACP's own permission-request dispatch today runs with
  `context.Background()`, not a context derived from anything
  cancellable; don't rely on the provider's own ctx alone.
- Answering an already-resolved or unknown request id is a clear error,
  not a silent no-op or a panic (a duplicate response — e.g. two browser
  tabs both showing the same pending prompt — must not double-deliver or
  crash).
- Frontend: the task detail timeline shows a pending permission request
  inline (what's being requested, buttons for each option) when one
  arrives on a run it's attached to, and updates once resolved (whether
  resolved from this tab or another connection entirely — the same
  cross-connection-visibility property every other run event already
  has).

## Test Scenarios

- A fake ACP agent scenario that actually issues a real
  `session/request_permission` call (extend
  `internal/acp/fakeagent`/`internal/taskrunner/fakeagent` — check what
  exists today; a permission-request scenario may not exist yet) proving
  the full path end to end: the request appears as an event, the turn
  stays blocked until answered, `run.respondPermission` unblocks it with
  the chosen option, and the agent's own subsequent behavvior (via the
  fake agent's scripted response to different Option Kinds) reflects the
  choice.
- The same for Claude Code native, if a comparable fake/test harness for
  that protocol exists or can reasonably be added; if genuinely not
  feasible in this pass, at minimum unit-test the Claude Code
  request/response adapter's conversion logic in isolation (real inputs
  in, confirm the exact `(allow, updatedInput, denyMessage, ...)` tuple
  the real protocol expects comes out) and say plainly in the report if
  full E2E coverage for this provider wasn't achieved and why.
- Stopping a run while a permission request is pending: `Decide()`
  unblocks (doesn't hang), the run reaches a terminal state, no goroutine
  leak (verify, don't assume).
- `run.respondPermission` from a **different** connection than the one
  watching the run (mirrors `run.stop`'s existing cross-connection test
  pattern in `internal/wsapi/run_test.go`).
- Answering the same request id twice: the second call is a clear error,
  not a panic, not silently accepted.
- Answering an unknown/never-existed request id: a clear error.
- `run.logs` on a run that had a permission request (answered or not by
  the time of the call) shows it correctly in history — not mis-rendered
  as an empty/wrong text chunk (this is a real, specific risk: today
  `toRunLogEvent`'s `default:` case would silently do exactly that for
  any event type it doesn't recognize — confirm your new event types have
  explicit cases there, don't rely on the default).
- Concurrency: run this under `-race`, repeated, same standard as every
  prior concurrency-sensitive piece this session (`internal/runs`,
  `internal/terminal`) — this feature adds a new way for an external
  goroutine (whatever's dispatching the provider's permission-request
  callback) to call into the Registry concurrently with everything else
  already happening on a run.
- Frontend component tests (jsdom + Testing Library + `FakeWsClient`,
  established pattern) for the pending-request UI: renders the prompt
  when a `permission_request`-shaped event arrives, calls
  `run.respondPermission` with the right ids when a button is clicked,
  clears/updates when a `permission_resolved` event arrives (including
  one that didn't originate from this tab's own click).
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  clean. `bunx tsc -b` clean, `bun run test` passes. `task build`
  succeeds; check `internal/server/dist/.gitkeep` as usual.

## Decisions (read before implementing — this is the actual design)

**Where the blocking happens and how the answer gets back in.** The
existing `PermissionPolicy` interfaces
(`acp.PermissionPolicy.Decide(ctx, RequestPermissionParams) (optionID
string, err error)`, `claudecode.PermissionPolicy.Decide(ctx,
CanUseToolRequest) (allow bool, updatedInput map[string]any, denyMessage
string, updatedPermissions []map[string]any, interrupt bool, err error)`
— confirm these exact signatures against the real source before writing
any code, don't trust this doc as gospel) are each invoked synchronously,
on their own goroutine, by their respective provider client
(`internal/acp/client.go`'s per-request dispatch;
`claude-agent-sdk-go`'s `dispatchControlRequest`) — blocking that call is
exactly what "the agent is waiting for a human" means, so blocking is the
right behavior, not something to work around.

Introduce one new, provider-agnostic interface in `internal/taskrunner`:

```go
type PermissionOption struct {
    ID    string // opaque; provider-specific meaning (ACP's optionId; a synthesized "allow"/"deny" for Claude Code)
    Label string
    Kind  string // "allow_once" | "allow_always" | "reject_once" | "reject_always"
}

type PermissionDecider interface {
    Decide(ctx context.Context, summary string, options []PermissionOption) (optionID string, err error)
}
```

`Runner`/`RunPrompt` gain a way to accept one of these per call (today
`acpPermissionPolicy`/`claudePermissionPolicy` are Runner-level fields set
once at construction — that's fine as the *fallback* when no
`PermissionDecider` is supplied for a given call, but a human-in-the-loop
decider is inherently per-run, not a Runner-wide default, since it needs
to know which run's event stream to push the request onto). Decide the
exact mechanism (new `RunPrompt` parameter vs. some other per-call path)
based on what's least disruptive to existing call sites — there are
several today (`internal/runs/registry.go`'s `drive`, and direct
`RunPrompt` calls in tests); grep for all of them before choosing, and
update every one. When no decider is supplied, behavior must be
byte-for-byte identical to today (falls through to the existing
Runner-level auto-policy).

Inside `RunPrompt`, when a `PermissionDecider` is supplied, wrap it in a
small provider-specific adapter satisfying `acp.PermissionPolicy` (trivial
— ACP's own request/response already *is* an options-list-in,
optionId-out shape, just translate the types) or `claudecode.PermissionPolicy`
(needs real design: Claude Code's request has no options list, just a
tool name/input to allow or deny — synthesize two `PermissionOption`s,
e.g. `{ID: "allow", Kind: "allow_once"}` / `{ID: "deny", Kind:
"reject_once"}`, and translate the chosen id back into the real
`(allow bool, ...)` tuple Claude Code's protocol expects).

**Where the request lives and how the UI sees it.** Don't invent a new
side-channel. `internal/runs.Registry.record` is already safe to call
from any goroutine (proven by `Stop`/`History`/`List` already doing
exactly that) — it just locks the run's own mutex, appends to history,
and broadcasts to subscribers. Construct the per-run
`PermissionDecider` implementation *inside* `internal/runs` (it needs
`reg`/`r` in closure, both unexported to that package), and have its
`Decide` call `reg.record` directly with two new `taskrunner.EventType`s
— `EventTypePermissionRequest` (carrying a request id, a summary, and the
options) and `EventTypePermissionResolved` (carrying which request id
and which option was chosen) — reusing 100% of the existing
history/backfill/live-broadcast machinery
(`docs/plans/completed/run-registry-and-cli.md`'s already-proven
`Subscribe` design) with zero changes to that locking logic. This
deliberately bypasses `RunPrompt`'s own `events chan<- Event` entirely
(that channel has exactly one legitimate writer at a time today — the
provider's forwarder goroutine, then the final `EventTypeDone` write —
and the permission decider runs on a *different*, concurrent goroutine;
writing directly to that channel from a third goroutine would be exactly
the close-vs-send race class this project has hit three times already
this session. Going through `reg.record` instead sidesteps this
entirely, since `record` was already designed to be called from any
goroutine safely).

**The pending-response bridge.** The run's own bookkeeping struct (in
`internal/runs/registry.go`) gains a `pendingPermissions map[string]chan
string`, guarded by the same mutex as `history`/`subscribers`. `Decide`
registers a buffered (size 1) channel under a fresh request id, calls
`reg.record` for the request event, then selects on: the channel
(resolved), the *provider's own* `ctx` going Done, AND the run's own
context going Done (construct the decider with access to the run's own
ctx — the same one `drive`/`Start` already has — specifically because
ACP's permission dispatch today does *not* derive its ctx from anything
cancellable, so relying on the provider's ctx alone would let a stopped
run's pending permission request hang forever). New `Registry.RespondPermission(runID,
requestID, optionID string) error`: look up the pending channel,
non-blocking-send into it (`select { case ch <- optionID: default: return
error("already resolved") }` — the buffered-size-1 channel plus a
non-blocking send is what makes double-answering safely detectable
rather than racy), and — same goroutine, right after the send succeeds —
have `Decide` itself call `reg.record` again with the
`EventTypePermissionResolved` event once it wakes up from the channel
receive (not `RespondPermission` doing it directly, so there's exactly
one code path appending both the request and its resolution to history,
in the correct order relative to `Decide` actually returning).

**New wsapi surface.** `run.respondPermission {runId, requestId,
optionId}` → `Registry.RespondPermission`. `attachAndStream` (currently
only forwards `EventTypeText` as `"chunk"`, silently drops everything
else — confirmed by investigation) needs explicit new cases for both new
event types, emitted as their own named events (e.g.
`"permission_request"` / `"permission_resolved"`) so the frontend can
tell them apart from a text chunk. `toRunLogEvent` (used by `run.logs`)
also needs explicit cases — its current `default:` branch would otherwise
silently mis-render either new event type as an empty/wrong text chunk,
not just drop it, which is worse and easy to miss if untested.

**Out of scope for this pass**: allow-lists / "always allow this tool for
this task" persistence beyond what a single `allow_always` Kind value
communicates to the *agent* (the agent process itself, not smind, is what
actually remembers an `allow_always` choice within its own session, per
each protocol's existing semantics) — smind doesn't need to persist or
special-case that value itself, just carry it through faithfully. Also
out of scope: a UI control for "deny with a message" (Claude Code's
`denyMessage` field is real but this pass just uses a fixed message on
deny; a custom-message UI is a reasonable future addition, not required
here).

## Progress

- [ ] `internal/taskrunner`: `PermissionOption`/`PermissionDecider`, new
  `EventType`s, per-call decider wiring, both provider adapters
- [ ] `internal/runs`: per-run pending-permission bridge,
  `RespondPermission`, decider construction in `Start`/`drive`
- [ ] `internal/wsapi`: `run.respondPermission` +
  `attachAndStream`/`toRunLogEvent` new-event-type handling
- [ ] Frontend: pending-permission UI in the task detail timeline
- [ ] Verification (typecheck/tests/build/-race + real-daemon E2E,
  ACP end-to-end at minimum; Claude Code E2E or a documented reason why
  not)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
