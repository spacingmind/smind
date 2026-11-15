# Stop tracking the generated Tauri capability schemas

Status: active (2026-11-15). Size: S (2): scope 1 (a gitignore and one doc), PRs 1, risk 0 (files the build rewrites), decisions 1 (below), verification 1 (a clean build regenerates them).

## Why

`desktop/src-tauri/gen/schemas/*.json` (five files) are written by `tauri-build` on every desktop build, and the daemon version, platform and plugin set change their content, so every Lead who builds the app rewrites them and the diffs collide. They are not source: they carry no decision of ours. The capability files only point at one of them as an editor `$schema` hint for autocompletion, nothing in the build, the tests or CI reads them (`capability_tests.rs` reads `capabilities/*.json`, not `gen/`).

## Decision

Option 1, untrack and ignore, over option 2 (keep them tracked and add a CI "is up to date" check). Reasons: the Tauri project templates ignore `/gen/schemas` for exactly this reason ("will have schema files for capabilities auto-completion"); the other Tauri apps this repo studies do the same; an up-to-date check would force every dependency bump and every platform build to commit a regenerated file and would fail on the platform differences between the Linux, macOS and Windows schemas, which is the churn we are removing. The cost is that the `$schema` hint in the capability files resolves only after the first build, which only affects editor autocompletion.

## Acceptance criteria

- AC1: `git ls-files desktop/src-tauri/gen` is empty and `desktop/src-tauri/.gitignore` ignores `/gen/schemas`.
- AC2: a build from a tree with no `gen/` directory regenerates the five files and leaves the working tree clean (they are ignored).
- AC3: the capability tests and the desktop suite pass without the files present.
- AC4: `desktop/README.md` says the schemas are generated, by which command, and why the capability `$schema` hint needs one build first.

## Test scenarios

`generated-schemas-are-ignored-and-untracked` (a repo test: `git ls-files` shows nothing under `gen/` and `git check-ignore` matches `gen/schemas/capabilities.json`), and the by-hand AC2 run recorded in Validation: delete `gen/`, run the build's check, observe the files return and `git status` stay clean.

## Progress

- [x] spec (this file)
- [ ] untrack, ignore, README
- [ ] clean-build run

## Validation

Filled in when the PR is ready.
