# Home directory: `~/.spacingmind` → `~/.smind`

Size: M (5) — scope 2, PRs 0, risk 2, decisions 0, verify 1

Goal (user, 2026-10-11): smind's default home directory becomes `~/.smind`.
`SMIND_HOME` still overrides it. Existing installs are migrated once, without
any risk of losing data.

## Decided semantics

1. Resolution order: `SMIND_HOME` if set, else `~/.smind`.
2. One-time migration, only when `SMIND_HOME` is unset, `~/.smind` does not
   exist and `~/.spacingmind` is a directory:
   - `os.Rename(~/.spacingmind, ~/.smind)` (same filesystem, atomic);
   - leave a compat symlink `~/.spacingmind -> ~/.smind`;
   - log one line about the move.
3. Rename fails (e.g. cross-device): copy nothing, delete nothing; log a
   warning and keep using `~/.spacingmind`.
4. Both directories exist: use `~/.smind`, warn, never merge or touch either.
   (`~/.spacingmind` being the compat symlink to `~/.smind` is the normal
   post-migration state and is silent.)
5. The migration runs only from `internal/config.Load` (the daemon/CLI
   startup path). Two processes starting at once are safe: after a failed
   rename, re-check; "`~/.smind` exists and legacy is gone or points at it"
   means someone else migrated, which is success.
6. Windows has no symlink privilege by default: skip the symlink with a
   warning there (the rename still happens).

## Design

- `config.Dir()` stays a cheap, side-effect-free resolver and is called from
  many packages (store, workspace, relay, auth) that never go through `Load`.
  It returns `SMIND_HOME`, else `~/.smind` if it exists (`Lstat`, so a user's
  own `~/.smind -> elsewhere` symlink counts), else `~/.spacingmind` if that is
  a directory (legacy not yet migrated, or migration failed), else `~/.smind`.
  So a failed rename needs no process state: `Dir()` keeps pointing at legacy.
- `config.Load()` calls `migrateHome(...)` first. The core takes injected
  `rename`, `symlink` and `logf` so tests cover every branch.
- The legacy path is checked with `os.Stat` (must be a directory, a symlink to
  a directory counts), `~/.smind` with `os.Lstat` (anything there blocks).
- A symlink loop is impossible by construction: the compat symlink is only
  created right after a successful rename, when `~/.spacingmind` no longer
  exists; and an existing `~/.smind -> ~/.spacingmind` link counts as
  "`~/.smind` exists", so no migration runs.
- Rename never replaces a non-empty directory, so no rename failure mode can
  destroy data.

## Acceptance criteria

- AC1 Fresh machine: `Dir()` is `$HOME/.smind`; nothing is created by `Dir()`.
- AC2 `SMIND_HOME` wins over everything and no migration happens.
- AC3 Legacy-only: `Load()` moves the whole tree to `~/.smind` (content
  byte-identical), leaves `~/.spacingmind` as a symlink to `~/.smind`, and
  logs one line; a second `Load()` is a silent no-op.
- AC4 Both exist: `Dir()` is `~/.smind`, one warning, neither tree changed.
- AC5 Rename fails: legacy untouched, no `~/.smind`, warning logged, `Dir()`
  still the legacy path, `Load()` returns no error.
- AC6 Concurrent migration: two goroutines, exactly one performs the rename,
  both return without error, final state is the AC3 state.
- AC7 Symlink creation fails (Windows, or anything else): warn, keep going,
  `Dir()` is `~/.smind`.
- AC8 Every reference to the home dir in code, comments, help text, active
  plans and current docs says `~/.smind`; the repo name, `dev.spacingmind.*`,
  `spacingmind.sh/.com` hosts and Go module paths are unchanged.

## Test scenarios (`internal/config`, temp `HOME`, never the real one)

- `TestHome_DefaultIsDotSmind`
- `TestHome_SmindHomeOverride`
- `TestHome_MigratesLegacyDirWithSymlink`
- `TestHome_BothExistUsesSmindAndWarns`
- `TestHome_RenameFailureKeepsLegacy` (injected rename)
- `TestHome_ConcurrentMigrationSafe`

## Audit of references (done before coding)

Changed: `internal/config/config.go`, `cmd/smind/main.go`,
`cmd/smind/relay.go`, `internal/relay/server/run.go`,
`internal/workspace/task_test.go` (comment), `SECURITY.md`, `AGENTS.md`,
`docs/WORKSPACE_PROTOCOL.md`, `docs/ROADMAP.md`,
`docs/plans/active/desktop-macos-app.md`.

