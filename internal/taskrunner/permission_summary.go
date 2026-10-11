package taskrunner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Permission summaries for file writes. A human approving "Write file x"
// otherwise cannot tell a new file from a clobbered one, so the summary
// states the byte size and -- for an existing file -- the old -> new line
// counts and an "overwrites existing file" marker. Summary text only: the
// approval path itself is untouched.

// writeSummaryReadLimit bounds how much of an existing file is read to count
// its lines; beyond it the line count is omitted.
const writeSummaryReadLimit = 4 << 20

// countLines counts lines the way an editor would: a trailing newline does
// not start a new line, and "" has zero lines.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// describeWrite describes writing newContent to path (resolved against cwd
// when relative). existing/oldContent come from the caller when the agent
// reported the old text itself (an ACP diff block); otherwise the file is
// stat'ed under cwd.
func describeWrite(cwd, path, newContent string, oldKnown bool, oldContent string) string {
	size := fmt.Sprintf("%d bytes", len(newContent))
	if oldKnown {
		return fmt.Sprintf("overwrites existing file: %d -> %d lines, %s", countLines(oldContent), countLines(newContent), size)
	}
	if path == "" {
		return size
	}
	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Sprintf("new file, %s, %d lines", size, countLines(newContent))
	}
	if info.Size() > writeSummaryReadLimit {
		return fmt.Sprintf("overwrites existing file (%d bytes): new %s", info.Size(), size)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("overwrites existing file: new %s", size)
	}
	defer f.Close()
	old, err := io.ReadAll(io.LimitReader(f, writeSummaryReadLimit))
	if err != nil {
		return fmt.Sprintf("overwrites existing file: new %s", size)
	}
	return fmt.Sprintf("overwrites existing file: %d -> %d lines, %s", countLines(string(old)), countLines(newContent), size)
}

// writeInputPath returns the file path of a Write-style tool input.
func writeInputPath(in map[string]any) string {
	for _, k := range []string{"file_path", "filePath", "path"} {
		if s, ok := in[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
