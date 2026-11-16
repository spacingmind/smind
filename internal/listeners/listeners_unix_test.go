//go:build !windows

package listeners

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO (or any non-regular file) named like a listener file is ignored and
// never opened, so a reader cannot be made to block.
func TestReadIgnoresNonRegularFilesWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "1-evil-aaaaaaaa.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	link := filepath.Join(dir, "2-link-bbbbbbbb.json")
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte(`{"name":"x","addr":"127.0.0.1:4000","pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	done := make(chan []Entry, 1)
	go func() {
		got, err := Read(dir, func(int) bool { return true })
		if err != nil {
			t.Errorf("Read() error = %v", err)
		}
		done <- got
	}()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Fatalf("a FIFO and a symlink are ignored: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read blocked on a FIFO")
	}
}

// readEntry itself rejects a symlink and a FIFO without blocking.
func TestReadEntryRejectsASymlinkAndAFifoWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "1-evil-aaaaaaaa.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	link := filepath.Join(dir, "2-link-bbbbbbbb.json")
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte(`{"name":"x","addr":"127.0.0.1:4000","pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{fifo, link} {
		okc := make(chan bool, 1)
		go func() {
			_, ok := readEntry(file)
			okc <- ok
		}()
		select {
		case ok := <-okc:
			if ok {
				t.Errorf("readEntry(%s) = ok, want not ok", file)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("readEntry(%s) blocked", file)
		}
	}
}

// Something planted where the record belongs (a directory, a FIFO, a symlink) is
// removed by the refresh and the record comes back; a symlink's TARGET is untouched.
func TestHealRemovesAnObstructionAndRestoresTheRecord(t *testing.T) {
	old := refreshEvery
	refreshEvery = 20 * time.Millisecond
	t.Cleanup(func() { refreshEvery = old })
	for name, plant := range map[string]func(path, target string) error{
		"directory": func(path, _ string) error { return os.MkdirAll(filepath.Join(path, "sub"), 0o700) },
		"fifo":      func(path, _ string) error { return syscall.Mkfifo(path, 0o600) },
		"symlink":   func(path, target string) error { return os.Symlink(target, path) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			rm, err := Register(dir, "relay", "127.0.0.1:43111")
			if err != nil {
				t.Fatal(err)
			}
			defer rm()
			var file string
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				file = filepath.Join(dir, e.Name())
			}
			target := filepath.Join(t.TempDir(), "victim.txt")
			if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := plant(file, target); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if info, err := os.Lstat(file); err == nil && info.Mode().IsRegular() {
					if got, _ := Read(dir, func(int) bool { return true }); len(got) == 1 && got[0].Addr == "127.0.0.1:43111" {
						break
					}
				}
				time.Sleep(10 * time.Millisecond) // sleep-ok: polling
			}
			got, err := Read(dir, func(int) bool { return true })
			if err != nil || len(got) != 1 || got[0].Addr != "127.0.0.1:43111" {
				t.Fatalf("the record must come back after a %s was planted over it: %+v %v", name, got, err)
			}
			if b, err := os.ReadFile(target); err != nil || string(b) != "keep me" {
				t.Fatalf("a symlink's target must never be touched: %q %v", b, err)
			}
		})
	}
}

// A panic in the refresh (a broken filesystem hook) cannot take the process down:
// the registration's entry stays in memory.
func TestRefreshPanicIsContained(t *testing.T) {
	old := refreshEvery
	refreshEvery = 10 * time.Millisecond
	t.Cleanup(func() { refreshEvery = old })
	dir := t.TempDir()
	rm, err := Register(dir, "relay", "127.0.0.1:43112")
	if err != nil {
		t.Fatal(err)
	}
	defer rm()
	// Make the heal path panic: replace the directory with a value that makes MkdirAll's
	// callers fine, then provoke the panic through a nil-deref hook.
	boom := func() { panic("boom") }
	healPanicHook.Store(&boom)
	t.Cleanup(func() { healPanicHook.Store(nil) })
	time.Sleep(100 * time.Millisecond) // sleep-ok: the refresh must run, panic and be contained
	healPanicHook.Store(nil)
	found := false
	for _, e := range Local() {
		if e.Addr == "127.0.0.1:43112" {
			found = true
		}
	}
	if !found {
		t.Fatal("the in-memory entry stays after a contained panic")
	}
}
