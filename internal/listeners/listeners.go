// Package listeners records the addresses smind processes listen on, one small
// file each, so a page the agent browser loads can be refused access to them.
package listeners

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxDirEntries = 8192    // directory entries Read scans, at most
	maxEntries    = 256     // valid entries Read returns, at most
	maxFileSize   = 4 << 10 // entries larger than this are ignored
	maxNameLen    = 24
)

// Entry is one recorded listener.
type Entry struct {
	Name string `json:"name"` // "daemon", "relay", "grpcweb", "oauth-callback", ...
	Addr string `json:"addr"` // host:port as bound, e.g. "127.0.0.1:4648" or "[::]:4648" or "0.0.0.0:4700"
	PID  int    `json:"pid"`
}

// refreshEvery is how often a registration re-checks (and if needed rewrites)
// its file.
var refreshEvery = 5 * time.Second

// local is the in-memory set of entries this process registered, keyed by a
// registration id (one process registers several entries under one pid).
var local = struct {
	sync.Mutex
	m    map[int]Entry
	next int
}{m: make(map[int]Entry)}

// Local returns a copy of the entries this process registered, sorted by Name
// then Addr.
func Local() []Entry {
	local.Lock()
	defer local.Unlock()
	entries := make([]Entry, 0, len(local.m))
	for _, e := range local.m {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Addr < entries[j].Addr
	})
	return entries
}

// Register writes an entry for this process under dir and returns a function
// that removes it. The entry is also kept in memory (see Local) even when the
// file cannot be written, and one goroutine per registration re-creates the
// directory and file if they go missing or the file's content stops matching
// the entry. Errors are returned (the caller logs them); remove is safe to
// call twice and works whether or not the file was ever written.
func Register(dir, name, addr string) (remove func(), err error) {
	remove = func() {}
	if !validAddr(addr) {
		return remove, fmt.Errorf("listeners: invalid addr %q", addr)
	}
	entry := Entry{Name: sanitiseName(name), Addr: addr, PID: os.Getpid()}
	data, err := json.Marshal(entry)
	if err != nil {
		return remove, fmt.Errorf("listeners: encode entry: %w", err)
	}
	suffix, err := randomSuffix()
	if err != nil {
		return remove, fmt.Errorf("listeners: random suffix: %w", err)
	}
	file := filepath.Join(dir, fmt.Sprintf("%d-%s-%s.json", entry.PID, entry.Name, suffix))

	// In memory the entry exists even if the file below cannot be written: the
	// listener is real regardless of whether publishing it succeeded.
	local.Lock()
	id := local.next
	local.next++
	local.m[id] = entry
	local.Unlock()

	done := make(chan struct{})
	var once sync.Once
	var healMu sync.Mutex
	stopped := false // guarded by healMu
	// interval is captured here so the refresh goroutine never races a test
	// (or caller) reconfiguring refreshEvery.
	interval := refreshEvery
	go func() {
		// A panic in the refresh must never take the daemon down (this package
		// cannot import internal/crash without a cycle): contain it and stop this
		// registration's refresh; the in-memory entry stays.
		defer func() { _ = recover() }()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// healMu spans the whole check-and-rewrite so remove cannot
				// delete the file while a rewrite is in flight.
				if healOnce(&healMu, &stopped, dir, file, data) {
					return
				}
			}
		}
	}()

	remove = func() {
		once.Do(func() {
			healMu.Lock()
			stopped = true
			close(done)
			healMu.Unlock()
			_ = os.Remove(file)
			local.Lock()
			delete(local.m, id)
			local.Unlock()
		})
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return remove, fmt.Errorf("listeners: create dir: %w", err)
	}
	if err := writeEntryFile(dir, file, data); err != nil {
		return remove, err
	}
	return remove, nil
}

