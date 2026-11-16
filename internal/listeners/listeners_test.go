package listeners

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func aliveYes(int) bool { return true }

func writeFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func writeEntry(t *testing.T, dir, name string, entry Entry) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	writeFile(t, dir, name, data)
}

func fileNames(t *testing.T, dir string) []string {
	t.Helper()
	dirents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, de := range dirents {
		names = append(names, de.Name())
	}
	return names
}

func TestRegisterThenRead(t *testing.T) {
	dir := t.TempDir()
	remove, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	defer remove()

	got, err := Read(dir, aliveYes)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Read() returned %d entries, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "daemon" {
		t.Errorf("Name = %q, want %q", e.Name, "daemon")
	}
	if e.Addr != "127.0.0.1:4648" {
		t.Errorf("Addr = %q, want %q", e.Addr, "127.0.0.1:4648")
	}
	if e.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", e.PID, os.Getpid())
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	remove, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if names := fileNames(t, dir); len(names) != 1 {
		t.Fatalf("after Register: %d files, want 1: %v", len(names), names)
	}

	remove()

	if names := fileNames(t, dir); len(names) != 0 {
		t.Fatalf("after remove: %d files, want 0: %v", len(names), names)
	}
	remove() // must be safe to call twice
}

func TestReadIgnoresDeadAndRemovesTheirFile(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, "4242-dead-aaaaaaaa.json", Entry{Name: "dead1", Addr: "127.0.0.1:1", PID: 4242})
	writeEntry(t, dir, "4243-dead-bbbbbbbb.json", Entry{Name: "dead2", Addr: "127.0.0.1:2", PID: 4243})
	writeEntry(t, dir, "9999-live-cccccccc.json", Entry{Name: "live1", Addr: "127.0.0.1:3", PID: 9999})
	writeEntry(t, dir, "9999-live-dddddddd.json", Entry{Name: "live2", Addr: "127.0.0.1:4", PID: 9999})

	calls := make(map[int]int)
	got, err := Read(dir, func(pid int) bool {
		calls[pid]++
		return pid == 9999
	})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("Read() returned %d entries, want 2: %+v", len(got), got)
	}
	for _, e := range got {
		if e.PID != 9999 {
			t.Errorf("entry %+v has pid %d, want 9999", e, e.PID)
		}
	}
	for pid, n := range calls {
		if n != 1 {
			t.Errorf("alive called %d times for pid %d, want 1", n, pid)
		}
	}
	for _, dead := range []string{"4242-dead-aaaaaaaa.json", "4243-dead-bbbbbbbb.json"} {
		if _, err := os.Stat(filepath.Join(dir, dead)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("dead file %s still present (stat err = %v)", dead, err)
		}
	}
	if names := fileNames(t, dir); len(names) != 2 {
		t.Fatalf("after Read: %d files, want the 2 live ones: %v", len(names), names)
	}
}

func TestReadIgnoresMalformed(t *testing.T) {
	padded := append([]byte(`{"name":"big","addr":"127.0.0.1:80","pid":123}`), make([]byte, 5*1024)...)
	cases := []struct {
		name string
		file string
		data []byte
		dir  bool
	}{
		{"garbage json", "1-a-aaaaaaaa.json", []byte("not json at all"), false},
		{"unknown field", "1-a-aaaaaaaa.json", []byte(`{"name":"a","addr":"127.0.0.1:1","pid":123,"extra":true}`), false},
		{"pid 0", "1-a-aaaaaaaa.json", mustJSON(t, Entry{Name: "a", Addr: "127.0.0.1:1", PID: 0}), false},
		{"negative pid", "1-a-aaaaaaaa.json", mustJSON(t, Entry{Name: "a", Addr: "127.0.0.1:1", PID: -5}), false},
		{"bad addr", "1-a-aaaaaaaa.json", mustJSON(t, Entry{Name: "a", Addr: "nonsense", PID: 123}), false},
		{"port 0", "1-a-aaaaaaaa.json", mustJSON(t, Entry{Name: "a", Addr: "127.0.0.1:0", PID: 123}), false},
		{"port 70000", "1-a-aaaaaaaa.json", mustJSON(t, Entry{Name: "a", Addr: "127.0.0.1:70000", PID: 123}), false},
		{"name 25 chars", "1-a-aaaaaaaa.json", mustJSON(t, Entry{Name: strings.Repeat("a", 25), Addr: "127.0.0.1:1", PID: 123}), false},
		{"5 KiB file", "1-a-aaaaaaaa.json", padded, false},
		{"directory named x.json", "x.json", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.dir {
				if err := os.Mkdir(filepath.Join(dir, tc.file), 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			} else {
				writeFile(t, dir, tc.file, tc.data)
			}
			got, err := Read(dir, aliveYes)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("Read() = %+v, want no entries", got)
			}
		})
	}
}

