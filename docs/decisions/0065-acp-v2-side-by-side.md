# 0065: ACP v2 side by side with v1 (opt-in while v2 is in Preview), and seat instructions through the stable system prompt

## Status

**Proposed. Not accepted. No code.** Written 2026-11-28 for the Human to decide.

Builds on [ADR-0041](0041-in-smind-orchestration.md) (seat instructions for role seats) and the ACP provider work in `internal/acp`. Every claim about the protocol was read in the protocol repository's v1 and v2 docs (`docs/protocol/v2/migration.mdx`, `docs/protocol/v1/session-setup.mdx`); every claim about sMind was read in the code on `develop` at this date.

## Context

**sMind speaks ACP v1 only.** `internal/acp/client.go` sends `protocolVersion: 1` in `initialize` (`const protocolVersion = 1`, line 18) and handles the v1 client side: `fs/read_text_file` and `fs/write_text_file` (line 541), `session/load`, `session/set_mode`, and a `session/prompt` whose response ends the turn.

**ACP v2 is in Preview.** It is ready to build on and is not frozen. The protocol's own guidance is to support both versions side by side: negotiate the version per connection, keep v1 working, add v2 next to it. A v1-only agent answers `protocolVersion: 1` to a client that asks for 2, and the client continues only if it supports v1, so asking for 2 never breaks an old agent. Agents that speak v2 will serve it to any client that asks for it; clients choose when to ask.

**What v2 changes for a client like sMind** (migration guide):

1. `session/prompt` no longer ends the turn. Its response only acknowledges the prompt. Progress and the stop reason (failures included) arrive as `state_update` notifications (`running`, `idle`, `requires_action`, `unknown`).
2. Updates are upserts: messages and tool calls are patched by id (omitted = unchanged, `null` = cleared, value = replaced, chunks append). The first `tool_call_update` for an id creates the tool call. Plans are replaced whole by `planId`.
3. The client file system (`fs/*`), client terminals (`terminal/*`), `session/set_mode` and `session/load` are removed. Modes become config options; `session/load` becomes `session/resume` with `replayFrom`. When an agent needs client-side tools, the client offers them as MCP servers.
4. Capabilities are reorganised: one `capabilities` and a required `info` on both sides, support markers are objects, `authenticate` becomes `auth/login`.
5. New: `idempotencyKey` on `session/prompt` (unstable), `tool_call/cancel`, `replay_complete`, display-only agent terminals.

