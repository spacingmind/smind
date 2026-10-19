# 0034: SQLite runs in WAL mode with synchronous=NORMAL

## Status

Accepted (2026-10-19, by the Human). Plan: [`docs/plans/completed/sqlite-wal.md`](../plans/active/sqlite-wal.md).
Reference: ZCode does the same (`refs/zcode/packages/services/src/session/tasksDatabase/startup.ts:70`).

## Decision

1. **Every connection of `smind.db` runs `journal_mode=WAL` and `synchronous=NORMAL`.** They are `_pragma` parameters of
   the DSN (`internal/store.sqliteDSN`), which the modernc driver applies, in order, to each connection the pool opens, not
   just the first. `busy_timeout(5000)` and `foreign_keys(1)` stay, and the driver applies `busy_timeout` first, so that switching an
   existing database waits for a lock instead of failing. `journal_mode` is persistent in the file: a database written by
   an older build switches on its first open by the new one, and an older build can still open it afterwards.
   `wal_autocheckpoint` keeps SQLite's default (1000 pages, about 4 MB). `journal_size_limit` is 64 MiB, so the `-wal`
   file shrinks back after a large transaction instead of staying at its high-water mark.
2. **`_txlock=immediate`: every `db.Begin()` is `BEGIN IMMEDIATE`.** Found while measuring this change: a deferred
   transaction that reads first and then writes (several learning-store transactions do) fails with `SQLITE_BUSY` as soon
   as another writer commits in between, and `busy_timeout` does not apply to that failure. Sixteen goroutines running
   such a transaction lost 277 to 383 of 480 of them, in the rollback journal and in WAL alike. Taking the write lock up
   front makes them queue under `busy_timeout`; the same test then loses none.
3. **A clean shutdown checkpoints.** `Store.Close` runs `PRAGMA wal_checkpoint(TRUNCATE)` before closing, so after a clean
   stop `smind.db` alone holds every committed row and `-wal` is empty. Best effort and bounded: it waits at most 250 ms for
   readers (SQLite reports "busy" as a result row, which is logged, not as an error), so a stuck query cannot hold up
   shutdown; the tail stays in the `-wal` for the next opener. A crash or `kill -9` needs no special handling; the next open replays the `-wal`.
4. **A network filesystem keeps the rollback journal.** WAL needs shared memory and byte-range locks that NFS, SMB and 9p do
   not provide reliably. When the database's directory is on one (Linux `statfs` magic for NFS, SMB, CIFS, AFS, Ceph, 9p;
   macOS `nfs`, `smbfs`, `afpfs`, `webdav`, `ftp`; Windows UNC paths and `DRIVE_REMOTE` volumes), `Open` logs a warning and
   leaves `journal_mode` and `synchronous` at SQLite's defaults, exactly as before this ADR. It does not refuse to start. An
   unknown or unreadable filesystem counts as local, so WAL is the default (FUSE, virtiofs and vboxsf are not recognised: they
   can be network-backed or not). In-memory databases are left alone too. `SMIND_SQLITE_JOURNAL=delete` forces the rollback
   journal on any mount, and `=wal` forces WAL on one the detector flagged.
5. **Backups never copy `smind.db` alone while the daemon runs.** A running database is `smind.db` + `smind.db-wal` +
   `smind.db-shm`; copying the first file misses committed rows and can tear. The supported methods
   ([`docs/homelab.md`](../homelab.md#backups-and-the-database-file)) are `VACUUM INTO` / the sqlite3 `.backup` command
   (consistent, one file, safe while the daemon runs), or stopping the daemon and copying the directory. On restore, delete a
   stale `-wal` and `-shm`. An audit of the repo found no code that copies, moves or exports `smind.db`: `internal/store` is the only
   package that opens SQLite (plus the `devseed` dev tool, which closes the store first), `smind update`, the installers and
   the desktop app's daemon manager leave the data directory in place, and the relay and device registries keep their state
   outside SQLite. So no code needed changing; the Windows installer end-to-end script's "database did not shrink" checks
   still hold, since the main file only grows.
6. **`SMIND_SQLITE_SYNCHRONOUS` stays as an override, nothing sets it.** When set to `off|normal|full|extra` it wins over
   the NORMAL default, as it did over FULL before. The runs test `TestMain` and the Windows full-suite workflow no longer
   set it: WAL commits without a per-commit fsync, which is what the knob worked around (#366).

## Trade-offs accepted

- **Sidecar files.** `smind.db-wal` and `smind.db-shm` appear next to the database while it is open. Tools that move the
  data directory must move all three, or stop the daemon first.
- **Durability of `NORMAL`.** A commit is durable against a crash of the daemon (`kill -9`, panic) but, after a power loss
  or OS crash, the last few commits since the last checkpoint may roll back. The database is never corrupted. The state
  smind stores (run history, tasks, accounts) tolerates losing the last seconds; credentials are re-enterable. If this ever
  matters more than write latency, `SMIND_SQLITE_SYNCHRONOUS=full` restores per-commit durability in WAL mode.
- **Local filesystems only.** See decision 4; a daemon on a network mount runs the slower rollback journal.
- **`BEGIN IMMEDIATE` makes a read-only `db.Begin()` transaction wait for the write lock.** Plain reads (`db.Query`) are
  unaffected and never block writers in WAL; a transaction that reads and then writes is the common case in the store.

## Alternatives considered

- **Keep the rollback journal and raise `busy_timeout`.** Treats the symptom: every commit still costs several fsyncs and a
  journal file created and deleted (1.97 s for 100 inserts on windows-latest, #366), and readers still block writers.
- **WAL with `synchronous=FULL`.** Keeps per-commit durability but not the speed; the daemon's data does not need it.
- **`synchronous=OFF`.** Not durable even against a daemon crash on some filesystems and can corrupt on power loss.
- **Refuse to start on a network filesystem.** Would turn a working homelab setup (a NAS-backed Docker volume) into a hard
  failure on upgrade; the fallback keeps it working at the old speed with a log line saying why.
- **`BEGIN IMMEDIATE` only at the call sites that read first.** Easy to miss one; the DSN covers existing and future
  transactions, and the cost is small.

## Rationale

#423: with the rollback journal and a per-commit fsync, 32 concurrent run starts on a slow disk waited out the 5 s
`busy_timeout` and failed with `SQLITE_BUSY`, a real risk whenever many agents start at once. Measured on windows-latest
(#366): 100 inserts in 1.97 s against 3 to 6 ms with WAL + NORMAL. At the store level (32 writers and 8 readers under CPU
and fsync load, `internal/store.TestStore_ConcurrentWritersNoBusy`, macOS arm64, 3 runs each) the rollback journal lost 1 to
45 of 480 writes to `SQLITE_BUSY` in every profile, WAL lost none in 12 of 12 runs, and finished 2 to 60 times faster; the
exact table is in that test's comment. `TestWAL_KillMidWriteRecovers` kills a writing process with SIGKILL and reopens: the
integrity check passes and every acknowledged commit is present.