func mustJSON(t *testing.T, e Entry) []byte {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// More live valid entries than any honest run has, or more directory entries than
// the scan bound, is a FLOOD (real records could hide behind planted ones): Read
// fails closed with an error instead of returning a sorted prefix.
func TestReadFailsClosedOnAFlood(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= maxEntries; i++ {
		writeEntry(t, dir, fmt.Sprintf("%03d-p-aaaaaaaa.json", i), Entry{Name: "p", Addr: "127.0.0.1:80", PID: i})
	}
	if got, err := Read(dir, aliveYes); err != nil || len(got) != maxEntries {
		t.Fatalf("exactly the cap is fine: %d %v", len(got), err)
	}
	writeEntry(t, dir, "999-p-aaaaaaaa.json", Entry{Name: "p", Addr: "127.0.0.1:80", PID: 999})
	if _, err := Read(dir, aliveYes); err == nil {
		t.Fatal("one live entry more than the cap must be an error (fail closed)")
	}
	junk := t.TempDir()
	for i := 0; i <= maxDirEntries; i++ {
		if err := os.WriteFile(filepath.Join(junk, fmt.Sprintf("%05d.txt", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Read(junk, aliveYes); err == nil {
		t.Fatal("more directory entries than the scan bound must be an error (fail closed)")
	}
}

// Junk files that sort ahead of the real entries are skipped without counting
// toward the valid-entry cap, so they cannot hide entries this process wrote.
func TestReadFindsRealEntriesBehind300JunkFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 300; i++ {
		writeFile(t, dir, fmt.Sprintf("%04d.json", i), []byte("junk"))
	}
	var removes []func()
	for _, name := range []string{"daemon", "relay", "grpcweb"} {
		remove, err := Register(dir, name, "127.0.0.1:4648")
		if err != nil {
			t.Fatalf("Register(%s) error = %v", name, err)
		}
		removes = append(removes, remove)
	}
	for _, remove := range removes {
		defer remove()
	}

	got, err := Read(dir, aliveYes)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Read() returned %d entries, want 3: %+v", len(got), got)
	}
	for _, name := range []string{"daemon", "grpcweb", "relay"} {
		found := false
		for _, e := range got {
			if e.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("no entry named %q in %+v", name, got)
		}
	}
}

// A directory Read cannot list (here: a regular file) is an error; a missing
// directory is not.
func TestReadReturnsErrorForAnUnreadableDir(t *testing.T) {
	base := t.TempDir()
	notADir := filepath.Join(base, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, err := Read(notADir, aliveYes); err == nil {
		t.Errorf("Read(regular file) = (%+v, nil), want an error", got)
	}
	if got, err := Read(filepath.Join(base, "nope"), aliveYes); err != nil || got != nil {
		t.Errorf("Read(missing dir) = (%+v, %v), want (nil, nil)", got, err)
	}
}

func TestRegisterRejectsABadAddrAndReturnsANoopRemove(t *testing.T) {
	dir := t.TempDir()
	for _, addr := range []string{"nonsense", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000"} {
		remove, err := Register(dir, "daemon", addr)
		if err == nil {
			t.Errorf("Register(%q) err = nil, want error", addr)
		}
		remove() // no-op, must not panic
	}
	got, err := Read(dir, aliveYes)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got != nil {
		t.Errorf("Read() = %+v, want nil", got)
	}
}

func TestFileModeAndDirMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are not meaningful on windows")
	}
	dir := filepath.Join(t.TempDir(), "listeners")
	remove, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	defer remove()

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
	names := fileNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("%d files, want 1: %v", len(names), names)
	}
	fi, err := os.Stat(filepath.Join(dir, names[0]))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}

func TestAtomicNoPartialFileVisible(t *testing.T) {
	dir := t.TempDir()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			got, err := Read(dir, aliveYes)
			if err != nil {
				t.Errorf("Read() error = %v", err)
			}
			for _, e := range got {
				if e.PID != os.Getpid() || !validAddr(e.Addr) || e.Name == "" {
					t.Errorf("Read() returned an incomplete entry: %+v", e)
				}
			}
		}
	}()

	var removes []func()
	for i := 0; i < 20; i++ {
		remove, err := Register(dir, fmt.Sprintf("svc-%d", i), fmt.Sprintf("127.0.0.1:%d", 4700+i))
		if err != nil {
			t.Errorf("Register(svc-%d) error = %v", i, err)
			continue
		}
		removes = append(removes, remove)
	}
	close(done)
	wg.Wait()
	for _, remove := range removes {
		remove()
	}
}

func TestNameSanitised(t *testing.T) {
	dir := t.TempDir()
	remove, err := Register(dir, "a/b ..\x00c", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	defer remove()

	got, err := Read(dir, aliveYes)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "a-b----c" {
		t.Fatalf("Read() = %+v, want one entry named %q", got, "a-b----c")
	}
	names := fileNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("%d files, want 1: %v", len(names), names)
	}
	wantPrefix := fmt.Sprintf("%d-a-b----c-", os.Getpid())
	if !strings.HasPrefix(names[0], wantPrefix) || !strings.HasSuffix(names[0], ".json") {
		t.Errorf("file %q, want prefix %q and .json suffix", names[0], wantPrefix)
	}

	longRemove, err := Register(dir, strings.Repeat("x", 30), "127.0.0.1:4649")
	if err != nil {
		t.Fatalf("Register(long) error = %v", err)
	}
	defer longRemove()
	long, err := Read(dir, aliveYes)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(long) != 2 || len(long[1].Name) != maxNameLen {
		t.Fatalf("Read() = %+v, want a second entry with a %d-char name", long, maxNameLen)
	}
}