**Seat instructions today.** ADR-0041 role text for ACP seats is prepended to the first prompt of a new session and skipped on resume (`internal/taskrunner/runner.go:703-717`, `roleInstructionsPreface`), so a changed profile reaches new sessions only and the text is part of the visible first turn. ACP v1 already has a stable alternative: an agent that advertises `sessionCapabilities.systemPrompt` accepts `systemPrompt` (`mode: append`, content blocks) on `session/new`, `session/load` and `session/resume`. sMind does not send it. (The claude-native path already uses the SDK's append-system-prompt option, `runner.go:1074`.)

## What is decided and what is open

| | State |
|---|---|
| sMind stays on v1 by default | Decided by the protocol's own guidance (v2 is Preview; v1 peers stay common). |
| Everything below | **Open**, each with a recommendation. |

## The choices

### 1. Send seat instructions as the v1 `systemPrompt` where the agent advertises it

- **A. Yes, when advertised; otherwise keep the first-prompt preface.** Instructions survive a resume and leave the visible first turn. Small and independent of v2. Cost: two paths, and an agent that advertises the capability but applies it badly (test with the agents we ship recipes for).
- **B. Keep the preface everywhere.** No change; the resume gap stays.

**Recommendation: A.** Ship it first, as its own small slice. A `systemPrompt` the agent cannot apply returns `-32602`; on that error fall back to the preface for the session and say so in the run notes.

### 2. How v2 is chosen

- **A. Per provider, opt-in setting (default v1).** A provider entry (and a profile or recipe) can say `acp.protocol: 2`. The client sends `protocolVersion: 2` for that provider only; if the agent answers 1, sMind continues on v1 (the negotiated version decides the handlers). Safe, testable one provider at a time.
- **B. Ask for 2 everywhere, fall back to v1.** Fastest to exercise, but every ACP provider becomes a v2 test subject while v2 can still change.
- **C. Wait.** No cost now, but the first agent that drops v1 breaks.

**Recommendation: A.** A global switch that flips the default belongs to a later decision, after v2 leaves Preview.

### 3. Shape of the code

One connection speaks exactly one negotiated version after `initialize`. So:

- **A. A version-specific adapter behind the existing `acp.Client` surface** (`Prompt`, `LoadSession`, cancel, permission, config): a `v1` and a `v2` implementation chosen after `initialize`. The runner and everything above it stay unchanged. The hard part is the turn lifecycle: v1 ends a turn on the prompt response; v2 on `state_update: idle` with a stop reason. The adapter maps both to the same "turn ended with stop reason X" the runner already consumes.
- **B. Branches inside the one client.** Less code at first, a tangle after the second protocol change.

**Recommendation: A.**

### 4. What sMind must provide itself under v2

Under v2 an agent can no longer ask sMind to read or write files, or to run terminals. Two consequences:

- The `fs/read_text_file` guard (workspace-boundary check, line 541) has no v2 caller; keep it for v1 and do not re-create it. Where an agent needs client-side tools, the answer is MCP servers sMind already serves (the daemon's own endpoint), under the same permission policy as today. **Recommendation: no new client-side tool surface for v2.**
- Mode switching: v1 `session/set_mode` becomes `session/set_config_option`; sMind's permission-mode control must map to the agent's config option where one exists, and say "this agent has no mode option" otherwise. **Recommendation: map it in the adapter, one test per shipped agent.**

### 5. `idempotencyKey` for the relay

The relay resends a prompt after a reconnect; v2's `idempotencyKey` (unstable) would make a resend safe. **Recommendation: not now.** It is unstable; sMind's own run queue already de-duplicates sends. Revisit when it is stable.

### 6. Testing without a real v2 agent

There is `internal/acp/fakeagent`, which speaks v1. **Recommendation:** extend it with a v2 mode (a flag), so the adapter has a deterministic peer: version negotiation both ways, the `state_update` lifecycle, an upserted tool call, `session/resume` with replay, a config-option mode change, `-32022` on a bad version. One real v2 agent is a Human smoke test, listed as not verified until done.

## Security and safety

- Under v2 the agent runs its own terminals and file access. That is the agent's own sandbox plus sMind's existing permission requests (`session/request_permission` keeps its name, its params are restructured: a required `title`, an optional `subject`); the card must still show who, why and exactly what from those fields.
- A relayed or failed permission request is never an approval; v2's wording makes this binding ("a failed permission request is not approval", "explicit permission rejection binding"). Add these as test scenarios in both versions.
- An unknown enum value or update kind is possible by design in v2; the adapter must ignore unknown kinds without ending the turn and log them once.

## Migration and compatibility

No installed base depends on v2, so there is no migration. The default stays v1; a provider opts in. A provider that opts in and meets a v1-only agent keeps working.

## Phased implementation (for the Human to approve)

1. **System prompt for seats (choice 1).** S. Small, independent, ships first.
2. **Adapter seam (choice 3):** extract the v1 behaviour behind an interface with no behaviour change. S-M, risk normal.
3. **Fake agent v2 mode and the v2 adapter** behind `acp.protocol: 2`, off by default (choices 2, 4, 6). L, risk high (turn lifecycle, permissions): validator and two fresh reviewers.
4. **Real-agent smoke and docs.** Human step.
5. **Later:** `idempotencyKey` (choice 5), default flip after v2 leaves Preview.

## Decisions needed from the Human

1. **Send seat instructions as `systemPrompt` where advertised?** Recommended: **yes**, slice 1.
2. **How is v2 chosen?** Recommended: **per provider, opt-in, default v1** (choice 2A).
3. **Adapter per version behind the current client surface?** Recommended: **yes** (choice 3A).
4. **No new client-side file or terminal surface under v2; mode through config options?** Recommended: **yes**.
5. **`idempotencyKey` now?** Recommended: **no**, wait for stable.
6. **A v2 mode in the fake agent as the test peer, real-agent smoke as a Human step?** Recommended: **yes**.
7. **When does the v2 slice start?** Recommended: **after the Astryx waves and the P1 batch**; slice 1 can go earlier.
