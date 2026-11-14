# Crash capture: the desktop shell (DT1 item 3b)

Status: active (2026-11-15). Size: L (10): scope 2 (a new small Rust crate, the shell's `run()`), PRs 1, risk 3 (a panic hook runs in every thread of the app and writes files; redaction is the same privacy promise as the daemon's), decisions 1, verification 3. Second half of `crash-capture.md` (3a, merged: `internal/crash`, the daemon recorder, `smind doctor`, `docs/crash-reports.md`); read its Decisions first, they are not repeated here.

## Why

A panic in the desktop shell (the Tauri app: tray, proxy, daemon manager, updater) leaves nothing on disk: the window disappears and the only trace is whatever a terminal launch printed. 3a records daemon panics; `smind doctor` already looks for the shell's crash directory (`crash.DesktopDir()`) and shows nothing because nothing writes there yet. This PR makes the shell write the same redacted, bounded report there. Nothing is sent anywhere.

## Checked against `wave/desktop` (2026-11-15): what exists, what is dropped

- No panic hook, crash directory or crash code anywhere in `desktop/`. Nothing to drop.
- `smind doctor` already reads `crash.DesktopDir()` (3a). The path must match the directory Tauri calls `app_data_dir` (`<platform app data>/<identifier>`), so the identifier is pinned to `tauri.conf.json` by a test on each side.
- `desktop/daemon-client` is a heavy crate (tonic, protoc, rustls) and `desktop/src-tauri` pulls the whole Tauri stack: a test loop in either would need minutes and gigabytes. The recorder therefore goes in its own crate with only `serde_json` and `regex` (both already in the lock file through Tauri), so its tests run in seconds without any GUI dependency.

## Decisions

- **New crate `desktop/crash-report`** (`smind-crash-report`, `publish = false`, not part of release-please's version bump: it has no released version of its own). The shell calls `smind_crash_report::install(Config)` as the **first statement of `run()`**. `install` does no I/O and never panics: it stores the configuration and sets a `std::panic` hook that records, then calls the previous hook (so the default message, Tauri's own handling and any test harness keep working).
- **Same report, same files** as the daemon: JSON schema 1 with `source: "desktop-shell"`, `site` (`desktop.main` for the main thread, `desktop.thread` for any other: a constant, never the thread name or any panic text), `panicType` (`&str`, `String` or `other`: Rust's analogue of Go's `%T`), `os`/`arch` in Go's vocabulary (`darwin`/`windows`/`linux`, `arm64`/`amd64`) so one reader handles both, `time` RFC 3339 UTC, `message`/`messageWithheld`, `stack` (a forced `std::backtrace::Backtrace`), `suppressedSinceLast`. Same file name scheme `crash-<UTC yyyymmddThhmmss.mmm>-desktop-shell-<site>.json` (with the `-<n>` suffix when the name is taken), so `crash.List` reads them unchanged.
- **Where**: `<app data dir>/crashes/`, the app data dir being what Tauri reports as `app_data_dir` (macOS `~/Library/Application Support/dev.spacingmind.desktop`, Windows `%APPDATA%\dev.spacingmind.desktop`, Linux `${XDG_DATA_HOME:-~/.local/share}/dev.spacingmind.desktop`). The hook cannot ask Tauri (it may fire before the app exists), so the crate derives it exactly as `crash.DesktopDir()` does; a test pins the identifier to `tauri.conf.json` and a Go test pins `DesktopDir()` to the same identifier, so neither side can drift from the real directory.
- **Message withheld by default, the same rule as Go with Rust's vocabulary.** A Rust panic message can carry text (`unwrap()` on an `Err` prints the error's `Debug`; `expect("...")`, `panic!("{}", x)`). A message is kept only when it matches a short allow-list of fully static or number-only runtime messages (`index out of bounds: the len is N but the index is M`, `range end/start index N out of range for slice of length M`, `attempt to <op> with overflow`, `attempt to divide by zero`, `called \`Option::unwrap()\` on a \`None\` value`, `already borrowed: BorrowMutError`, `already mutably borrowed: BorrowError`, `capacity overflow`, `byte index N is out of bounds ...`), still redacted; every other message is withheld (`messageWithheld: true`, the type only). The location (`file:line`) is not stored separately; the stack carries it, redacted.
- **One redaction, two implementations, one set of golden cases.** `internal/crash/testdata/redact_vectors.json` is a list of `{name, input, home, smindHome, want}` produced by the Go `Redact` and checked by a Go test; the Rust crate's test reads the same file (`include_str!`) and must produce the same `want` for every case. A change to either implementation's rules that is not mirrored fails one side's test, so the promise cannot drift. The Rust rules are the same regular expressions in the same order (RE2 syntax is shared by Go's `regexp` and Rust's `regex`), the same three-pass loop, the same whole-path-component home matching and symlink-resolved home form, the same high-entropy filter.
- **Bounds, permissions, link refusal** exactly as 3a: report at most 64 KiB (stack cut with a marker), newest 20 kept, 2 MiB per directory, 10 a minute, an identical panic (same site and same first frame outside std/panic/this crate) inside 5 minutes suppressed and counted, failed writes count toward the limit and the dedupe window, a second report while one is being written is dropped and counted (`try_lock`), directory `0700` and files `0600` on Unix (temp file then rename), a crash directory that is a symlink or a Windows reparse point (junction) is refused and never followed (checked with `symlink_metadata` before any create or chmod), a fresh `.tmp-crash-*` of another writer is kept (age check), a hostile newest file is the doctor's concern (already handled in 3a).
- **Never blocks startup, never panics, never re-enters**: no I/O at `install`; the directory is created on the first report; the hook body runs under `catch_unwind` and a thread-local re-entrancy guard (a panic inside the hook, or a panic while recording, ends the recording silently and goes to the previous hook); every write error is swallowed after one `eprintln` per process.
- **The shell stays up for caught panics only**: the hook records and returns, as hooks do; whether the process dies is Rust's normal rule (a panic in the main thread ends the app after the hook, a panic in a spawned thread ends that thread, `catch_unwind` boundaries in Tauri keep working). No policy table like the daemon's is needed: this PR adds no recovery, only a recorder in front of the existing behaviour.
- No ADR: same reasoning as 3a.

