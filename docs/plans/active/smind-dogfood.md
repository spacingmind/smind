# Dogfood smind on smind (Phase 2 definition of done)

Re-scoped by user 2026-09-11 (was: "build a real scopedocs feature
end-to-end from smind UI"). The Phase 2 gate is now: **use smind, not
Paseo, as the daily driver for real work on this repo** — closing the
loop smind was built for. Everything else in Phase 2 is done
(`docs/ROADMAP.md`).

## Acceptance Criteria

- The real daemon runs under the user's real `~/.spacingmind` (not a
  test SMIND_HOME), serving the embedded web UI, with at least one real
  working account (Claude credential imported 2026-09-11 flow).
- Real tasks (bug fixes, plans, reviews — work that would otherwise
  happen in Paseo or a bare terminal) are created, run, and reviewed
  through smind's UI: workspace = the smind repo itself.
- Continuity: dogfood sessions accumulate without a forced return to
  Paseo for anything smind was built to cover (task creation, prompting,
  timeline, diff review, file editing, terminal, permission approval).
- Every real friction hit is recorded in this plan's Decisions/Notes so
  the gap list stays honest — dogfood exists to surface gaps, not to
  pretend there are none.
- Definition of done (quantified): **5 real work sessions** completed
  through smind's UI on the smind repo, each ending with committed work,
  with zero sessions abandoned back to Paseo/terminal-only for reasons
  smind itself could address.

## Test Scenarios

Not unit-testable by nature; evidence is the session log below (date,
task, what smind handled, any gap hit). Each entry must name real
commits that came out of the session.

## Decisions

- Dogfood target = this repo (`~/Coding/personal/smind`) as a workspace.
  No second workspace needed for the gate; scopedocs remains a good
  future stress test but is explicitly not the gate.
- Gaps found during dogfood are logged here first; ones worth fixing
  become their own plans, they don't expand this one.
- Codex turn completion (quota resets 2026-10-07) is orthogonal to this
  plan — dogfood proceeds on Claude/GLM.

## Progress

- [ ] Real daemon + account running (user's ~/.spacingmind)
- [ ] Session 1 (real task, committed via smind UI)
- [ ] Session 2
- [ ] Session 3
- [ ] Session 4
- [ ] Session 5 — gate reached

## Validation

(Session log: append per session — date, task title, commits produced,
gaps hit.)

- **Session 3** (2026-09-13) — closing out `docs/plans/active/task-permission-ux.md`
  Item 7 and validating auto-safe end-to-end:
  - Dispatched tasks 5-11 as smind tasks, mostly `claude-native` +
    `auto-safe` policy. `glm` is still not usable for real dispatch work:
    3 separate runs died mid-exploration, hitting
    `max_turn_requests`/`max_tokens` before producing anything landable.
  - 10 PRs landed this session: #96-#105 (task.createPr RPC + UI, the
    approval-policy selector and CLI flag, GLM surfaced in the accounts
    dialog, the store migration for pre-#93 databases, the
    `provider.test` diagnostic RPC + accounts-dialog health dots, deriving
    the accounts-dialog provider list from `provider.list`, the
    cd-chaining allowlist fix, the `smind account test` CLI, and
    allowlisting `task test`/`task lint`/`task build`).
  - Root-caused the recurring "sandbox blocked every go/task/bun
    invocation" gap logged against earlier sessions: it wasn't a sandbox
    misconfiguration, it was the auto-safe allowlist itself — (1)
    `AllowlistedCommand` only matched a whole-string prefix, so the
    `cd <worktree> && go test ./...` shape real Claude Code turns
    actually emit fell through to manual with no human watching, and (2)
    the allowlist only recognized raw `go`/`gofmt` invocations, not this
    repo's own `task test`/`task lint`/`task build` wrappers. Fixed by
    #103 and #105 respectively.
  - Daemon CPU stayed healthy throughout — 0.1% with 3 concurrent runs in
    flight, no repeat of the PR #95 `pumpEvents` spin.
  - **Remaining gap, discovered by task 12's final summary**: even with
    both allowlist fixes live, `task test`/`task lint` and every git
    write still required approval — because the Claude Code CLI applies
    its own harness-level Bash-tool permission gate *before* smind's
    can_use_tool decider ever sees the request. smind's auto-safe policy
    and the CLI's session sandbox are two independent layers; fixing the
    former (correctly, per #103/#105) does not disable the latter. The
    spawn configuration for claude-native runs needs a sandbox/permission
    mode that defers Bash decisions to the decider (or an explicit
    pre-approved command set at spawn time) — next session's first
    candidate.
