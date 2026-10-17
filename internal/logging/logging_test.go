package logging

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDaemonLogFileRotatesAtSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r, err := OpenRotating(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	line := strings.Repeat("x", 39) + "\n" // 40 bytes: two fit in 100, the third rotates
	for i := 0; i < 9; i++ {
		l := fmt.Sprintf("%d", i) + line[1:]
		if _, err := r.Write([]byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	// 9 lines, 2 per file: daemon.log holds the last one (8), .1 holds 6-7, .2 holds 4-5; 0-3 are gone.
	if got := readFile(t, path); !strings.HasPrefix(got, "8") || len(got) != 40 {
		t.Errorf("daemon.log = %q", got)
	}
	if got := readFile(t, path+".1"); !strings.HasPrefix(got, "6") || !strings.Contains(got, "\n7") {
		t.Errorf("daemon.log.1 = %q", got)
	}
	if got := readFile(t, path+".2"); !strings.HasPrefix(got, "4") || !strings.Contains(got, "\n5") {
		t.Errorf("daemon.log.2 = %q", got)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("daemon.log.3 must not exist (maxFiles=3 counts the active file): %v", err)
	}
}

func TestRotatingKeepsOnlyTheActiveFileWhenMaxFilesIsOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r, _ := OpenRotating(path, 10, 1)
	defer r.Close()
	_, _ = r.Write([]byte("aaaaaaaa\n"))
	_, _ = r.Write([]byte("bbbbbbbb\n"))
	if got := readFile(t, path); got != "bbbbbbbb\n" {
		t.Errorf("daemon.log = %q", got)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("no numbered file expected: %v", err)
	}
}

func TestRotatingOversizedSingleWriteIsKeptWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r, _ := OpenRotating(path, 10, 2)
	defer r.Close()
	big := strings.Repeat("y", 50) + "\n"
	if _, err := r.Write([]byte(big)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != big {
		t.Errorf("a line longer than maxSize must not be split or dropped, got %d bytes", len(got))
	}
}

func TestDaemonLogFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	// A pre-existing, too-open file is tightened.
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRotating(path, 20, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	check := func(p string) {
		t.Helper()
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
	check(path)
	_, _ = r.Write([]byte(strings.Repeat("z", 30) + "\n")) // rotates
	check(path)
	check(path + ".1")
}

func TestRotatingWritesAfterCloseFail(t *testing.T) {
	r, _ := OpenRotating(filepath.Join(t.TempDir(), FileName), 100, 2)
	_ = r.Close()
	if _, err := r.Write([]byte("x")); err == nil {
		t.Error("write after Close must fail")
	}
}

func TestRotatingRejectsBadLimits(t *testing.T) {
	if _, err := OpenRotating(filepath.Join(t.TempDir(), FileName), 0, 5); err == nil {
		t.Error("maxSize 0 must be rejected")
	}
	if _, err := OpenRotating(filepath.Join(t.TempDir(), FileName), 10, 0); err == nil {
		t.Error("maxFiles 0 must be rejected")
	}
}

func TestRotatingConcurrentWritersLoseNoWholeLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r, _ := OpenRotating(path, 1<<20, 2)
	defer r.Close()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = r.Write([]byte(fmt.Sprintf("g%d-%03d\n", g, i)))
			}
		}(g)
	}
	wg.Wait()
	if n := strings.Count(readFile(t, path), "\n"); n != 1600 {
		t.Errorf("lines = %d, want 1600", n)
	}
}

func TestBestEffortHidesWriterErrors(t *testing.T) {
	r, _ := OpenRotating(filepath.Join(t.TempDir(), FileName), 100, 2)
	_ = r.Close()
	n, err := BestEffort{W: r}.Write([]byte("hello"))
	if n != 5 || err != nil {
		t.Errorf("BestEffort = (%d, %v), want (5, nil)", n, err)
	}
}

func TestTailFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	var all strings.Builder
	for i := 1; i <= 5000; i++ {
		fmt.Fprintf(&all, "line %d %s\n", i, strings.Repeat("p", 40))
	}
	if err := os.WriteFile(path, []byte(all.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	off, err := TailFile(path, 3, &out)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("line 4998 %[1]s\nline 4999 %[1]s\nline 5000 %[1]s\n", strings.Repeat("p", 40))
	if out.String() != want {
		t.Errorf("tail = %q", out.String())
	}
	if off != int64(all.Len()) {
		t.Errorf("offset = %d, want %d", off, all.Len())
	}

	// Fewer lines than asked: everything, no partial first line.
	short := filepath.Join(t.TempDir(), "short")
	_ = os.WriteFile(short, []byte("a\nb\nc"), 0o600) // no trailing newline
	out.Reset()
	if _, err := TailFile(short, 10, &out); err != nil || out.String() != "a\nb\nc" {
		t.Errorf("short tail = %q, %v", out.String(), err)
	}
	out.Reset()
	if _, err := TailFile(short, 2, &out); err != nil || out.String() != "b\nc" {
		t.Errorf("tail 2 of unterminated file = %q, %v", out.String(), err)
	}

	if _, err := TailFile(filepath.Join(t.TempDir(), "missing"), 5, &out); !os.IsNotExist(err) && !errorsIs(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v", err)
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// syncBuf is a goroutine-safe bytes.Buffer for Follow's output.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFollowSeesAppendsAndSurvivesRotation(t *testing.T) {
	old := followPoll
	followPoll = 10 * time.Millisecond
	defer func() { followPoll = old }()

	path := filepath.Join(t.TempDir(), FileName)
	r, err := OpenRotating(path, 60, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("before\n"))

	var seen bytes.Buffer
	off, err := TailFile(path, 10, &seen)
	if err != nil {
		t.Fatal(err)
	}

	out := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Follow(ctx, path, out, off) }()

	_, _ = r.Write([]byte("one\n"))
	waitFor(t, "one", func() bool { return strings.Contains(out.String(), "one\n") })

	// Force a rotation, then keep writing: nothing may be missed or repeated.
	_, _ = r.Write([]byte(strings.Repeat("filler-", 8) + "\n")) // 57 bytes, rotates
	waitFor(t, "filler", func() bool { return strings.Contains(out.String(), "filler-\n") })
	_, _ = r.Write([]byte("two\n")) // rotates again
	waitFor(t, "two", func() bool { return strings.Contains(out.String(), "two\n") })

	got := out.String()
	for _, want := range []string{"one\n", "filler-filler-filler-filler-filler-filler-filler-filler-\n", "two\n"} {
		if strings.Count(got, want) != 1 {
			t.Errorf("%q seen %d times in %q", want, strings.Count(got, want), got)
		}
	}
	if strings.Contains(got, "before") {
		t.Errorf("Follow replayed what the tail already printed: %q", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Follow returned %v after cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Follow did not stop on cancel")
	}
}

func TestFollowWaitsForAMissingFile(t *testing.T) {
	old := followPoll
	followPoll = 10 * time.Millisecond
	defer func() { followPoll = old }()

	path := filepath.Join(t.TempDir(), FileName)
	out := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Follow(ctx, path, out, 0) }()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "hello", func() bool { return out.String() == "hello\n" })
}

// A rotation that cannot reopen the log (directory briefly gone) must not
// leave the writer dead: the next Write after the directory is back works.
func TestRotatingRecoversWhenTheLogCannotBeReopened(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	r, err := OpenRotating(path, 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("first line\n"))

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("this one rotates and cannot reopen\n")); err == nil {
		t.Fatal("a write with no directory must report an error")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("after recovery\n")); err != nil {
		t.Fatalf("the writer must recover once the directory is back: %v", err)
	}
	if got := readFile(t, path); got != "after recovery\n" {
		t.Errorf("daemon.log = %q", got)
	}
}
