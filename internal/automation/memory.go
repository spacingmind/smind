package automation

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	memoryFile     = "MEMORY.md"
	memoryMaxLines = 400
	memoryFence    = "automation-memory"
)

// Memory is the per-automation notes file (ADR-0044 section 8):
// $SMIND_HOME/automations/<id>/MEMORY.md, one file, written only by the
// daemon from the run's final fenced block. There is no path in the
// contract, so a path escape cannot be expressed; a symlink at the
// directory or the file is refused.
type Memory struct {
	// Home is $SMIND_HOME.
	Home string
}

var tokenShapes = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
}

// Dir is automation id's memory directory.
func (m Memory) Dir(id int64) string {
	return filepath.Join(m.Home, "automations", fmt.Sprint(id))
}

// ensureDir creates the directory (0700) and refuses a symlink anywhere on
// the automation's own path (the "automations" directory and the id
// directory).
func (m Memory) ensureDir(id int64) (string, error) {
	root := filepath.Join(m.Home, "automations")
	dir := m.Dir(id)
	for _, p := range []string{root, dir} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return "", fmt.Errorf("memory dir: %w", err)
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return "", fmt.Errorf("memory dir: %w", err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return "", fmt.Errorf("memory dir %s is not a plain directory", p)
		}
	}
	return dir, nil
}

// Read returns the memory (empty if none). A symlinked file is refused. The
// result is capped at max bytes.
func (m Memory) Read(id int64, max int) (string, error) {
	path := filepath.Join(m.Dir(id), memoryFile)
	for _, p := range []string{filepath.Join(m.Home, "automations"), m.Dir(id)} {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("memory path %s is a symlink", p)
		}
	}
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read memory: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("memory file %s is not a regular file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read memory: %w", err)
	}
	if max > 0 && len(b) > max {
		b = b[:max]
	}
	return strings.ToValidUTF8(string(b), ""), nil
}

// Clear deletes the memory file.
func (m Memory) Clear(id int64) error {
	dir, err := m.ensureDir(id)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, memoryFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear memory: %w", err)
	}
	return nil
}

// ExtractBlock returns the content of the LAST ```automation-memory fenced
// block in a run's final message, and whether there was one.
func ExtractBlock(final string) (string, bool) {
	open := "```" + memoryFence
	i := strings.LastIndex(final, open)
	if i < 0 {
		return "", false
	}
	rest := final[i+len(open):]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 || strings.TrimSpace(rest[:nl]) != "" {
		return "", false
	}
	rest = rest[nl+1:]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// MemoryRejected is returned by Write when the block is not stored; the old
// memory is kept.
type MemoryRejected struct{ Reason string }

func (e *MemoryRejected) Error() string { return "memory rejected: " + e.Reason }

// Write validates content and replaces the memory atomically (temp file in the
// same directory, then rename). max is the automation's memoryMaxBytes.
func (m Memory) Write(id int64, content string, max int) error {
	switch {
	case len(content) > max:
		return &MemoryRejected{"too_large"}
	case strings.Count(content, "\n")+1 > memoryMaxLines:
		return &MemoryRejected{"too_many_lines"}
	case !utf8.ValidString(content) || strings.ContainsRune(content, 0):
		return &MemoryRejected{"not_text"}
	}
	for _, re := range tokenShapes {
		if re.MatchString(content) {
			return &MemoryRejected{"token_shaped"}
		}
	}
	dir, err := m.ensureDir(id)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, memoryFile)
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("memory file %s is not a regular file", path)
	}
	tmp, err := os.CreateTemp(dir, ".memory-*")
	if err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(bytes.TrimRight([]byte(content), "\n")); err == nil {
		_, err = tmp.WriteString("\n")
	}
	if err != nil {
		tmp.Close()
		return fmt.Errorf("write memory: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		tmp.Close()
		return fmt.Errorf("write memory: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	return nil
}
