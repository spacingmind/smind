# Wire the audit guard into the audit build script, and prove the built app carries it

Status: active (2026-11-15). Follows `desktop-audit-build.md` and `desktop-audit-app-guard.md`. Size: S (3): scope 1 (two scripts, their tests, Taskfile), PRs 1, risk 1 (a build script; the guard itself is already reviewed), decisions 0, verification 1 (a macOS build run).

## Why

`audit-guard` is a cargo feature that no build enables by default. Without the wiring the audit build would not carry the guard, and nothing would notice. Two things must hold: the one build script passes the feature, and the app it produces really refuses to start without a scratch daemon.

## Decisions

- `scripts/desktop-audit-build.sh` passes `--features audit-guard` to `tauri build`. The command line is held in one array that is both printed (`tauri args: ...`, also in `DRY_RUN`) and run, so a test on the printed line is a test on what the real build runs. The build target is computed before the dry-run exit for the same reason (`aarch64` hosts are accepted too).
- A new `scripts/desktop-audit-smoke.sh <app binary>` launches the built app with an empty environment (a scratch `HOME`, no `SMIND_*`) and requires: exit status 2, the refusal message on stderr, and nothing created in its `HOME`. An app that keeps running is killed after `SMOKE_TIMEOUT` seconds (default 15) and fails: that is what a build without the guard does. A missing binary is a clean skip (exit 0, says so), so the check never fails on a machine with no macOS build.
- The build script runs the smoke on the freshly built app as its last step, so a build without the guard never reaches the user as "built".
- The smoke is never pointed at a real build in a test (an unguarded real app would start); its tests use fake apps.
- Nothing else passes the feature: the build test also pins that `default = []` and that no workflow, Taskfile target or tauri config mentions `audit-guard`.

## Acceptance criteria

- AC1: the printed tauri command line contains `--features audit-guard`, `--bundles app` and the override `--config`, and the real build runs exactly that array.
- AC2: the build script runs the smoke on the built app.
- AC3: the smoke passes a fake app that exits 2 with the message and creates nothing; it fails (non-zero, with a reason) one that keeps running, exits 0, exits 2 without the message, or writes into its `HOME`; it is skipped cleanly for a missing binary or no argument; it never passes the caller's `SMIND_*` to the app; it kills a runaway within its timeout.
- AC4: nothing in `.github`, `Taskfile.yml`, `tauri.conf.json`, `tauri.macos.conf.json` or `desktop/package.json` mentions `audit-guard`, and `Cargo.toml` keeps `default = []`.
- AC5: the two new scripts and tests are in `task test` and the shellcheck list.
- AC6 (by hand, macOS): a real build with the script passes its own smoke step.

## Test scenarios

`scripts/desktop-audit-build_test.sh`: `build-args-carry-the-guard-feature` (AC1, AC2, AC4). `scripts/desktop-audit-smoke_test.sh`: `passes-when-the-guard-refuses`, `fails-when-the-app-has-no-guard`, `fails-on-exit-zero`, `fails-on-exit-two-without-the-message`, `fails-when-the-app-writes-before-refusing`, `skips-cleanly-without-a-build`, the caller-environment probe, and the runaway-killed timing check (AC3).

Mutations (each fails a named scenario): drop `--features audit-guard` from the script; the real build ignores the printed array; remove the smoke call from the build; the smoke accepts any exit status; ignores the message; ignores files written; never kills a runaway; passes a running app; leaks the caller's environment; fails instead of skipping.

## Progress

- [x] spec (this file)
- [x] script wiring and the smoke script
- [x] tests and Taskfile wiring
- [x] mutation table
- [x] one real macOS build with the wiring passed its smoke step
- [ ] PR to `wave/desktop`, review, merge

## Validation

Filled in when the PR is ready.
