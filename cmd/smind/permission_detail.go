package main

import (
	"fmt"
	"strings"
	"unicode"
)

// permissionDetail mirrors internal/taskrunner.PermissionDetail's wire JSON,
// the optional `detail` on a permission_request. Typed fields only: the MCP
// SDK infers a bad schema for json.RawMessage (see mcpRunEvent).
type permissionDetail struct {
	Kind             string `json:"kind"`
	Path             string `json:"path,omitempty"`
	NewBytes         int    `json:"newBytes,omitempty"`
	NewLines         int    `json:"newLines,omitempty"`
	ExistingBytes    int    `json:"existingBytes,omitempty"`
	ExistingLines    int    `json:"existingLines,omitempty"`
	ReplacesExisting bool   `json:"replacesExisting,omitempty"`
	OutsideWorktree  bool   `json:"outsideWorktree,omitempty"`
	LargeShrink      bool   `json:"largeShrink,omitempty"`
	Preview          string `json:"preview,omitempty"`
	Diff             string `json:"diff,omitempty"`
	Command          string `json:"command,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	Redacted         bool   `json:"redacted,omitempty"`
}

const (
	detailMaxBodyLines    = 40
	detailMaxCommandLines = 10
)

// sanitizeForTerminal drops control characters (ESC, CR, BEL, C1, ...) except
// \n and \t, so agent-controlled text cannot move the cursor or inject ANSI.
func sanitizeForTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func formatKB(n int) string { return fmt.Sprintf("%.1f KB", float64(n)/1024) }

// renderPermissionDetail formats d as indented lines for a permission request.
func renderPermissionDetail(d *permissionDetail) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	if d.Path != "" {
		fmt.Fprintf(&b, "  path: %s\n", sanitizeForTerminal(d.Path))
	}
	if d.Kind == "execute" {
		lines := strings.Split(strings.TrimRight(sanitizeForTerminal(d.Command), "\n"), "\n")
		if len(lines) > detailMaxCommandLines {
			lines = lines[:detailMaxCommandLines]
			lines = append(lines, "...")
		}
		if d.Command != "" {
			fmt.Fprintf(&b, "  command: %s\n", strings.Join(lines, "\n    "))
		}
	}
	switch {
	case d.ExistingBytes > 0 || d.ExistingLines > 0 || d.ReplacesExisting:
		fmt.Fprintf(&b, "  %d -> %d lines, %s -> %s\n", d.ExistingLines, d.NewLines, formatKB(d.ExistingBytes), formatKB(d.NewBytes))
	case d.NewLines > 0 || d.NewBytes > 0:
		fmt.Fprintf(&b, "  new file, %d lines, %s\n", d.NewLines, formatKB(d.NewBytes))
	}
	if d.ReplacesExisting {
		b.WriteString("  WARNING: replaces an existing file\n")
	}
	if d.LargeShrink {
		fmt.Fprintf(&b, "  DANGER: overwrites %d lines with %d -- most of the file will be lost\n", d.ExistingLines, d.NewLines)
	}
	if d.OutsideWorktree {
		b.WriteString("  WARNING: outside the task folder\n")
	}
	if d.Redacted {
		b.WriteString("  existing contents hidden (restricted path)\n")
	}
	body := d.Diff
	if body == "" {
		body = d.Preview
	}
	if body = strings.TrimRight(sanitizeForTerminal(body), "\n"); body != "" {
		lines := strings.Split(body, "\n")
		more := 0
		if len(lines) > detailMaxBodyLines {
			more = len(lines) - detailMaxBodyLines
			lines = lines[:detailMaxBodyLines]
		}
		for _, l := range lines {
			fmt.Fprintf(&b, "    %s\n", l)
		}
		if more > 0 {
			fmt.Fprintf(&b, "    ... (%d more lines; see smind task logs)\n", more)
		}
	}
	if d.Truncated {
		b.WriteString("    (truncated)\n")
	}
	return b.String()
}
