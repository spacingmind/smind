# SQLite WAL mode with synchronous=NORMAL

Implements [ADR-0034](../../decisions/0034-sqlite-wal-synchronous-normal.md) (Accepted 2026-10-19, by the Human). Motivation: #423, 32
concurrent run starts on a slow disk hit `SQLITE_BUSY` after the 5 s `busy_timeout`.

Size: **M (4)** — scope 1 (`internal/store` + docs + one test TestMain + one workflow), PRs 0, risk 2 (every deployed database
switches journal mode on first open), decisions 0 (decided), verification 1 (Windows CI).

## Acceptance Criteria

- AC1. Every connection the pool opens reports `journal_mode=wal`, `synchronous=NORMAL`, `foreign_keys=1`, `busy_timeout=5000`
  (not only the first one).
- AC2. An existing rollback-journal `smind.db` switches to WAL on its first open with its rows intact.
- AC3. 32 concurrent writers and 8 readers under CPU and fsync load, with `SMIND_SQLITE_SYNCHRONOUS` unset, finish with zero
  `SQLITE_BUSY` (the rollback journal loses writes in the same test), also under `-race`.
- AC4. A transaction that reads and then writes does not fail under concurrency (`_txlock=immediate`).
- AC5. `kill -9` of a process mid-write leaves a database that reopens, passes `integrity_check`, and contains every commit
  the process acknowledged; the `-wal` was non-empty (recovery exercised).
- AC6. A clean `Store.Close` leaves an empty or absent `-wal`, and `smind.db` copied alone holds every committed row.
  `Close` twice is still a no-op.
- AC7. `VACUUM INTO` of a running store yields a complete single-file backup.
- AC8. On a network filesystem (Linux/macOS/Windows detectors, injectable in tests) `Open` logs a warning and keeps the
  rollback journal and SQLite's default `synchronous`; in-memory databases are left alone; an unknown filesystem counts as local.
- AC8b. `SMIND_SQLITE_JOURNAL=delete|wal` forces the journal mode (escape hatch for mounts the detector does not
  recognise); `Close` waits at most 250 ms for readers when checkpointing, logs an incomplete checkpoint, and never fails.
- AC9. `SMIND_SQLITE_SYNCHRONOUS` still overrides the NORMAL default and does not turn WAL off. Nothing in tests or CI sets it
  (runs `TestMain`, `windows-full-suite.yml`).
- AC10. Every place that copies, moves or exports the database was audited; `docs/homelab.md` documents backup, restore, Docker
  volume and the network-filesystem limit.
- AC11. `go test ./...`, `-race` on the store/runs/wsapi/learning/quota/accounts/terminal/cmd packages, `task lint`, and a
  Windows run of store/runs/wsapi (and the full suite) are green.

## Test Scenarios

| AC | Test |
|---|---|
| AC1 | `TestOpen_PragmasOnEveryConnection` (8 simultaneously held connections) |
| AC2 | `TestOpen_SwitchesExistingDatabaseToWAL` |
| AC3 | `TestStore_ConcurrentWritersNoBusy` (`SMIND_STORE_LOAD=heavy` for the measurement profile) |
| AC4 | `TestStore_ReadThenWriteTransactionsQueue` |
| AC5 | `TestWAL_KillMidWriteRecovers` (re-executes the test binary as the writer, SIGKILL / TerminateProcess) |
| AC6 | `TestClose_CheckpointsWAL`; the existing `wsapi` store-close tests cover double close |
| AC7 | `TestVacuumInto_ConsistentBackupWhileOpen` |
| AC8 | `TestOpen_NetworkFilesystemKeepsRollbackJournal`, `TestNetworkFS_TempDirIsLocal`, `TestOpen_InMemoryKeepsDefaultJournal` |
| AC8b | `TestOpen_JournalEnvOverrides`, `TestClose_DoesNotWaitOutBusyTimeoutForAReader` |
| AC9 | `TestOpen_SynchronousEnvOverridesNormal` |
| AC10 | audit below; docs review |
| AC11 | Validation |

## Decisions

- Recorded in ADR-0034: WAL + NORMAL via DSN pragmas, `journal_size_limit` 64 MiB, default autocheckpoint, checkpoint on
  `Close`, rollback-journal fallback on network filesystems (warn, do not refuse), `VACUUM INTO` backups, the
  `SMIND_SQLITE_SYNCHRONOUS` override kept.
