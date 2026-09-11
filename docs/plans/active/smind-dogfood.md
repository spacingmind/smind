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
