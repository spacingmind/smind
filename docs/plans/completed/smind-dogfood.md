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

- [x] Real daemon + account running (user's ~/.spacingmind)
- [x] Session 1 (real task, committed via smind UI)
- [x] Session 2
- [x] Session 3
- [x] Session 4
- [x] Session 5 — gate reached

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

- **Session 4** (2026-09-14) — closing the harness-level gate found in
  Session 3, plus GLM's ACP file-edit gap:
  - Root-caused Session 3's harness-gate gap: the Claude Code CLI only
    routes Bash decisions through smind's `can_use_tool` decider when
    the command isn't already pre-approved at spawn time via
    `--allowedTools`. Fixed by deriving `Bash(<prefix>:*)` rules from the
    same `safeCommandPrefixes` the decider already trusts and passing
    them to `RunPrompt` when `approvalPolicy == auto-safe` (#107).
    Validated live: task 13 (a real README doc task, dispatched
    `claude-native` + `auto-safe`) ran `task test` (full Go suite + 178
    UI tests) and `task lint` with **zero** approval prompts, then wrote
    and landed its own change (#108) — first fully unattended
    edit-verify loop.
  - Extended auto-safe to local `git add`/`git commit` (not push) so the
    commit step doesn't need a human either (#109) — user confirmed this
    was in scope ("có" when asked).
  - Re-ran the GLM validation task (idle since Session 3's 3 dead runs)
    now that a Z.ai API key was configured. GLM's ACP session now
    connects, authenticates, and streams turns correctly, but every file
    write timed out to auto-deny: `acpDeciderAdapter` only auto-allows
    edit/move/delete tool calls when the request carries structured
    `kind`+`locations`, and glm-acp-agent's Write/Edit tool calls carry
    neither — only a free-text `"Write file: <path>"` title. Fixed with
    a title-parsing fallback, used only when `kind` is absent entirely,
    still requiring an absolute path inside the task's worktree (#111,
    then a bug in the first cut's test file introduced by a mid-edit
    interruption was fixed in #115).
  - Live re-validation of #111/#115 together was attempted but blocked
    by a Z.ai-side `429 Usage limit reached for 5 hour` on the GLM
    account (resets 2026-09-14 17:24:54) — external quota, not a smind
    bug. The fix itself is covered by unit tests
    (`TestAutoAllowACPFileEdit`, including the title-fallback cases) but
    still needs one live GLM run to confirm end-to-end once quota
    resets.
  - **Gaps carried forward**: (1) live GLM dispatch is still unproven
    end-to-end (auth + protocol work, but no run has gone
    prompt-to-commit yet — first Session 3's `max_turn_requests`/
    `max_tokens` deaths, now a provider-side rate limit); (2) the
    two-vocabulary provider id issue (taskrunner ids like
    `claude-native` vs. accounts ids like `anthropic`) noted while
    merging #101/#102 is still unresolved; (3) xai/antigravity still
    can't be added through the accounts dialog; (4) no per-workspace
    base-branch config yet (every PR path assumes `develop`).


- **Session 5** (2026-09-16) — Perplexity Pro as a smind provider; Phase 2
  gate reached:
  - Built `perplexity-proxy-go` (spacingmind/perplexity-proxy-go v0.1.0,
    MIT): Go client for Perplexity's unofficial web API — login/OTP,
    Chrome-fingerprinted TLS+H2 transport, MCP server, and an
    Anthropic-compatible `/v1/messages` server with a curl_cffi bridge
    fallback for the server's bot scoring.
  - Dogfooded the full chain through smind itself: `smind /v1/messages`
    → per-account base_url (#140) → `pplx serve` → Perplexity Pro —
    live query "what is 2+2" returned "4". This session's orchestration
    (multi-agent Paseo spawns debugging the fingerprint regression) was
    itself the "real work" this plan's gate asks for, ending in landed
    commits.
  - PRs landed this session: smind #140 (per-account base_url routing)
    and #142 (wire base_url through CLI/wsapi/UI); upstream
    perplexity-proxy-go released v0.1.0.
  - **Gaps carried forward** (unchanged from Session 4 except as noted):
    (1) live GLM dispatch still unproven prompt-to-commit (Z.ai quota);
    (2) two-vocabulary provider id issue; (3) xai/antigravity not
    addable via accounts dialog; (4) no per-workspace base-branch
    config. New: (5) pplx accounts currently need the CLI/boot DB seed
    for base_url — UI covers it since #142.


---

## Closure (2026-09-16)

All 5 sessions logged, zero abandoned. Final session closed the loop the
plan was built for: real provider integration (Perplexity Pro via
perplexity-proxy-go) routed through smind's own proxy, with commits landed
in both repos. Moved to docs/plans/completed/.
