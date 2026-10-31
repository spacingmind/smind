# Per-task scratch dir and cleanup on close

Size: L (7) — scope 2, PRs 1, risk 2 (deletes files), decisions 0, verify 2 (UI frames + validator).
Human 2026-10-31: "tu clean phan cache do khi close task". A full disk killed runs the same day: every
`HOME=$(mktemp -d)` hermetic run leaked ~1 GB (fresh GOMODCACHE + GOCACHE) under `/var/folders/.../T/tmp.*`.

## Design (Supervisor default)

- Every agent run gets `$SMIND_HOME/scratch/<taskId>/` (0700, created before launch). The daemon puts
  `TMPDIR` (plus `TMP`/`TEMP` on Windows) in the launch env of every harness: claude-native, codex-native,
  ACP (GLM, the five native ACP providers, custom ACP). The entries are PREPENDED, so a `TMPDIR` pinned in
  provider env (typed env) or task env still wins (last entry wins).
- Shared caches (`agents.sharedCaches`, default true): `GOMODCACHE`, `GOCACHE` (from `go env`, resolved once at
  daemon start; skipped silently when `go` is missing), `npm_config_cache` (`~/.npm` if it exists),
  `BUN_INSTALL_CACHE_DIR` (`~/.bun/install/cache` if it exists). Set only when the daemon's own env does not
  already set the name; prepended, so provider/task env wins. Agent runs only.
- Cleanup: archive and delete of a task (and the cascade delete of a space/workspace) remove `scratch/<taskId>/`;
  daemon start and a periodic sweep (10 min) remove numeric dirs whose task is archived, deleted or unknown.
  Non-numeric entries in `scratch/` are never touched. Children have their own dirs.
- Safety (`internal/scratch`): removal never leaves `scratch/<id>`; a symlink at `scratch/<id>` is removed as a
  link, a symlink inside is removed as a link (never followed, never chmod-ed); read-only files are made
  writable first (`chmod u+rwx` on real dirs, `u+w` on real files); the resolved path must be inside the
  resolved scratch root. A task with a running run is never cleaned (the manager's busy hook); the sweep
  retries it later. Logs carry count and bytes only.
- `scratch.usage` / `scratch.clean` RPCs, human-only (refused for run-scoped, plugin and automation callers,
  like provider.setSecret); `smind scratch usage|clean`; Settings -> General "Scratch disk usage" line with a
  "Clean archived scratch" action.

## Acceptance criteria and named tests

- AC1 Launch env carries TMPDIR per harness and respects a pin.
  `TestLaunchEnv_ScratchTmpdir_Claude`, `_Codex`, `_ACP`, `_GLM`, `TestLaunchEnv_PinnedTmpdirWins`.
- AC2 Shared caches: on/off and no override.
  `TestSharedCaches_On`, `TestSharedCaches_Off`, `TestSharedCaches_DoesNotOverrideExisting`,
  `TestConfig_SharedCachesDefaultAndValidate`, `TestResolveSharedCaches_NoGo`.
- AC3 Archive/delete remove only that dir.
  `TestRemove_OnlyTaskDir`, `TestRemove_SymlinkEscapeNotFollowed`, `TestRemove_SymlinkAtTaskDirRemovesLinkOnly`,
  `TestRemove_ReadOnlyFiles`, `TestArchive_RemovesScratch`, `TestDelete_RemovesScratch`.
- AC4 Startup sweep: removes archived/deleted/unknown dirs, keeps live ones, ignores non-numeric.
  `TestSweep_RemovesClosedKeepsLive`, `TestSweep_IgnoresNonNumeric`.
- AC5 Running-run guard. `TestRemove_BusyRefused`, `TestSweep_SkipsBusyThenRetries`.
- AC6 RPCs are human-only. `TestScratchRPC_RefusedForRunScoped`, `TestScratchRPC_RefusedForPlugin`.
- AC7 Usage numbers. `TestUsage_Counts`, `TestScratchRPC_UsageAndClean`.
- AC8 Frames: Settings -> General line + action, light+dark, 1280 and 390.

## Decisions
- Prepend (not detect) for pins: simplest correct under last-wins.
- Periodic sweep instead of a run-finished hook for the busy retry.
- No compat/migration: no installed base.

## Progress
- 2026-10-31 internal/scratch, config knob, store lister, runner env, workspace hook, wsapi + serve wiring, CLI and public docs done (GLM peers, Lead-reviewed).
- Settings -> General line (AC8) NOT done in this PR: GLM 5h window exhausted until ~10:42; follow-up PR.

## Validation
- AC1 TestLaunchEnv_ScratchTmpdir_{Claude,Codex,ACP,GLM}, TestLaunchEnv_PinnedTmpdirWins: pass (fake agents dump env-all).
- AC2 TestSharedCaches_{On,Off,DoesNotOverrideExisting}, TestConfig_SharedCachesDefaultAndValidate, TestResolveSharedCaches_*: pass.
- AC3 TestRemove_*, TestArchive_RemovesScratch, TestDelete_RemovesScratch: pass.
- AC4 TestSweep_*: pass. AC5 TestRemove_BusyRefused, TestSweep_SkipsBusyThenRetries: pass.
- AC6 TestScratchRPC_*: pass. AC7 TestUsage_Counts, TestScratchRPC_UsageAndClean: pass.
- Mutations (each made the named test FAIL, restored): follow-symlink removal (TestRemove_SymlinkAtTaskDirRemovesLinkOnly), symlink-inside target removal (TestRemove_SymlinkEscapeNotFollowed), skip chmod (TestRemove_ReadOnlyFiles), ignore busy (TestRemove_BusyRefused, TestSweep_SkipsBusyThenRetries), sweep ignores keep (TestSweep_RemovesClosedKeepsLive), drop runScoped refusal (TestScratchRPC_RefusedForRunScoped), scratch env after provider env (TestLaunchEnv_PinnedTmpdirWins).
- task lint: clean. task test: all packages pass except cmd/smind TestServe_DaemonLogAndManagedBy, which reads the machine's real service log (environment dependent, unrelated).
- AC8 frames: pending.
