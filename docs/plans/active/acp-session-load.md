# ACP `session/load` history rebuild (ADR-0053 D4, H5, H7)

Spec for the explicit, human-only rebuild of an ACP agent's run history. Builds on the claude/codex rebuilders and the read
contract of ADR-0053; nothing here changes how any other reader sees a run.

## What the protocol says (published ACP, session setup)

- `session/load` is only allowed when the agent advertised `loadSession`; "Clients MUST NOT attempt to call `session/load`" otherwise.
- The agent MUST replay the whole conversation as `session/update` notifications (`user_message_chunk`, `agent_message_chunk`,
  tool call updates) and MUST answer the request only after the last one.
- The page says **nothing** about whether loading modifies the session. smind cannot verify "read-only" from the protocol: it can
  only make sure it asks for nothing else (see Safety). `session/resume` does not replay and is not used.

## Behaviour

- Only for a run whose `native_ref.harness` is `acp`, whose chat has no running run, asked for by a human principal (H7) with
  `run.logs {detail: "full", confirmStart: true}`. `auto` never starts an agent. `detail: "full"` without `confirmStart` answers
  the stored rows with `skeletonReason: "confirm_required"` and starts nothing.
- The daemon starts the provider's own agent command (same launch as a run: scrubbed provider env, plugin confinement, no MCP
  servers, permission policy auto-deny), `initialize`, refuses unless the agent advertised `loadSession`, `session/load` with the
  run's session id and the task worktree as cwd, collects the replay, closes the agent. It never sends a prompt, sets a mode or a
  config option.
- The replay covers every turn of the session. The run's turn is the one whose user message is the run's prompt (the whole user
  text, or its last chunk: smind prepends role/skill text to a first prompt as an earlier block). Exactly one match is required; zero or
  several is `slice_not_found`. The turn's text, thinking and tool calls become events (adjacent text chunks coalesced), then the
  same scrub and elide as every rebuilder.
- Bounds: the shared 2-concurrent slot pool, a 30 s timeout (a process start plus the replay; claude/codex files keep 10 s), 64 MiB
  of replayed bytes. The process is killed on completion, timeout, cancellation and daemon shutdown (the client's close).
- Degrades to the skeleton with a reason: `provider_cannot_rebuild` (no loadSession, or no command), `native_session_missing`
  (agent says the session is unknown, worktree gone), `slice_not_found`, `format_drift` (tool call count off by more than 10%),
  `rebuild_failed` (timeout, size, agent error), `rebuild_busy`.
- A provider found not to advertise `loadSession` is remembered (memory only) and stops offering the button.
- Wire, additive and `omitempty`: result `loadStartsAgent` (the load would start the agent: the client confirms first), param
  `confirmStart`, reason `confirm_required`.
- UI: "Load full history" on an ACP run asks first: "This starts the agent to load the history. It can take a few seconds and uses
  the agent's own session." Cancel / Start agent. Web and mobile.

## Acceptance criteria and named tests

- AC1 `detail:auto` and a machine caller never start an agent. `TestRunLogsACPAutoNeverStartsAgent`, `TestRebuildOnOpenSkipsACP`.
- AC2 `full` without `confirmStart` starts nothing and says `confirm_required`. `TestRunLogsACPFullNeedsConfirm`.
- AC3 an agent without `loadSession` is refused before `session/load`, and remembered. `TestReplayACPSessionRefusesWithoutLoadSession`,
  `TestHistoryInfoACPAndNoLoadSession`.
- AC4 the replay is read without a prompt being sent. `TestLoadSessionReplayCollectsUpdatesAndSendsNoPrompt`.
- AC5 turn slicing: unique prompt match, tail match, none, several. `TestSliceACPTurn*`.
- AC6 same scrub and elision as the other rebuilders; equivalence with a live read of the same turn. `TestRebuildACPEquivalentToLive`.
- AC7 bounds: slot pool busy, timeout kills the process, over-size replay is refused. `TestRebuildACPBounds`.
- AC8 the process does not outlive the call (timeout, cancel). `TestReplayACPSessionKillsAgentOnTimeout`.
- AC9 default reads are byte-identical; ACP runs without a reason change only gain `loadStartsAgent` on a skeleton run.
  `TestRunLogsDefaultReadIsUnchanged` (existing) plus `TestRunLogsACPLabel`.
- AC10 confirm dialog and states in web and mobile, light and dark.

## Safety

The agent is started like any run's agent (scrubbed env, plugin confinement) with auto-deny permissions and no MCP servers, cwd
the task worktree, no prompt. What a given agent does on `session/load` beyond replaying (some may touch the session's last-used
time) is the agent's own and is not verified here.

## Decisions

- ACP rebuilds are not cached (no file identity to key them by); each confirmed load starts the agent once.
- 30 s timeout instead of 10 s.

## Progress

- [x] spec
- [x] acp client replay + fake agent
- [x] native ACP rebuild (slice, bounds) + runs/wsapi wiring
- [x] web + mobile confirm dialog, frames
- [x] review, PR

## Validation

(filled in by the PR)
