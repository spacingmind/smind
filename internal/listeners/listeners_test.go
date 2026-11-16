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

	got := Read(dir, aliveYes)
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
	got := Read(dir, func(pid int) bool {
		calls[pid]++
		return pid == 9999
	})

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
			if got := Read(dir, aliveYes); len(got) != 0 {
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

func TestReadCapsFileCount(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 300; i++ {
		writeEntry(t, dir, fmt.Sprintf("%03d-p-aaaaaaaa.json", i), Entry{Name: "p", Addr: "127.0.0.1:80", PID: i})
	}
	got := Read(dir, aliveYes)
	if len(got) != 256 {
		t.Fatalf("Read() returned %d entries, want 256", len(got))
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
	if got := Read(dir, aliveYes); got != nil {
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
			for _, e := range Read(dir, aliveYes) {
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

	got := Read(dir, aliveYes)
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
	if long := Read(dir, aliveYes); len(long) != 2 || len(long[1].Name) != maxNameLen {
		t.Fatalf("Read() = %+v, want a second entry with a %d-char name", long, maxNameLen)
	}
}

func TestMissingDirReadsNothing(t *testing.T) {
	got := Read(filepath.Join(t.TempDir(), "nope"), aliveYes)
	if got != nil {
		t.Fatalf("Read() = %+v, want nil", got)
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
