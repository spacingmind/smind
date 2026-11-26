---
name: verify
description: Run smind's standard build+test+lint sequence with a tri-state result (PASS, FAIL, INCONCLUSIVE), plus a spec-traceability check against the current plan's Test Scenarios if one exists. Use this before considering any bounded code change done (AGENTS.md rule b), or whenever asked to "verify", "check the build", or "make sure it still works".
---

Run `sh scripts/verify.sh`. It runs, in order, `task build`, `task test` and
`task lint` (the same three steps as before), classifies each step's exit code
and output with `scripts/verify-classify.sh`, and ends with one line:
`VERIFY: PASS`, `VERIFY: FAIL` or `VERIFY: INCONCLUSIVE` (exit code 0, 1, 2).
Spec: `docs/plans/completed/e9-tri-state-verify.md` (the table of causes).

## The three results

- **PASS**: every step ran, exited 0 and produced a signal. Only an overall
  PASS means the change is verified.
- **FAIL**: something ran and failed: a failing test, a compile or type error,
  a vet or gofmt finding, a panic, a test binary that hit its own deadline.
  **A flaky test that failed is FAIL.** Nothing retries it or downgrades it;
  "it passed on the second run" is a bug to investigate, not a result. An
  unrecognised non-zero exit is FAIL too (the classifier fails closed).
  Paste the raw failing output and fix the code. Do not modify `Taskfile.yml`
  to work around a failure; if the task configuration itself is the bug, fix
  that narrowly and re-run from the start.
- **INCONCLUSIVE**: the step could not run or produced no signal, recognised
  by a positive match: `missing-tool`, `deps-not-installed` (for the web UI:
  `bun install` was not run), `network` (a download offline), `disk-full`,
  `timeout` (the runner's own wall-clock limit), `killed`, `no-signal` or
  `no-tests-matched` (an exit 0 that ran no test), or `tmpdir-in-daemon-home`
  (`TMPDIR` inside `~/.smind`; use a directory outside it).

## What to do with INCONCLUSIVE

**Never report a change as done, ready or verified on INCONCLUSIVE.** Say
"verification inconclusive: <cause>" and either fix the environment yourself
when that is within your remit (install the dependencies, set `TMPDIR`, free
disk space) and re-run, or hand it to your Lead (a Lead to the Steward or the
Human) with the cause and the step. A later FAIL or PASS replaces it; nothing
else does.

## Spec traceability (if a plan doc exists)

If the current work has a `docs/plans/active/<slug>.md` (see the `plan`
skill) with a Test Scenarios section, check each named scenario has an
actual corresponding test, not just that the test suite passes overall, but
that nothing in the list was quietly skipped. Report any scenario with no
matching test explicitly; don't silently pass over the gap. This step is
skipped (not failed) when no plan doc applies to the current work.

Report format: one line per step as `verify.sh` prints it (`build: PASS`,
`test: INCONCLUSIVE (deps-not-installed: ...)`, `lint: FAIL (...)`), the
`VERIFY:` line, `spec-traceability: 4/4 scenarios covered` (or the gap), and
the raw output `verify.sh` printed for any FAIL or INCONCLUSIVE step.