Checked and deliberately **unchanged**:

- `desktop/daemon-client/src/daemon_manager/native.rs:257` is a test fixture
  string for the macOS managed daemon, `~/Library/Application Support/
  dev.spacingmind.desktop/managed-daemon/bin/smind`. The desktop uses the app
  data dir for the daemon binary and log; no Rust code reads
  `~/.spacingmind`. No Rust change, so no cargo checks required.
- `desktop/src-tauri/tauri.conf.json:5` is the bundle identifier
  `dev.spacingmind.desktop`, not part of this change.
- `internal/relay/pairing/*_test.go` only mention `relay.spacingmind.sh`
  and `spacingmind.sh/pair` (hosts), not the home directory.
- `docs/plans/completed/`, ADR history and `CHANGELOG.md` are history.

## Known consequences (for the PR body)

- The task worktrees live under `<home>/worktrees/...` and their absolute
  paths are stored in the DB and in git's worktree metadata. After migration
  those paths still resolve through the compat symlink, so **the symlink must
  not be removed**. On Windows without the symlink, those stored paths would
  dangle: flagged as residual risk, not solved here.
- A daemon still running from the old binary keeps working across the rename
  (open fds survive, path lookups follow the symlink), but upgrading with the
  daemon stopped is cleaner.

## Decisions

None open; semantics were decided by the user (above).

## Progress

- [x] Spec written, reference audit done.
- [x] Peer candidate (glm-peer/glm-5.3) wrote `internal/config/home.go`,
  `home_test.go` and the text updates, then hit its 20-turn limit with no
  handoff report. The Peer was stopped, so the Lead took write ownership and
  reviewed the working-tree diff directly.
- [x] Adversarial review: ACCEPTED with two Lead edits (below).
- [x] PR opened against `develop`, not merged.

## Validation

Review (Lead, adversarial):

- **Data loss.** The code never calls remove/copy/merge. `os.Rename` cannot
  replace a non-empty directory, so a lost race or a cross-device failure
  leaves both trees intact. Rename failure: legacy untouched, `Dir()` keeps
  returning it (`TestHome_RenameFailureKeepsLegacy`).
- **Races.** After a failed rename the state is re-checked: `~/.smind` exists
  and legacy is gone or resolves to it means success, silently
  (`TestHome_ConcurrentMigrationSafe`, real goroutines plus a deterministic
  loser). `-race -count=20` clean. Symlink EEXIST is ignored.
- **Symlink loop.** None by construction (link is made right after a
  successful rename, target is the relative `.smind`). A user's own
  `~/.smind -> ~/.spacingmind` counts as "exists" (`Lstat`), so nothing
  migrates and there is no chain.
- **Windows.** No symlink attempt, one warning, rename still happens
  (`TestHome_WindowsSkipsSymlink`). Failure to symlink anywhere warns and
  continues (`TestHome_SymlinkFailureStillMigrates`). **Not tested on real
  Windows.** WSL runs the Linux path inside the distro's own `~`.
- **Lead edits.** (1) After a failed rename, if `~/.smind` appeared meanwhile
  the warning is now the accurate "both exist, using ~/.smind" instead of
  "legacy keeps being used". (2) Added the two Windows/symlink-failure
  tests (AC7).

Evidence:

- `go test -race -count=20 -run TestHome_ ./internal/config/`: ok.
- `task build`, `task test`, `task lint`: exit 0.
- End to end with the real binary and a temp `HOME`
  (`/tmp/smind-home-e2e.*`, port 4793, PID recorded and only it killed,
  directory removed): `serve` logged one "moved" line, `~/.smind` held
  config.yaml, token, marker and smind.db, `~/.spacingmind -> .smind`,
  `/healthz` 200; a restart logged no migration line.
  Never run against the real `~`.
- AC8: `git grep '\.spacingmind'` outside completed plans/CHANGELOG now only
  hits the migration code, its tests, this plan, and the
  `dev.spacingmind.desktop` identifier.
- Spec traceability: 6/6 named tests exist and pass.
- Rust not touched, so no cargo checks (see audit above).

Residual risk: Windows without the symlink leaves stored absolute worktree
paths (`<home>/worktrees/...` in the DB and git worktree metadata) dangling.
Not solved here; needs a user call (junction fallback, or skip migration on
Windows).
