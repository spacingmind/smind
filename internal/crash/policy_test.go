package crash

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// crash-failed-write-counts-toward-rate-limit: a panic loop on an unwritable
// disk must not retry the whole write path on every panic.
func TestCrashFailedWriteCountsTowardRateLimit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(filepath.Join(file, "crashes")) // every write fails
	cfg.MaxPerMinute = 3
	r := New(cfg)
	for i := 0; i < 10; i++ {
		r.Record("fail.site", "x", []byte("pkg.Fn"+string(rune('a'+i))+"()\n"))
	}
	r.mu.Lock()
	attempts, suppressed := len(r.writeTimes), r.suppressed
	r.mu.Unlock()
	if attempts != 3 || suppressed != 7 {
		t.Fatalf("attempts = %d, suppressed = %d; want 3 attempts and 7 suppressed (failed writes count)", attempts, suppressed)
	}
}

// crash-failed-write-dedupes: the same panic after a failed write is not retried.
func TestCrashFailedWriteDedupes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(testConfig(filepath.Join(file, "crashes")))
	r.Record("fail.site", "x", []byte("pkg.Same()\n"))
	r.Record("fail.site", "x", []byte("pkg.Same()\n"))
	r.mu.Lock()
	attempts := len(r.writeTimes)
	r.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1: an identical panic must be deduped even after a failed write", attempts)
	}
}

// crash-record-does-not-queue-behind-slow-disk: while one report is being
// written, another Record returns at once (dropped and counted) instead of
// waiting on the disk.
func TestCrashRecordDoesNotQueueBehindSlowDisk(t *testing.T) {
	r := New(testConfig(filepath.Join(t.TempDir(), "crashes")))
	r.ioMu.Lock() // a write is "in progress" (a hung filesystem)
	done := make(chan string, 1)
	go func() { done <- r.Record("slow.site", "x", []byte("pkg.Fn()\n")) }()
	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("Record = %q while another write was in progress, want it dropped", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked behind a write in progress")
	}
	r.ioMu.Unlock()
	r.mu.Lock()
	suppressed := r.suppressed
	r.mu.Unlock()
	if suppressed != 1 {
		t.Fatalf("suppressed = %d, want 1", suppressed)
	}
}

// crash-contained-panic-is-logged: a swallowed panic is never silent.
func TestCrashContainedPanicIsLogged(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	dir := filepath.Join(t.TempDir(), "crashes")
	setDefault(t, testConfig(dir))
	func() {
		defer Recover("log.site")
		panic("SECRET-LOG-TEXT")
	}()
	out := buf.String()
	if !strings.Contains(out, "recovered a panic in log.site") || strings.Contains(out, "SECRET-LOG-TEXT") {
		t.Fatalf("log = %q: want the site named and the panic value absent", out)
	}
}

// crash-gothen-runs-compensation: GoThen records, then runs the compensating
// action; a panic in the action is swallowed; the process survives.
func TestCrashGoThenRunsCompensation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	setDefault(t, testConfig(dir))
	var ran atomic.Bool
	GoThen("then.site", func() { panic("boom") }, func() { ran.Store(true); panic("compensation also fails") })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (!ran.Load() || len(crashFiles(t, dir)) == 0) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ran.Load() {
		t.Fatal("the compensating action never ran")
	}
	if got := crashFiles(t, dir); len(got) != 1 {
		t.Fatalf("reports = %d, want 1", len(got))
	}
}

// crash-gofatal-records-then-dies: GoFatal's panic still ends the process,
// after the report is on disk. Run in a child process.
func TestCrashGoFatalRecordsThenDies(t *testing.T) {
	if dir := os.Getenv("CRASH_FATAL_CHILD_DIR"); dir != "" {
		Init(Config{Dir: dir, Source: "daemon", Version: "child"})
		GoFatal("fatal.site", func() { panic("fatal") })
		time.Sleep(10 * time.Second) // the panic must kill us first
		os.Exit(0)
	}
	dir := filepath.Join(t.TempDir(), "crashes")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashGoFatalRecordsThenDies$")
	cmd.Env = append(os.Environ(), "CRASH_FATAL_CHILD_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the child survived a GoFatal panic:\n%s", out)
	}
	if got := crashFiles(t, dir); len(got) != 1 {
		t.Fatalf("reports = %d, want 1 (output: %s)", len(got), out)
	}
}
