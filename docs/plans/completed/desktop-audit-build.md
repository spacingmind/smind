# Isolated desktop audit build script

Closed 2026-11-26: shipped as #891 (landed with wave #948); the PR-to-wave box below was never ticked.

Status: completed (2026-11-26; started 2026-11-15). Size: S (3): scope 1 (one script, one test, Taskfile), PRs 1, risk 1 (a build script that must never touch an installed app), decisions 0, verification 1 (macOS build run once by hand).

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

- [x] spec (this file)
- [x] script guards, `DRY_RUN`, `CARGO_TARGET_DIR`
- [x] test and Taskfile wiring (`task test`, `task lint` shellcheck list)
- [x] mutation table
- [x] run once end to end on macOS after the guards
- [ ] PR to `wave/desktop`, review, merge

## Validation

- AC1, AC2, AC5: `override-has-derived-identity` (identifier `<real>.audit` and `<real>.second`, product name, scheme, dead updater endpoint, dry run stops before the build).
- AC1: `committed-config-untouched` (git status of `tauri.conf.json` unchanged).
- AC3: `refuses-bad-audit-id` for empty, `Audit`, `a.b`, `../x`, `a/b`, `1abc`, `a b` and a 26-character id (non-zero exit, nothing written, a refusal message).
- AC4: `refuses-real-app-data-out-dir`, `refuses-under-real-app-data`, `refuses-real-webkit`, `refuses-real-logs`, `refuses-real-caches`, `refuses-applications` (the applications directory is overridden in the test so a broken guard can never write into the real one), `refuses-real-smind-home`, `refuses-under-real-smind-home`, `refuses-symlink-to-real-app-data`; allowed: an app-data sibling with another identifier and the default scratch place under `~/.smind/dogfood`.
- AC6: both files are in the shellcheck list; the test runs in `task test`.
- Mutations (each fails a named scenario): drop the id check; drop the out-dir refusal; identifier not derived; no canonicalisation (symlink and temp-dir aliases); dry run exits non-zero; scratch exemption widened to all of `~/.smind`; applications guard removed; WebKit guard removed. Two guards first survived in an earlier table (`/Applications`, which the mutated script then wrote into): that is why the test now overrides the applications directory.
- End to end (macOS, Apple silicon): the script built `sMind Audit.app` with the shared Rust cache; `Info.plist` shows identifier `dev.spacingmind.desktop.audit`, name `sMind Audit`, scheme `smind-audit`. Launched earlier against an isolated daemon: it adopted it and logged no token.
- Not verified: an Intel host; CI does not run the build (only the dry-run test and shellcheck).
