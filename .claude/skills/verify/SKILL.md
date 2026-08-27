---
name: verify
description: Run smind's standard build+test+lint sequence and report pass/fail per step. Use this before considering any bounded code change done (AGENTS.md rule b), or whenever asked to "verify", "check the build", or "make sure it still works".
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

Report format: one line per step (`build: ok`, `test: ok`, `lint: FAILED`),
followed by raw output for any failure.
