# The audit app refuses to start against the real daemon

Closed 2026-11-26: AC6 was delivered by the guard-wiring PR #897 and the work is on develop; the PR-to-wave box below was never ticked.

Status: completed (2026-11-26; started 2026-11-15). Follows `desktop-audit-build.md`. Size: S (4): scope 1 (one Rust module, one cargo feature, the build script), PRs 1, risk 2 (a guard that must fail closed, and a release build that must not change), decisions 0, verification 1.

## Why

The audit build already has its own bundle identifier, scheme and app-data directory, but it still finds its daemon the way the release app does: `SMIND_DAEMON_URL`, else the default `127.0.0.1:4648`, and the token from `SMIND_HOME`, else `~/.smind`. Launched by hand without the scratch variables, an audit app would attach to the real daemon and read the real token. The guard makes that impossible: the built audit app checks its environment first and exits with a clear message before it touches any daemon.

## Decisions

- The guard is compiled in only by a cargo feature, `audit-guard`, which is not a default feature and is passed only by `scripts/desktop-audit-build.sh` (`tauri build --features audit-guard`). A release build never sets it, so the guard code and its one extra dependency are absent.
- The one extra dependency, `libc` (to read the account's real home directory from the user database, because an isolated launch sets `HOME` to a scratch directory), is optional and enabled only by the feature.
- The check is a pure function over an environment lookup and the real home directory, so it is tested without a window. A thin `enforce_or_exit` runs it at the very top of `run()`, before the Tauri builder, plugins, single-instance lock, logging or any network or file access, prints the reason to stderr and exits with status 2.
- The rule: the app starts only when all of these hold.
  - `SMIND_HOME` is set and non-empty, and its path (symlinks resolved, for the deepest existing ancestor) is not the real `~/.smind`, not under it unless under `~/.smind/dogfood`, and not `$HOME/.smind` of the current environment either (covers a failed user-database lookup).
  - `SMIND_DAEMON_URL` is set, parses as an `http` or `https` URL, has an explicit port other than 4648 and 4870 (the daemon and its alternate default), and a loopback host (`127.0.0.1`, `::1`, `localhost`). Stricter than "not :4648" on purpose: an audit build has no reason to talk to a remote daemon, and 4870 is the other port the project reserves for the real one. Drop either extra condition if unwanted.
- No behaviour change without the feature: `default = []`, no workflow or Taskfile target passes the feature, and a test pins both.

## Acceptance criteria

- AC1: with the feature, `SMIND_HOME` unset or empty, `SMIND_HOME` equal to or under the real `~/.smind` outside `dogfood` (also through a symlink), or `SMIND_HOME` equal to `$HOME/.smind`, exits 2 with a message naming the variable, before any daemon access.
- AC2: with the feature, `SMIND_DAEMON_URL` unset or empty, unparseable, non-http(s), without an explicit port, on port 4648 or 4870, or on a non-loopback host, exits 2 with a message naming the variable.
- AC3: with both set to scratch values (a home under `~/.smind/dogfood` or elsewhere outside `~/.smind`, a loopback URL on another port), the check passes and the app starts as before.
- AC4: the check runs first in `run()`, before the Tauri builder is created.
- AC5: a build without the feature is unchanged: `default = []` in `Cargo.toml`, no `libc` in the default dependency graph, and no workflow, Taskfile target or script other than `scripts/desktop-audit-build.sh` mentions `audit-guard`.
- AC6: `scripts/desktop-audit-build.sh` passes `--features audit-guard`, and its dry-run test pins that.

## Test scenarios

Rust unit tests in `src-tauri/src/audit_guard.rs`:

- `home_must_be_set` (unset, empty), `home_rejects_the_real_smind_home`, `home_rejects_under_the_real_smind_home`, `home_allows_dogfood_under_the_real_home`, `home_allows_a_scratch_dir_elsewhere`, `home_rejects_a_symlink_onto_the_real_home`, `home_rejects_the_env_home_dot_smind`.
- `url_must_be_set`, `url_rejects_garbage_and_other_schemes`, `url_needs_an_explicit_port`, `url_rejects_4648_and_4870`, `url_rejects_a_remote_host`, `url_allows_a_loopback_scratch_port` (127.0.0.1, ::1, localhost).
- `run_enforces_the_guard_before_building_the_app` (source order in `lib.rs`).
- `audit_guard_is_off_by_default` (without the feature, `cfg!` is false).

Integration test `src-tauri/tests/audit_guard_launch.rs` (compiled only with the feature): runs the built binary with each bad environment and asserts exit status 2, the message on stderr, and that no app-data directory was created and nothing listened or connected (the process exits before the window exists).

Shell test additions in `scripts/desktop-audit-build_test.sh`: `Cargo.toml` has `default = []`, no workflow or Taskfile target passes `audit-guard`, and the build script's tauri command carries the feature.

Mutations (each must fail a named test): drop the home check; drop the URL port check; drop the loopback check; accept an unset home; remove the `enforce_or_exit` call from `run()`; make the feature a default feature.

## Progress

- [x] spec (this file)
- [x] `audit_guard.rs`, the feature, the call in `run()`
- [x] integration test (kills a child that a broken guard let start)
- [x] mutation table
- [ ] AC6: the build script passes `--features audit-guard` and its test pins it. Waits for #891 (the script) to merge into `wave/desktop`; it is then a second, small PR or commit on this branch after merging `wave/desktop` in (no stacking, no rebase).
- [ ] PR to `wave/desktop`, review, merge

## Validation

- AC1: `home_must_be_set`, `home_rejects_the_real_smind_home`, `home_rejects_under_the_real_smind_home`, `home_rejects_a_symlink_onto_the_real_home`, `home_rejects_the_env_home_dot_smind` (unit) and the launch tests `refuses_with_no_environment`, `refuses_without_a_scratch_home`, `refuses_the_real_smind_home_even_with_a_scratch_home_env` (the built binary, exit 2, message, no files created).
- AC2: `url_must_be_set`, `url_rejects_garbage_and_other_schemes`, `url_needs_an_explicit_port`, `url_rejects_4648_and_4870`, `url_rejects_a_remote_host` and the launch tests `refuses_without_a_daemon_url`, `refuses_the_real_daemon_port`.
- AC3: `home_allows_dogfood_under_the_real_home`, `home_allows_a_scratch_dir_elsewhere`, `url_allows_a_loopback_scratch_port`. (The passing path is not launched in a test: it would start the app.)
- AC4: `run_enforces_the_guard_before_building_the_app` (source order and the feature gate).
- AC5: `audit_guard_is_not_a_default_feature` (runs with and without the feature), `audit_guard_is_off_by_default`; `cargo test --lib` without the feature passes and does not build `libc` into the default graph (the dependency is optional and only the feature enables it).
- AC6: pending, see Progress.
- Mutations (13, each killed by a named test): drop the home check; drop the URL check; drop the real-port check; drop the loopback check; drop the explicit-port requirement; widen the dogfood exemption; no symlink resolution; skip `$HOME/.smind`; remove the call from `run()`; call it after the builder; make the feature a default feature; exit 0 instead of 2; remove the real-home lookup. The last one let the real app start and hang the launch test: the test now kills a child still running after 10 seconds.
- Path comparison (found by Windows CI): paths are compared as separator-independent segments (symlinks resolved for the existing part, `/` and, on Windows, `\` split, `.` dropped, `..` applied lexically, case folded, a verbatim `\\?\` prefix removed) and the guard fails closed: an unknown real home, or a path or real home that cannot be compared, is refused. Tests: `home_rejects_under_the_real_smind_home` (platform-built fixtures in a temp dir, including `..` that returns into the real home), `home_rejects_the_real_home_in_any_separator_and_case_spelling`, `home_rejects_a_dogfood_escape`, `home_fails_closed_when_the_real_home_is_unknown`, `segments_apply_dots_and_both_separators_lexically`. Mutations: `..` not applied, no case folding, no symlink resolution, unknown real home allowed, verbatim prefix kept, UNC mishandled: each killed.
- Path comparison, third version (found by macOS CI, where the temp dir is under the symlinked `/var`): one normalisation function (`split_segments_for`, table-tested for both separator rules on every OS) and two readings of `SMIND_HOME`, both checked: the OS reading walks components resolving symlinks as it goes (so `/var` and `/private/var` are one place and `link/..` is the parent of the link's target) and the cleaner's reading drops `.`/`..` first and then resolves; either reading under the real home refuses, a dangling link or unresolvable component refuses. Tests run with the real macOS `TMPDIR` as well as a plain one: `home_rejects_the_real_home_reached_through_a_symlinked_parent`, `home_rejects_a_link_followed_by_dotdot_in_either_reading`, `home_fails_closed_on_a_dangling_link`, `split_segments_table`. 21 mutations, each killed by a named test.
- Not verified locally: Windows (reasoned in the PR body; CI is the only Windows check); Linux; an end-to-end run of a rebuilt audit app (waits for AC6).