## Acceptance criteria

- AC1: a panic on the main thread and a panic on a spawned thread each produce exactly one report file in the configured directory, with `source: "desktop-shell"`, the right `site`, and the process (the test binary) keeps its normal panic behaviour (the previous hook still runs).
- AC2: every golden redaction vector gives the same output in Go and in Rust (one shared file, tested on both sides); a non-allow-listed panic message is withheld, an allow-listed one kept redacted.
- AC3: bounds and permissions as in 3a (report size, newest 20, 2 MiB, 10 a minute, dedupe, failed writes counted, second concurrent report dropped, `0700`/`0600`, link and junction refused, fresh temp file of another writer kept, same-millisecond names do not overwrite).
- AC4: `install` does no I/O and cannot panic; a bad directory (a file where the directory should be, a read-only parent) never panics or blocks; a panic inside the hook is contained.
- AC5: `run()` calls `install` before anything else (a source test in the shell crate), and the directory the crate derives is the one `crash.DesktopDir()` derives, with the identifier pinned to `tauri.conf.json` on both sides.
- AC6: `smind doctor` shows the shell's reports: a report written by the Rust crate is read by `crash.List` (the cross-language file-format test: the Go test reads a report file produced by the Rust test and checked in as a fixture).
- AC7: `docs/crash-reports.md` documents the shell side: its directory per platform, that the message is withheld unless it is a known runtime message, the same limits, and what it cannot catch (an abort, a stack overflow, a crash in a webview or other native code, a panic before `install`, a `panic = "abort"` build).

### Named tests

`desktop/crash-report` (`cargo test`, no GUI): `crash-rs-main-thread-panic-produces-report`, `crash-rs-thread-panic-produces-report`, `crash-rs-previous-hook-still-runs`, `crash-rs-install-does-no-io`, `crash-rs-redaction-matches-golden-vectors`, `crash-rs-withholds-non-runtime-message`, `crash-rs-keeps-runtime-message-redacted`, `crash-rs-report-fields-and-schema`, `crash-rs-caps-report-size`, `crash-rs-keeps-newest-20`, `crash-rs-caps-dir-size`, `crash-rs-rate-limit`, `crash-rs-dedupes-identical-panics`, `crash-rs-failed-write-counts-toward-rate-limit`, `crash-rs-second-concurrent-report-dropped`, `crash-rs-perms-0700-0600`, `crash-rs-refuses-link-dir`, `crash-rs-write-failure-is-silent`, `crash-rs-hook-panic-is-contained`, `crash-rs-same-millisecond-no-overwrite`, `crash-rs-tmp-file-of-other-writer-kept`, `crash-rs-default-dir-per-platform`, `crash-rs-identifier-matches-tauri-conf`.
`desktop/src-tauri`: `crash-shell-installs-first-in-run` (source test), `crash-shell-identifier-matches-tauri-conf`.
`internal/crash` (Go): `crash-go-redaction-matches-golden-vectors`, `crash-go-reads-rust-report-fixture`, `crash-go-desktop-dir-identifier-matches-tauri-conf`.

### Mutation list (each must fail the named test; results go in Validation and the PR body)

home redaction off -> golden vectors (Rust and Go sides each); one secret rule dropped -> golden vectors; message allow-list widened to everything -> `crash-rs-withholds-non-runtime-message`; keep-N off; size cap off; dir cap off; dedupe off; rate limit off; failed write not counted; `try_lock` -> `lock`; file mode `0600` -> `0644`; link check off (with the directory check off too, it is a second guard); previous hook not called -> `crash-rs-previous-hook-still-runs`; re-entrancy guard removed -> `crash-rs-hook-panic-is-contained`; `install` moved after the builder in `run()` -> `crash-shell-installs-first-in-run`; identifier changed in the crate -> `crash-rs-identifier-matches-tauri-conf`; a Rust golden vector changed -> the Go side fails (and vice versa).

## Progress

- [ ] spec
- [ ] shared golden vectors + Go side tests
- [ ] `desktop/crash-report` crate
- [ ] shell wiring, docs

## Validation

(filled as the PR lands)