- `_txlock=immediate` was not in the brief; it came out of measuring (AC4) and is in the ADR as decision 2.
- No `smind backup` command: documented `sqlite3 ... VACUUM INTO` instead. A built-in command is a public CLI addition and
  can follow if wanted.

## Progress

- [x] Baseline measured on the rollback-journal build (1-45 BUSY per 480-800 writes, every profile).
- [x] `internal/store`: DSN pragmas, `_txlock=immediate`, `Close` checkpoint (idempotent), network-filesystem detection
  (`netfs_{linux,darwin,windows,other}.go`).
- [x] Tests above; knob removed from `internal/runs` `TestMain` and `windows-full-suite.yml`.
- [x] ADR-0034, `docs/homelab.md` (new "Backups and the database file" section, Docker note).
- [ ] Windows run of the store/runs/wsapi packages and the full suite.
- [x] Reviewer pass (read-only, `reviewer`-style): no blocking bug. Fixed: `Close` checkpoint bounded to 250 ms and busy
  result logged; Linux magic numbers compared as uint32 (32-bit CIFS/SMB2); `SMIND_SQLITE_JOURNAL` escape hatch; pragma-order
  wording; docs (FUSE mounts, single-file bind mount, no sqlite3 in the image). Not changed: concurrent second `Close` returns
  before the first finishes (no such caller), CPU spinners in the concurrency test stay at NumCPU (measured table).
- [x] Windows run 1 (store/runs/wsapi): runs and wsapi green; store red only on 4 `updated_at > created_at` tests (coarse
  clock + no fsync), fixed with `advanceClock`. The new WAL tests passed on Windows (concurrency 241 ms, 0 BUSY; kill recovery).

## Audit of code that touches the database file

- `internal/store` is the only package that opens SQLite; `internal/learning/devseed` (a dev tool) opens it raw after
  `store.Close`, which checkpoints.
- No code copies, renames or exports `smind.db`: `smind update`, `internal/selfupdate`, the install scripts, the desktop
  app's daemon manager (`desktop/src-tauri/src/daemon_manager.rs`) and the relay/device registries (not SQLite) were checked.
- `desktop/scripts/installer-e2e.ps1` compares the size of `smind.db` before and after install/replace/uninstall
  (`-ge`); the main file only grows, so the check still holds, though with WAL it proves less (rows may sit in the `-wal`).
- Docker: the compose volume and `docker stop` (SIGTERM, graceful close, checkpoint) are fine; SIGKILL after the stop timeout
  is recovered by the next open. A network-backed volume falls back to the rollback journal.

## Validation

- AC1-AC9, AC8b: the tests in the table pass locally (macOS arm64) and under `-race`; the whole `internal/store` suite is
  green on windows-latest (run 39237732452), including the concurrency test (241 ms, 0 BUSY), the kill-recovery test and
  the Close test.
- AC3 before/after: table in `TestStore_ConcurrentWritersNoBusy`'s comment (rollback journal 1-45 BUSY per run, WAL 0 in
  12 of 12 runs), plus the unraced fsync-only profile: 134 BUSY in 47 s before.
- AC10: audit above; `docs/homelab.md` "Backups and the database file".
- AC11: `task build/test/lint` green; `go test -race` green on store, runs, wsapi, learning/..., quota, accounts, terminal,
  cmd/smind; PR CI (ci, windows-terminal) green. Windows full suite on the branch vs develop at the same time
  (runs 39236696196 vs 39236683784): the same failing set on both (pre-existing: `TestDaemonLogCrashHelperProcess`,
  `learning/{executor,export,loads,reflector}`, `TestLearn*`/`TestLearningWiring*` in `cmd/smind`), plus one new failure
  on the branch, `accounts.TestRegistry_EnsureFresh` (an `updated_at` tick tie, fixed). After the fixes, accounts, acp,
  quota, runs, terminal, wsapi and store are all green on Windows (runs 39237313078, 39237732452). The SQLITE_BUSY the
  `SMIND_SQLITE_SYNCHRONOUS=off` knob worked around did not reappear without it.
- Live-only gap: the effect on a real, slow Windows `C:` drive with real agent starts is not measured beyond CI.