func TestMissingDirReadsNothing(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "nope"), aliveYes)
	if err != nil {
		t.Fatalf("Read(missing dir) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("Read() = %+v, want nil", got)
	}
}

func setRefreshEvery(t *testing.T, d time.Duration) {
	t.Helper()
	old := refreshEvery
	refreshEvery = d
	t.Cleanup(func() { refreshEvery = old })
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: polling
	}
	t.Fatal(msg)
}

func TestLocalListsWhatThisProcessRegisteredAndForgetsOnRemove(t *testing.T) {
	dir := t.TempDir()
	removeDaemon, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register(daemon) error = %v", err)
	}
	removeRelay, err := Register(dir, "relay", "127.0.0.1:4649")
	if err != nil {
		t.Fatalf("Register(relay) error = %v", err)
	}

	got := Local()
	if len(got) != 2 {
		t.Fatalf("Local() = %+v, want 2 entries", got)
	}
	if got[0].Name != "daemon" || got[0].Addr != "127.0.0.1:4648" || got[0].PID != os.Getpid() {
		t.Errorf("Local()[0] = %+v, want the daemon entry", got[0])
	}
	if got[1].Name != "relay" || got[1].Addr != "127.0.0.1:4649" {
		t.Errorf("Local()[1] = %+v, want the relay entry", got[1])
	}

	removeDaemon()
	if left := Local(); len(left) != 1 || left[0].Name != "relay" {
		t.Errorf("Local() after remove = %+v, want only relay", left)
	}
	removeRelay()
	if left := Local(); len(left) != 0 {
		t.Errorf("Local() after removes = %+v, want none", left)
	}
}

func TestRegisterSelfHealsADeletedFile(t *testing.T) {
	setRefreshEvery(t, 20*time.Millisecond)
	dir := t.TempDir()
	remove, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	defer remove()

	names := fileNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("%d files, want 1: %v", len(names), names)
	}
	if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
		t.Fatalf("remove entry file: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		got, err := Read(dir, aliveYes)
		return err == nil && len(got) == 1 && got[0].Name == "daemon"
	}, "deleted entry file was not recreated within 5s")
}

func TestRegisterSelfHealsADeletedDirectory(t *testing.T) {
	setRefreshEvery(t, 20*time.Millisecond)
	dir := filepath.Join(t.TempDir(), "listeners")
	remove, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	defer remove()

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		got, err := Read(dir, aliveYes)
		return err == nil && len(got) == 1 && got[0].Name == "daemon"
	}, "deleted listener directory was not recreated within 5s")
}

func TestRemoveStopsTheRefresh(t *testing.T) {
	setRefreshEvery(t, 20*time.Millisecond)
	dir := t.TempDir()
	remove, err := Register(dir, "daemon", "127.0.0.1:4648")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	names := fileNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("%d files, want 1: %v", len(names), names)
	}
	file := filepath.Join(dir, names[0])
	remove()
	remove() // must be safe to call twice
	_ = os.Remove(file)

	// With refreshEvery = 20ms a live refresh goroutine would rewrite the file
	// well within 300ms.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("entry file reappeared after remove (stat err = %v)", err)
		}
		time.Sleep(10 * time.Millisecond) // sleep-ok: polling
	}
}

func TestLocalEntryExistsEvenWhenTheFileCouldNotBeWritten(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	remove, err := Register(filepath.Join(blocker, "listeners"), "daemon", "127.0.0.1:4648")
	if err == nil {
		remove()
		t.Fatal("Register(under a regular file) err = nil, want error")
	}

	got := Local()
	if len(got) != 1 || got[0].Name != "daemon" || got[0].Addr != "127.0.0.1:4648" || got[0].PID != os.Getpid() {
		t.Fatalf("Local() = %+v, want the registered daemon entry", got)
	}

	remove() // must be safe even though the file was never written
	remove() // twice is fine
	if left := Local(); len(left) != 0 {
		t.Errorf("Local() after remove = %+v, want none", left)
	}
}

func TestProcessAliveReal(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Errorf("ProcessAlive(%d) = false, want true", os.Getpid())
	}

	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run child: %v", err)
	}
	if ProcessAlive(cmd.Process.Pid) {
		t.Errorf("ProcessAlive(%d) = true after child exited, want false", cmd.Process.Pid)
	}

	for _, pid := range []int{0, -1} {
		if ProcessAlive(pid) {
			t.Errorf("ProcessAlive(%d) = true, want false", pid)
		}
	}
}
