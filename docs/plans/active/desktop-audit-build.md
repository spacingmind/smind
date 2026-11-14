# Isolated desktop audit build script

Status: active (2026-11-15). Size: S (3): scope 1 (one script, one test, Taskfile), PRs 1, risk 1 (a build script that must never touch an installed app), decisions 0, verification 1 (macOS build run once by hand).

## Why

A real-window audit of the macOS app, and the manual desktop-macos-app and desktop-auto-update steps, need a build that can run next to an installed sMind. Earlier dogfood runs rebuilt this by hand each time. The ways it goes wrong are all silent: a copy that keeps the real bundle identifier shares the installed app's single-instance lock, app-data directory, WebKit data and settings, and a copy that keeps the `smind://` scheme or the release updater endpoint takes over deep links or talks to the real release feed.

## Decisions

- One script, `scripts/desktop-audit-build.sh`, writes a generated Tauri config override into an output directory and passes it with `--config`. The committed `tauri.conf.json` is never edited.
- The override sets a derived identifier (`<real identifier>.<id>`), the product name `sMind Audit`, the deep-link scheme `smind-<id>` and an updater endpoint that cannot be reached.
- The real identifier is read from `desktop/src-tauri/tauri.conf.json`, not copied into the script, so a rename cannot leave the guard stale.
- The script refuses, before it writes or builds anything:
  - an `AUDIT_ID` that is not a short lowercase word (`[a-z][a-z0-9-]*`, at most 24 characters), so the derived identifier can never equal the real one or escape a path;
  - an output directory that is, or lies under, the real app-data directory (`~/Library/Application Support/<real identifier>`), the real WebKit or log directories for it, `/Applications`, or the real `~/.smind`.
- `DRY_RUN=1` writes the override, prints the derived identifier and exits before the web build, the daemon build and `tauri build`, so the guard is testable on any OS and in CI without a Rust build.

## Acceptance criteria

- AC1: the generated override has the derived identifier, product name, scheme and dead updater endpoint, and the committed config is unchanged.
- AC2: the derived identifier never equals the real identifier for any accepted `AUDIT_ID`.
- AC3: an empty, uppercase, dotted, slashed or over-long `AUDIT_ID` is refused with a non-zero exit and nothing written.
- AC4: an output directory equal to or under the real app-data, WebKit or log directory, `/Applications`, or the real `~/.smind` is refused with a non-zero exit and nothing written.
- AC5: `DRY_RUN=1` exits 0 without running a build.
- AC6: the script and its test are shellchecked in `task lint` and the test runs in `task test`.

## Test scenarios

`scripts/desktop-audit-build_test.sh` (POSIX sh, no network, no build, temp `HOME`):

- `override-has-derived-identity` (AC1, AC2, AC5)
- `committed-config-untouched` (AC1)
- `refuses-bad-audit-id` (AC3): empty, `Audit`, `a.b`, `../x`, 30 characters
- `refuses-real-app-data-out-dir`, `refuses-under-real-app-data`, `refuses-applications`, `refuses-real-smind-home` (AC4)
- Mutation per guard (recorded in Validation): drop the id check, drop the out-dir check, drop the dry-run exit: each makes the named scenario fail.

## Progress

- [ ] spec (this file)
- [ ] script guards and `DRY_RUN`
- [ ] test and Taskfile wiring
- [ ] mutation table
- [ ] run once on macOS (done by hand during the audit; re-run after the guards)
- [ ] PR to `wave/desktop`

## Validation

Filled in when the PR is ready.