// writeEntryFile writes data to dir/filepath.Base(file) atomically: a temp
// file in the same directory, then rename.
func writeEntryFile(dir, file string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "*"+filepath.Base(file)+".tmp")
	if err != nil {
		return fmt.Errorf("listeners: create temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("listeners: write entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("listeners: close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("listeners: publish entry: %w", err)
	}
	return nil
}

// heal re-creates dir and file when the file is missing, its content no
// longer equals the entry, or the directory is gone.
// healPanicHook is a test seam: set, it runs at the start of every heal.
var healPanicHook atomic.Pointer[func()]

func heal(dir, file string, data []byte) {
	if h := healPanicHook.Load(); h != nil {
		(*h)()
	}
	if entryFileMatches(file, data) {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	// Something that is not a regular file may sit where the record belongs (an
	// agent shell can mkdir or mkfifo or symlink there so the rename fails for
	// ever): remove it, without following it, and only this one path inside dir.
	if filepath.Dir(file) == filepath.Clean(dir) {
		if info, err := os.Lstat(file); err == nil && !info.Mode().IsRegular() {
			if info.IsDir() {
				_ = os.RemoveAll(file) // does not follow symlinks inside
			} else {
				_ = os.Remove(file) // a symlink or FIFO or device: the link itself, never its target
			}
		}
	}
	_ = writeEntryFile(dir, file, data)
}

// entryFileMatches reports whether file exists and holds exactly data. It
// opens through openEntryFile so a planted FIFO or symlink cannot block it.
func entryFileMatches(file string, data []byte) bool {
	f, err := openEntryFile(file)
	if err != nil {
		return false
	}
	defer f.Close()
	cur, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil || len(cur) > maxFileSize {
		return false
	}
	return bytes.Equal(cur, data)
}

// Read returns the entries under dir whose process is alive (alive(pid) true).
// It scans at most maxDirEntries directory entries and returns at most
// maxEntries valid ones; files that are unreadable, malformed, oversized or
// name no valid host:port are skipped without counting toward either cap, so
// junk files cannot crowd out real entries. A dead process's file is removed
// best effort. A missing directory is not an error (nil, nil); any other
// ReadDir failure is returned so the caller can fail closed.
func Read(dir string, alive func(pid int) bool) ([]Entry, error) {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(dirents) > maxDirEntries {
		// More entries than any honest run has: someone is flooding the directory to
		// hide real records behind junk. Fail closed rather than read a prefix.
		return nil, fmt.Errorf("listeners: %s holds more than %d entries", dir, maxDirEntries)
	}
	var entries []Entry
	live := make(map[int]bool)
	for _, de := range dirents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		file := filepath.Join(dir, de.Name())
		entry, ok := readEntry(file)
		if !ok {
			continue
		}
		aliveNow, seen := live[entry.PID]
		if !seen {
			aliveNow = alive(entry.PID)
			live[entry.PID] = aliveNow
		}
		if !aliveNow {
			_ = os.Remove(file)
			continue
		}
		if len(entries) == maxEntries {
			// A 257th valid, live entry: more than any honest run has. Fail closed
			// (the sorted prefix could hide real records behind planted ones).
			return nil, fmt.Errorf("listeners: %s holds more than %d live entries", dir, maxEntries)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func readEntry(file string) (Entry, bool) {
	// openEntryFile checks the open descriptor, so a FIFO, a symlink, a
	// directory or a device planted by another process never blocks and is
	// never followed; only a small regular file is read.
	f, err := openEntryFile(file)
	if err != nil {
		return Entry{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil || len(data) > maxFileSize {
		return Entry{}, false
	}
	var entry Entry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&entry); err != nil {
		return Entry{}, false
	}
	if entry.PID <= 0 || len(entry.Name) > maxNameLen || !validAddr(entry.Addr) {
		return Entry{}, false
	}
	return entry, true
}

func validAddr(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return n >= 1 && n <= 65535
}

func sanitiseName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	if len(s) > maxNameLen {
		s = s[:maxNameLen]
	}
	return s
}

func randomSuffix() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// healOnce runs one check-and-rewrite under mu and reports whether the
// registration was stopped. The unlock is deferred, so a panic in heal can never
// leave mu locked (remove takes it).
func healOnce(mu *sync.Mutex, stopped *bool, dir, file string, data []byte) bool {
	mu.Lock()
	defer mu.Unlock()
	if *stopped {
		return true
	}
	heal(dir, file, data)
	return false
}
