# E9: tri-state verify (PASS / FAIL / INCONCLUSIVE)

Size S (4): scope 2 (two POSIX scripts, the skill text), PRs 1, risk 1 (a verify that says PASS wrongly is the danger), verify 1. Governing: AGENTS.md rule (b) (`task test` and `task lint` before a change is done) and the `verify` skill (`.claude/skills/verify/SKILL.md`).

## Why

Today `verify` is prose: run `task build`, `task test`, `task lint`, report ok or FAILED. An agent that cannot run a step (no `bun`, no network, a full disk, a TMPDIR inside the daemon's home) has no honest word for it: it reports "failed" (a false alarm that gets "fixed" in the code) or skips the step and reports "ok". INCONCLUSIVE is that word.

## Definitions (the contract)

- **PASS**: every step ran and exited 0, and produced a signal (see the exit-0 rules).
- **FAIL**: something ran and failed: a failing test, a compile or type error, a vet or format finding, a panic, a test binary that hit its own deadline. **A flaky test that failed is FAIL**: nothing here retries it, downgrades it or explains it away; "it passes locally" is a reason to investigate, not a result.
- **INCONCLUSIVE**: the step could not run or produced no signal, recognised by a POSITIVE match in the table below. An unrecognised failure is FAIL, never INCONCLUSIVE (the classifier fails closed).
- **Who acts**: an agent must NOT report a change done, ready or verified on INCONCLUSIVE. It says "verification inconclusive: <cause>", fixes the environment when that is within its own remit (install dependencies, set TMPDIR, free space) and re-runs, or reports it to its Lead (a Lead to the Steward/Human) with the cause. FAIL is fixed in the code. Only an overall PASS is "done".

## Interface

`scripts/verify-classify.sh <kind> <exit-code> <output-file>`: `kind` is `build`, `test` or `lint`. Prints three lines `RESULT=<PASS|FAIL|INCONCLUSIVE>`, `CAUSE=<id>`, `DETAIL=<first matching output line, at most 160 chars, or empty>` and exits 0 (PASS), 1 (FAIL), 2 (INCONCLUSIVE). Exit 64 for a usage error (bad kind, missing file, non-numeric exit).

`scripts/verify.sh`: runs `task build`, `task test`, `task lint` in order (command overridable with `VERIFY_TASK` for tests), each with its combined output in `$VERIFY_OUT_DIR` (default a fresh directory under `$TMPDIR`), each limited to `$VERIFY_TIMEOUT` seconds (default 1800) when `timeout` or `gtimeout` exists (otherwise unlimited, said once). It checks the TMPDIR precondition first. One line per step: `build: PASS`, `test: INCONCLUSIVE (deps-not-installed: <detail>)`, `lint: FAIL (test-failed: <detail>)`, `lint: not run (test FAIL)`; for FAIL and INCONCLUSIVE the last 40 lines of that step's output follow. The last line is `VERIFY: PASS|FAIL|INCONCLUSIVE`; exit 0, 1, 2 likewise (FAIL beats INCONCLUSIVE beats PASS). It stops after the first FAIL; an INCONCLUSIVE step does not stop later steps.

## Classification (first match wins; E = exit code)

| # | Condition | Result | Cause |
|---|---|---|---|
| 1 | E = 0 and output contains `shellcheck not installed; skipping` | PASS | `optional-skip` (an optional tool the Taskfile itself skips; the DETAIL line says so) |
| 2 | E = 0, kind `test`, no `^ok` / `Tests .* passed` / `PASS` line | INCONCLUSIVE | `no-signal` |
| 3 | E = 0, kind `test`, every `^ok` line carries `[no tests to run]`, or output has `testing: warning: no tests to run` | INCONCLUSIVE | `no-tests-matched` |
| 4 | E = 0 otherwise | PASS | `ok` |
| 5 | E != 0 and a FAIL marker: `^--- FAIL: `, `^FAIL[[:space:]]+\S+[[:space:]]+[0-9.]+s`, `\[build failed\]`, `^panic: ` (includes `panic: test timed out`), `\.go:[0-9]+:[0-9]+:`, `error TS[0-9]+`, `^[[:space:]]*FAIL[[:space:]]`, `Test Files[[:space:]]+[0-9]+ failed`, `Tests[[:space:]]+[0-9]+ failed`, `no required module provides package`, `is not in std`, and the other runners `task test` drives: `^FAIL:` (the sh tests), `^not ok` (TAP), `^[[:space:]]*✖ ` and `ℹ fail [1-9]` (node --test), `^# fail [1-9]`, `\(fail\)` (bun test) | FAIL | `build-error` for `[build failed]`, `.go:L:C:` (when no `--- FAIL`), the two import messages; `type-error` for `error TS`; else `test-failed` |
| 6 | E in 124, 137, 143 (timeout(1), SIGKILL, SIGTERM) | INCONCLUSIVE | `timeout` (124), `killed` (137, 143) |
| 7 | E != 0, `command not found` or `executable file not found in $PATH` | INCONCLUSIVE | `missing-tool` |
| 8 | E != 0, `Cannot find module`, `ERR_MODULE_NOT_FOUND`, `error: Cannot find package`, `Cannot find package '` | INCONCLUSIVE | `deps-not-installed` |
| 9 | E != 0, `no space left on device` or `ENOSPC` | INCONCLUSIVE | `disk-full` |
| 10 | E != 0, `dial tcp`, `no such host`, `Temporary failure in name resolution`, `i/o timeout`, `connection refused`, `TLS handshake timeout`, `ENOTFOUND`, `ETIMEDOUT`, `ECONNRESET`, `getaddrinfo` | INCONCLUSIVE | `network` |
| 11 | E != 0, nothing above matched | FAIL | `unrecognised` |

A runner precheck (not an output rule): when `TMPDIR` resolves to a path under `$HOME/.smind` or `$SMIND_HOME`, `verify.sh` prints `VERIFY: INCONCLUSIVE (tmpdir-in-daemon-home)` and exits 2 without running a step (tests there collide with a daemon's files; the repo's rule is TMPDIR outside `~/.smind`).

The Go deadline `panic: test timed out` is FAIL on purpose: a test that hangs is a defect (a deadlock), not an environment problem; only the RUNNER's own wall-clock limit (exit 124) is INCONCLUSIVE.

Known limit (row 2): "has a signal" is the union over everything `task test` runs, so one `PASS` from a sh test counts even if the Go step only printed `[no test files]`; the Go suite is large, so this is accepted.

## Acceptance criteria

- AC1 `scripts/verify-classify.sh` implements the table, exit codes and output format above; fixtures under `scripts/testdata/verify/` (real-shaped Go, vitest, bun, task outputs) cover every row.
- AC2 FAIL beats an environment pattern in the same output (a failed test plus a network line is FAIL); an exit-0 output never becomes FAIL; an unrecognised non-zero is FAIL (never INCONCLUSIVE).
- AC3 `scripts/verify.sh` runs the three steps, prints the line format, maps the overall result to exit 0/1/2, stops after the first FAIL, continues past INCONCLUSIVE, applies the TMPDIR precheck, and uses `VERIFY_TASK` so its test runs with a fake `task`.
- AC4 The `verify` skill text states the three results, the flaky rule, the cause table (by reference to this plan or inline), that INCONCLUSIVE is never "done", and who acts; AGENTS.md rule (b) gets one sentence.
- AC5 Both tests are wired into `task test`; the scripts are in the `task lint` shellcheck list.
- AC6 No product code changes; `bin/`, `node_modules` and temp files are not left behind by the tests.

## Named tests

- `scripts/verify-classify_test.sh`: one named case per table row (`row1-optional-skip` ... `row11-unrecognised`), plus `flaky-fails-stay-fail` (a `--- FAIL: TestFlaky` output with `(flaky)` text and a `dial tcp` line elsewhere: FAIL), `exit0-with-network-text-passes`, `usage-errors`.
- `scripts/verify_test.sh`: fake `task` that scripts each step: all pass -> exit 0; test fails -> exit 1 and lint `not run`; test inconclusive -> lint still runs, exit 2; fail plus inconclusive -> exit 1; TMPDIR under `$HOME/.smind` -> exit 2 and no step run; timeout path (exit 124 from the fake) -> exit 2.

## Mutations (committed code, each must fail a named test)

- Table rows 5 and 10 swapped (environment before FAIL) -> `flaky-fails-stay-fail`.
- Default row 11 returning INCONCLUSIVE -> `row11-unrecognised`.
- Row 6 removed -> `row6-timeout`.
- Overall exit for INCONCLUSIVE mapped to 0 -> `verify_test.sh` inconclusive case.
- TMPDIR precheck removed -> `verify_test.sh` tmpdir case.

## Decisions (recommended answers taken; the Human may overrule)

- D1 `panic: test timed out` is FAIL (see above).
- D2 The optional `shellcheck` skip is PASS with a note, not INCONCLUSIVE: the Taskfile skips it by design and CI runs it; making it INCONCLUSIVE would make most local runs inconclusive.
- D3 INCONCLUSIVE does not stop later steps, FAIL does.

## Progress

- [x] spec (this file)
- [x] classifier + fixtures + test (GLM peer, second attempt; Lead review and mutations)
- [x] runner + test (GLM peer)
- [x] skill text, AGENTS.md rule (b), Taskfile and shellcheck wiring (Lead)
- [x] fresh review, mutations, a real run

## Validation

- AC1/AC2: `sh scripts/verify-classify_test.sh` (31 cases: one or more per table row, `flaky-fails-stay-fail`, `exit0-with-network-text-passes`, `usage-errors`, node, sh and TAP failures with environment text in their log). Mutations on committed code, each failing its named case: row 11 returning INCONCLUSIVE (`row11-unrecognised`), exit 124 rule removed (`row6-timeout`), environment rows checked before the FAIL row (`flaky-fails-stay-fail`), exit 0 allowed to become INCONCLUSIVE on network text (`exit0-with-network-text-passes`), the new FAIL markers removed (`row5-sh-test-fail-with-env-text`), the shellcheck-skip pattern unanchored (`row1-echoed-command-is-not-a-skip`).
- AC3: `sh scripts/verify_test.sh` (fake `task`): all pass, test FAIL stops lint, INCONCLUSIVE lets lint run, FAIL beats INCONCLUSIVE, timeout (124), TMPDIR under the daemon home, shellcheck skip, unrecognised, tail lines. Mutations (committed code): INCONCLUSIVE exit mapped to 0 (`test-inconclusive`, `timeout`), TMPDIR precheck removed (`tmpdir-in-daemon-home`), FAIL not stopping later steps (`test-fails`), INCONCLUSIVE stopping later steps (`test-inconclusive`).
- AC4: `.claude/skills/verify/SKILL.md` and AGENTS.md rule (b) rewritten.
- AC5: both tests are in `task test`; the four scripts are in the `task lint` shellcheck list (shellcheck 0 findings).
- AC6: no product code; the real run's `bin/` and output directory were removed.
- A real run: `sh scripts/verify.sh` on this branch (macOS, TMPDIR outside the daemon home) printed `build: PASS`, `test: PASS`, `lint: PASS` and `VERIFY: PASS`, exit 0.
- One fresh review (attack): found that failing node, sh and TAP tests had no FAIL marker, so one with `Cannot find module` or `connection refused` in its log was INCONCLUSIVE (breaking the flaky rule): fixed with the markers above plus three fixtures and cases. Also fixed: the Taskfile-echoed command text no longer counts as a shellcheck skip. Accepted and documented: row 2's signal is a union over everything `task test` runs; `timeout` may leave orphaned children on some systems; the precheck covers `TMPDIR` only.
- Not verified: INCONCLUSIVE on a real machine without `bun` or without network (only fixtures and fake `task`); `gtimeout` path; Linux (shell scripts use POSIX tools only, run on macOS).
