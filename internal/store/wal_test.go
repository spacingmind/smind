package store

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// pragmaInt reads one integer pragma through conn.
func pragmaInt(t *testing.T, conn *sql.Conn, name string) int64 {
	t.Helper()
	var v int64
	if err := conn.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

// TestOpen_PragmasOnEveryConnection holds N connections open at once, so the
// pool has to open N distinct ones, and checks each reports the full pragma
// set: the DSN must configure every connection, not just the first.
func TestOpen_PragmasOnEveryConnection(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	const n = 8
	ctx := context.Background()
	conns := make([]*sql.Conn, 0, n)
	for range n {
		c, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn() error = %v", err)
		}
		conns = append(conns, c)
	}
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})
	for i, c := range conns {
		var mode string
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatalf("conn %d journal_mode: %v", i, err)
		}
		if mode != "wal" {
			t.Errorf("conn %d journal_mode = %q, want wal", i, mode)
		}
		if got := pragmaInt(t, c, "synchronous"); got != 1 { // 1 = NORMAL
			t.Errorf("conn %d synchronous = %d, want 1 (NORMAL)", i, got)
		}
		if got := pragmaInt(t, c, "foreign_keys"); got != 1 {
			t.Errorf("conn %d foreign_keys = %d, want 1", i, got)
		}
		if got := pragmaInt(t, c, "busy_timeout"); got != 5000 {
			t.Errorf("conn %d busy_timeout = %d, want 5000", i, got)
		}
		if got := pragmaInt(t, c, "journal_size_limit"); got != walJournalSizeLimit {
			t.Errorf("conn %d journal_size_limit = %d, want %d", i, got, walJournalSizeLimit)
		}
	}
}

// TestOpen_SynchronousEnvOverridesNormal pins how the #366 test knob composes
// with WAL: the env var, when set, wins over the NORMAL default.
func TestOpen_SynchronousEnvOverridesNormal(t *testing.T) {
	t.Setenv(SynchronousEnv, "off")
	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if got := pragmaString(t, s, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal (the knob must not turn WAL off)", got)
	}
	if got := pragmaString(t, s, "synchronous"); got != "0" {
		t.Errorf("synchronous = %q, want 0 (OFF)", got)
	}
}

// TestOpen_SwitchesExistingDatabaseToWAL: a database last written by the old
// build is in the rollback journal; the first Open of the new build moves it
// to WAL with its rows intact.
func TestOpen_SwitchesExistingDatabaseToWAL(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := s.CreateAccount(Account{Provider: "anthropic", Label: "kept", CredentialType: "oauth", CredentialData: "x"}); err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if _, err := s.db.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatalf("switch back to DELETE: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("precondition: journal_mode = %q (err %v), want delete", mode, err)
	}
	_ = raw.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if got := pragmaString(t, s, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode after first open = %q, want wal", got)
	}
	accts, err := s.ListAccounts()
	if err != nil || len(accts) != 1 || accts[0].Label != "kept" {
		t.Errorf("ListAccounts() = %+v, %v; want the one account written before the switch", accts, err)
	}
}

// TestOpen_InMemoryKeepsDefaultJournal: an in-memory database has no journal
// file to switch, so Open leaves it alone instead of failing.
func TestOpen_InMemoryKeepsDefaultJournal(t *testing.T) {
	s, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("Open(memory) error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if got := pragmaString(t, s, "journal_mode"); got != "memory" {
		t.Errorf("journal_mode = %q, want memory", got)
	}
}

