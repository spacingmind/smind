package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// loadEnv selects the heavy load profile for manual before/after measurement
// (SMIND_STORE_LOAD=heavy). The default profile is lighter so the test stays
// reliable under -race on a shared CI runner.
const loadEnv = "SMIND_STORE_LOAD"

// startDiskLoad makes a slow, contended disk on any machine: one goroutine per
// CPU spins, and fsyncers goroutines write a buffer and fsync it in a loop on
// the same filesystem as the database. Everything stops, and the scratch files
// are removed, when the test ends, so nothing outlives it.
func startDiskLoad(t *testing.T, dir string) {
	t.Helper()
	fsyncers, bufKiB := 2, 64
	if os.Getenv(loadEnv) == "heavy" {
		fsyncers, bufKiB = 4, 256
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x := 0
			for ctx.Err() == nil {
				for range 1 << 16 {
					x++
				}
			}
			_ = x
		}()
	}
	buf := make([]byte, bufKiB<<10)
	for i := range fsyncers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("fsync-load-%d.bin", i)))
			if err != nil {
				return
			}
			defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
			for ctx.Err() == nil {
				if _, err := f.WriteAt(buf, 0); err != nil {
					return
				}
				_ = f.Sync()
			}
		}(i)
	}
	t.Cleanup(func() { cancel(); wg.Wait() })
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// TestStore_ConcurrentWritersNoBusy reproduces #423 at the store level, with
// the SMIND_SQLITE_SYNCHRONOUS test knob unset: 32 writers (the shape of 32
// concurrent run starts) and 8 readers hammer one database under CPU and fsync
// load. With the rollback journal and a per-commit fsync, writers queue behind
// each other past the 5 s busy_timeout and fail with SQLITE_BUSY; WAL with
// synchronous=NORMAL commits without an fsync and readers never block writers,
// so the count must be zero. It logs the counts so before/after is visible.
//
// Measured on macOS (arm64), 3 runs each, 32 writers x 15 inserts, BUSY writes:
//
//	load     race   rollback journal + FULL   WAL + NORMAL
//	default  no     6, 1, 7  (6.3-8.0 s)      0, 0, 0  (0.1-1.1 s)
//	default  yes    7, 8, 10 (10.5-12.4 s)    0, 0, 0  (4.5-6.9 s)
//	heavy    no     7, 16, 26 (10-15 s)       0, 0, 0  (2.2-3.4 s)
//	heavy    yes    44, 45, 32 (21-26 s)      0, 0, 0  (6.3-8.6 s)
func TestStore_ConcurrentWritersNoBusy(t *testing.T) {
	// Not parallel: it clears a process-wide env var and loads every CPU.
	t.Setenv(SynchronousEnv, "")
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	startDiskLoad(t, dir)

	const (
		writers   = 32
		readers   = 8
		perWriter = 15
	)
	var busyWrites, busyReads, otherErrs, written atomic.Int64
	var firstOther atomic.Value
	stopReaders := make(chan struct{})
	var rwg, wwg sync.WaitGroup
	for range readers {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				time.Sleep(2 * time.Millisecond) // pollers, not a spin loop
				if _, err := s.ListAccounts(); isBusy(err) {
					busyReads.Add(1)
				} else if err != nil {
					otherErrs.Add(1)
					firstOther.CompareAndSwap(nil, err)
				}
			}
		}()
	}
	start := time.Now()
	for w := range writers {
		wwg.Add(1)
		go func(w int) {
			defer wwg.Done()
			for i := range perWriter {
				_, err := s.CreateAccount(Account{
					Provider:       "anthropic",
					Label:          fmt.Sprintf("w%02d-%02d", w, i),
					CredentialType: "oauth",
					CredentialData: "refresh-token",
				})
				switch {
				case err == nil:
					written.Add(1)
				case isBusy(err):
					busyWrites.Add(1)
				default:
					otherErrs.Add(1)
					firstOther.CompareAndSwap(nil, err)
				}
			}
		}(w)
	}
	wwg.Wait()
	elapsed := time.Since(start)
	close(stopReaders)
	rwg.Wait()

	t.Logf("journal_mode=%s: %d writers x %d inserts + %d readers in %s: written=%d busyWrites=%d busyReads=%d otherErrors=%d",
		pragmaString(t, s, "journal_mode"), writers, perWriter, readers, elapsed.Round(time.Millisecond),
		written.Load(), busyWrites.Load(), busyReads.Load(), otherErrs.Load())
	if v := firstOther.Load(); v != nil {
		t.Errorf("unexpected non-BUSY error: %v", v)
	}
	if n := busyWrites.Load() + busyReads.Load(); n != 0 {
		t.Errorf("%d operations failed with SQLITE_BUSY (writes %d, reads %d), want 0", n, busyWrites.Load(), busyReads.Load())
	}
	if got := written.Load(); got != writers*perWriter {
		t.Errorf("written = %d, want %d", got, writers*perWriter)
	}
}

func pragmaString(t *testing.T, s *Store, name string) string {
	t.Helper()
	var v string
	if err := s.db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

// TestStore_ReadThenWriteTransactionsQueue pins _txlock=immediate: 16
// goroutines each run a transaction that SELECTs and then INSERTs, the shape of
// several learning-store transactions. As deferred transactions most of them
// failed at once with SQLITE_BUSY (busy_timeout does not cover a read-to-write
// upgrade that lost a race); as immediate ones they queue and all succeed.
func TestStore_ReadThenWriteTransactionsQueue(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	s := newTestStore(t)
	var failed, ok atomic.Int64
	var first atomic.Value
	var wg sync.WaitGroup
	for w := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 30 {
				err := func() error {
					tx, err := s.db.Begin()
					if err != nil {
						return err
					}
					defer func() { _ = tx.Rollback() }()
					var n int
					if err := tx.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
						return err
					}
					if _, err := tx.Exec(`INSERT INTO accounts (provider, label, credential_type, credential_data, created_at, updated_at)
						VALUES ('p', ?, 'o', 'x', datetime('now'), datetime('now'))`, fmt.Sprintf("w%d-%d", w, i)); err != nil {
						return err
					}
					return tx.Commit()
				}()
				if err != nil {
					failed.Add(1)
					first.CompareAndSwap(nil, err)
				} else {
					ok.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Errorf("%d of %d read-then-write transactions failed, first error: %v", failed.Load(), failed.Load()+ok.Load(), first.Load())
	}
}
