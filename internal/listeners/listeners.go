// Package listeners records the addresses smind processes listen on, one small
// file each, so a page the agent browser loads can be refused access to them.
package listeners

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxFiles    = 256     // files Read inspects, at most
	maxFileSize = 4 << 10 // entries larger than this are ignored
	maxNameLen  = 24
)

// Entry is one recorded listener.
type Entry struct {
	Name string `json:"name"` // "daemon", "relay", "grpcweb", "oauth-callback", ...
	Addr string `json:"addr"` // host:port as bound, e.g. "127.0.0.1:4648" or "[::]:4648" or "0.0.0.0:4700"
	PID  int    `json:"pid"`
}

// Register writes an entry for this process under dir and returns a function
// that removes it. It never fails the caller: any error returns a no-op remove
// and the error (the caller logs it).
func Register(dir, name, addr string) (remove func(), err error) {
	remove = func() {}
	if !validAddr(addr) {
		return remove, fmt.Errorf("listeners: invalid addr %q", addr)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return remove, fmt.Errorf("listeners: create dir: %w", err)
	}
	suffix, err := randomSuffix()
	if err != nil {
		return remove, fmt.Errorf("listeners: random suffix: %w", err)
	}
	entry := Entry{Name: sanitiseName(name), Addr: addr, PID: os.Getpid()}
	data, err := json.Marshal(entry)
	if err != nil {
		return remove, fmt.Errorf("listeners: encode entry: %w", err)
	}
	file := filepath.Join(dir, fmt.Sprintf("%d-%s-%s.json", entry.PID, entry.Name, suffix))
	tmp, err := os.CreateTemp(dir, "*"+filepath.Base(file)+".tmp")
	if err != nil {
		return remove, fmt.Errorf("listeners: create temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return remove, fmt.Errorf("listeners: write entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return remove, fmt.Errorf("listeners: close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		_ = os.Remove(tmp.Name())
		return remove, fmt.Errorf("listeners: publish entry: %w", err)
	}
	return func() { _ = os.Remove(file) }, nil
}

// Read returns the entries under dir whose process is alive (alive(pid) true).
// Files that are unreadable, malformed, oversized or name no valid host:port
// are ignored; a dead process's file is removed best effort.
func Read(dir string, alive func(pid int) bool) []Entry {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var entries []Entry
	live := make(map[int]bool)
	files := 0
	for _, de := range dirents {
		if files == maxFiles {
			break
		}
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		files++
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
		entries = append(entries, entry)
	}
	return entries
}

func readEntry(file string) (Entry, bool) {
	// Only a regular file is opened: a FIFO or a device planted by another process
	// must never make a reader block, and a symlink is not followed.
	if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() {
		return Entry{}, false
	}
	f, err := os.Open(file)
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