// TestClose_CheckpointsWAL: after a clean Close the -wal file is empty or gone
// and smind.db alone, copied elsewhere, holds every committed row. This is the
// property a backup of a stopped daemon relies on.
func TestClose_CheckpointsWAL(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	const rows = 20
	for i := range rows {
		if _, err := s.CreateAccount(Account{Provider: "anthropic", Label: fmt.Sprintf("a%02d", i), CredentialType: "oauth", CredentialData: "x"}); err != nil {
			t.Fatalf("CreateAccount() error = %v", err)
		}
	}
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("precondition: -wal while open = %v, %v; want a non-empty file", fi, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() != 0 {
		t.Errorf("-wal after Close is %d bytes, want 0 or removed", fi.Size())
	}

	// Copy ONLY smind.db, as a naive backup would.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Open(copyPath)
	if err != nil {
		t.Fatalf("Open(copy) error = %v", err)
	}
	defer c.Close()
	accts, err := c.ListAccounts()
	if err != nil || len(accts) != rows {
		t.Errorf("copy has %d accounts (err %v), want %d", len(accts), err, rows)
	}
}

// TestVacuumInto_ConsistentBackupWhileOpen pins the supported backup method of
// a running daemon: VACUUM INTO writes a complete, single-file snapshot that
// includes what is still only in the -wal.
func TestVacuumInto_ConsistentBackupWhileOpen(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	s := newTestStore(t)
	for i := range 10 {
		if _, err := s.CreateAccount(Account{Provider: "anthropic", Label: fmt.Sprintf("a%02d", i), CredentialType: "oauth", CredentialData: "x"}); err != nil {
			t.Fatalf("CreateAccount() error = %v", err)
		}
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if _, err := s.db.Exec("VACUUM INTO ?", backup); err != nil {
		t.Fatalf("VACUUM INTO: %v", err)
	}
	b, err := Open(backup)
	if err != nil {
		t.Fatalf("Open(backup) error = %v", err)
	}
	defer b.Close()
	accts, err := b.ListAccounts()
	if err != nil || len(accts) != 10 {
		t.Errorf("backup has %d accounts (err %v), want 10", len(accts), err)
	}
}

const crashChildEnv = "SMIND_WAL_CRASH_CHILD_DB"

// TestWALCrashChild is not a test: it is the body of the child process of
// TestWAL_KillMidWriteRecovers, selected by crashChildEnv. It writes from four
// goroutines and prints "ok <label>" after each commit that returned, until
// the parent kills it.
func TestWALCrashChild(t *testing.T) {
	path := os.Getenv(crashChildEnv)
	if path == "" {
		t.Skip("child of TestWAL_KillMidWriteRecovers")
	}
	s, err := Open(path)
	if err != nil {
		fmt.Println("open error:", err)
		os.Exit(2)
	}
	var mu sync.Mutex
	deadline := time.Now().Add(60 * time.Second) // self-cleaning if the parent dies
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; time.Now().Before(deadline); i++ {
				label := fmt.Sprintf("c%d-%d", w, i)
				if _, err := s.CreateAccount(Account{Provider: "anthropic", Label: label, CredentialType: "oauth", CredentialData: strings.Repeat("x", 512)}); err != nil {
					return
				}
				mu.Lock()
				fmt.Println("ok", label)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

// TestWAL_KillMidWriteRecovers kills a writing process with SIGKILL (no
// Close, no checkpoint) and reopens the database: integrity_check must be ok,
// the -wal must have been replayed, and every commit the child acknowledged
// must be present. A process kill is not a power loss, so even
// synchronous=NORMAL may not lose an acknowledged commit here.
func TestWAL_KillMidWriteRecovers(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	path := filepath.Join(t.TempDir(), "smind.db")

	cmd := exec.Command(os.Args[0], "-test.run=^TestWALCrashChild$", "-test.v")
	cmd.Env = append(os.Environ(), crashChildEnv+"="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	acked := map[string]bool{}
	sc := bufio.NewScanner(out)
	const killAfter = 300
	for sc.Scan() {
		line := sc.Text()
		if label, ok := strings.CutPrefix(line, "ok "); ok {
			acked[label] = true
			if len(acked) >= killAfter {
				break
			}
		} else if strings.HasPrefix(line, "open error") {
			t.Fatalf("child: %s", line)
		}
	}
	if len(acked) < killAfter {
		t.Fatalf("child acknowledged only %d commits before exiting (scan err %v)", len(acked), sc.Err())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	_ = cmd.Wait()

	fi, err := os.Stat(path + "-wal")
	if err != nil || fi.Size() == 0 {
		t.Fatalf("-wal after kill = %v, %v; want a non-empty file (else recovery was not exercised)", fi, err)
	}
	t.Logf("killed after %d acknowledged commits with a %d-byte -wal", len(acked), fi.Size())

	s, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after kill -9: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if got := pragmaString(t, s, "integrity_check"); got != "ok" {
		t.Fatalf("integrity_check = %q, want ok", got)
	}
	accts, err := s.ListAccounts()
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	present := map[string]bool{}
	for _, a := range accts {
		present[a.Label] = true
	}
	for label := range acked {
		if !present[label] {
			t.Errorf("acknowledged commit %q is missing after recovery", label)
		}
	}
}

// TestNetworkFS_TempDirIsLocal: the detector must not call an ordinary local
// directory a network filesystem, or every database would lose WAL.
func TestNetworkFS_TempDirIsLocal(t *testing.T) {
	if networkFS(t.TempDir()) {
		t.Errorf("networkFS(%q) = true, want false for the test temp dir", os.TempDir())
	}
	if networkFS(filepath.Join(t.TempDir(), "does", "not", "exist")) {
		t.Error("networkFS(nonexistent dir) = true, want false (unknown counts as local)")
	}
}

// TestOpen_NetworkFilesystemKeepsRollbackJournal: on a (simulated) network
// filesystem Open must not enable WAL, must say so in the log, and must leave
// synchronous at SQLite's default; the database still works.
func TestOpen_NetworkFilesystemKeepsRollbackJournal(t *testing.T) {
	t.Setenv(SynchronousEnv, "")
	old := isNetworkFS
	isNetworkFS = func(string) bool { return true }
	t.Cleanup(func() { isNetworkFS = old })

	var logs strings.Builder
	oldOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOut) })

	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if got := pragmaString(t, s, "journal_mode"); got != "delete" {
		t.Errorf("journal_mode = %q, want delete", got)
	}
	if got := pragmaString(t, s, "synchronous"); got != "2" { // 2 = FULL
		t.Errorf("synchronous = %q, want 2 (FULL, SQLite's default)", got)
	}
	if !strings.Contains(logs.String(), "network filesystem") {
		t.Errorf("log = %q, want a network filesystem warning", logs.String())
	}
	if _, err := s.CreateAccount(Account{Provider: "anthropic", Label: "x", CredentialType: "oauth", CredentialData: "x"}); err != nil {
		t.Errorf("CreateAccount() on the rollback journal error = %v", err)
	}
}
