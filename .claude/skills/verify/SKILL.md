---
name: verify
description: Run smind's standard build+test+lint sequence, plus a spec-traceability check against the current plan's Test Scenarios if one exists. Use this before considering any bounded code change done (AGENTS.md rule b), or whenever asked to "verify", "check the build", or "make sure it still works".
---

Run, in order, stopping at (but reporting) the first failure:

1. `task build` — must produce `bin/smind` with no errors.
2. `task test` — `go test ./...` must pass.
3. `task lint` — `go vet ./...` and the gofmt check (tracked `.go` files only,
   via `git ls-files '*.go'` — deliberately excludes `refs/`) must both pass.

If a step fails, paste the raw failing output, then stop — do not run later
steps on top of a known failure unless asked to. Do not modify `Taskfile.yml`
to work around a failure; fix the underlying code, or if the task
configuration itself is the bug, fix that narrowly and re-run from step 1.

## Spec traceability (if a plan doc exists)

If the current work has a `docs/plans/active/<slug>.md` (see the `plan`
skill) with a Test Scenarios section, check each named scenario has an
actual corresponding test — not just that the test suite passes overall, but
that nothing in the list was quietly skipped. Report any scenario with no
matching test explicitly; don't silently pass over the gap. This step is
skipped (not failed) when no plan doc applies to the current work.

Report format: one line per step (`build: ok`, `test: ok`, `lint: FAILED`,
`spec-traceability: 4/4 scenarios covered` or listing the gap), followed by
raw output for any failure.
