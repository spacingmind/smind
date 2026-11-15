# Wire the audit guard into the audit build script, and prove the built app carries it

Status: active (2026-11-15). Follows `desktop-audit-build.md` and `desktop-audit-app-guard.md`. Size: S (3): scope 1 (two scripts, their tests, Taskfile), PRs 1, risk 1 (a build script; the guard itself is already reviewed), decisions 0, verification 1 (a macOS build run).

## Why

`audit-guard` is a cargo feature that no build enables by default. Without the wiring the audit build would not carry the guard, and nothing would notice. Two things must hold: the one build script passes the feature, and the app it produces really refuses to start without a scratch daemon.

## Decisions

- `scripts/desktop-audit-build.sh` passes `--features audit-guard` to `tauri build`. The command line is held in one array that is both printed (`tauri args: ...`, also in `DRY_RUN`) and run, so a test on the printed line is a test on what the real build runs. The build target is computed before the dry-run exit for the same reason (`aarch64` hosts are accepted too).
- A new `scripts/desktop-audit-smoke.sh <app binary>` launches the built app twice, each with a scratch `HOME` and an otherwise empty environment, and requires each time exit status 2, the refusal message naming the missing variable, and nothing created in `HOME` or the scratch `SMIND_HOME`:
  1. `SMIND_HOME` unset and `SMIND_DAEMON_URL=http://127.0.0.1:9` (the discard port). The URL is always set so a build WITHOUT the guard, exactly what the smoke exists to catch, can never attach to the real daemon on :4648 or start toward it: it can only try the discard port.
  2. Only after launch 1 passed (which proves the guard exists): a scratch `SMIND_HOME` and no URL, with a short timeout. A guard-less build would try :4648 here, so this launch is never made for an app that failed launch 1.
  An app that keeps running is killed after the timeout and fails (that is what a build without the guard does). A missing binary is a clean skip (exit 0, says so), so the check never fails on a machine with no macOS build.
- The build script runs the smoke on the freshly built app as its last step, so a build without the guard never reaches the user as "built".
- The smoke is never pointed at a real build in a test (an unguarded real app would start); its tests use fake apps.
- Nothing else passes the feature: the build test also pins that `default = []` and that no workflow, Taskfile target or tauri config mentions `audit-guard`.

## Acceptance criteria

- AC1: the printed tauri command line contains `--features audit-guard`, `--bundles app` and the override `--config`, and the real build runs exactly that array.
- AC2: the build script runs the smoke on the built app.
- AC3: the smoke passes a fake app that refuses both launches with exit 2 and a message naming SMIND_HOME and SMIND_DAEMON_URL and creates nothing, and the launches carry exactly the promised environment (launch 1: discard URL, no SMIND_HOME; launch 2: scratch SMIND_HOME, no URL; nothing of the caller's leaks); it makes launch 2 only after launch 1 passed; it fails (non-zero, with a reason) one that keeps running, exits 0, exits 2 without the message, or writes into its `HOME`; it is skipped cleanly for a missing binary or no argument; it never passes the caller's `SMIND_*` to the app; it kills a runaway within its timeout.
- AC4: nothing in `.github`, `Taskfile.yml`, `tauri.conf.json`, `tauri.macos.conf.json` or `desktop/package.json` mentions `audit-guard`, and `Cargo.toml` keeps `default = []`.
- AC5: the two new scripts and tests are in `task test` and the shellcheck list.
- AC6 (by hand, macOS): a real build with the script passes its own smoke step.

## Test scenarios

`scripts/desktop-audit-build_test.sh`: `build-args-carry-the-guard-feature` (AC1, AC2, AC4). `scripts/desktop-audit-smoke_test.sh`: `passes-when-the-guard-refuses` (also pins both launch environments), `fails-when-the-app-has-no-guard` (with the guard-less app launched exactly once, and only ever offered the discard URL), `fails-when-the-guard-skips-the-url-check`, `fails-when-the-refusal-names-another-variable`, `fails-on-exit-zero`, `fails-on-exit-two-without-the-message`, `fails-when-the-app-writes-before-refusing`, `skips-cleanly-without-a-build`, the caller-environment probe, and the runaway-killed timing check (AC3).

Mutations (each fails a named scenario): drop `--features audit-guard` from the script; the real build ignores the printed array; remove the smoke call from the build; the smoke accepts any exit status; ignores the message; ignores files written; never kills a runaway; passes a running app; leaks the caller's environment; fails instead of skipping.

## Progress

- [x] spec (this file)
- [x] script wiring and the smoke script
- [x] tests and Taskfile wiring
- [x] mutation table
- [x] one real macOS build on this commit passed its own smoke step; the built app also starts with the scratch environment
- [ ] PR to `wave/desktop`, review, merge

## Validation

- AC1, AC2, AC4: `build-args-carry-the-guard-feature` in `scripts/desktop-audit-build_test.sh` (the dry run prints the exact tauri command line; it must carry `--features audit-guard`, `--bundles app` and the override `--config`; the real build runs the same array; the build runs the smoke; `Cargo.toml` keeps `default = []`; no workflow, Taskfile target or tauri config mentions `audit-guard`).
- AC3: `scripts/desktop-audit-smoke_test.sh`: `passes-when-the-guard-refuses` (also pins both launch environments), `fails-when-the-app-has-no-guard` (with the guard-less app launched exactly once, and only ever offered the discard URL), `fails-when-the-guard-skips-the-url-check`, `fails-when-the-refusal-names-another-variable`, `fails-on-exit-zero`, `fails-on-exit-two-without-the-message`, `fails-when-the-app-writes-before-refusing`, `skips-cleanly-without-a-build`, the caller-environment probe, and the runaway-killed timing check.
- AC5: both scripts and both tests are in the shellcheck list and `task test` (shellcheck at `-S style` clean).
- AC6: a real macOS build with this script (Apple silicon, shared Rust cache) ended with `desktop-audit-smoke: ok, the built app refuses to start without a scratch daemon`; the same bundle then started and kept running with the scratch `SMIND_HOME` and a loopback `SMIND_DAEMON_URL` (the guard passes in the allowed case).
- Mutations (each fails a named scenario): drop the feature flag; the real build ignores the printed array; remove the smoke call; the smoke omits the discard URL, points it at the real port, skips launch 2, gives launch 2 a URL, makes launch 2 first, treats a launch-1 failure as non-fatal, does not require the variable name, does not check the exit status, the message or files in HOME, leaks the caller's environment, never kills a runaway, or fails instead of skipping; a workflow mentioning `audit-guard`.
- Full local suite on this commit: `cargo test` (default and `--features audit-guard`, with the real macOS `TMPDIR`), `cargo clippy --all-targets -- -D warnings` for both feature sets, `go test ./internal/hygiene/...`, shellcheck as in the Taskfile, both script tests.
- Not verified: an Intel host; Windows and Linux for the build step (macOS only; the dry-run test runs anywhere with `jq`).
