// Package commitdraft drafts a commit message from a task worktree's STAGED
// diff with one isolated, no-tools model call (ADR-0043). It never commits,
// stages or writes the index: git is only ever run read-only.
package commitdraft

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// DefaultBudget is the byte budget of the diff text sent to the model.
const DefaultBudget = 24 << 10

// Input is what the prompt is built from.
type Input struct {
	Stat     string   // git diff --cached --stat
	Diff     string   // per-file diff text, secret files by name only, truncated
	Subjects []string // last commit subjects
	// Truncated reports that hunk text was dropped to fit the budget.
	Truncated bool
	// Omitted lists the staged files sent by name only (likely secrets).
	Omitted []string
}

// BuildInput reads the staged state of worktree with read-only git
// (GIT_OPTIONAL_LOCKS=0, never plain `git diff`, nothing that writes the
// index). budget <= 0 means DefaultBudget.
func BuildInput(ctx context.Context, worktree string, budget int) (Input, error) {
	if budget <= 0 {
		budget = DefaultBudget
	}
	raw, err := git(ctx, worktree, "diff", "--cached", "--no-color", "--no-ext-diff")
	if err != nil {
		return Input{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return Input{}, ErrNothingStaged
	}
	stat, err := git(ctx, worktree, "diff", "--cached", "--stat", "--no-color", "--no-ext-diff")
	if err != nil {
		return Input{}, err
	}
	// A repository without commits has no subjects; that is not an error.
	logOut, _ := git(ctx, worktree, "log", "-5", "--format=%s")
	var subjects []string
	for _, l := range strings.Split(logOut, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			subjects = append(subjects, l)
		}
	}
	in := Input{Stat: strings.TrimSpace(stat), Subjects: subjects}
	in.Diff, in.Truncated, in.Omitted = fitDiff(splitFiles(raw), budget)
	return in, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("commitdraft: git %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// fileDiff is one file's section of a unified diff.
type fileDiff struct {
	name   string
	header string // from "diff --git" through the last ---/+++ (or binary) line
	body   string // hunks
}

// splitFiles cuts a `git diff` into per-file sections.
func splitFiles(raw string) []fileDiff {
	var files []fileDiff
	var cur *fileDiff
	var hdr, body strings.Builder
	inBody := false
	flush := func() {
		if cur != nil {
			cur.header, cur.body = hdr.String(), body.String()
			files = append(files, *cur)
		}
		hdr.Reset()
		body.Reset()
		inBody = false
	}
	for _, line := range strings.SplitAfter(raw, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			cur = &fileDiff{name: diffName(line)}
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inBody = true
		}
		if inBody {
			body.WriteString(line)
		} else {
			hdr.WriteString(line)
		}
	}
	flush()
	return files
}

// diffName returns the b/ path of a "diff --git a/x b/x" line.
func diffName(line string) string {
	line = strings.TrimRight(strings.TrimPrefix(line, "diff --git "), "\n")
	if i := strings.LastIndex(line, " b/"); i >= 0 {
		return line[i+3:]
	}
	return line
}

// secretName reports whether base looks like a credentials file.
func secretName(name string) bool {
	b := strings.ToLower(path.Base(name))
	switch {
	case strings.HasPrefix(b, ".env"), strings.HasSuffix(b, ".pem"), strings.HasSuffix(b, ".key"),
		strings.HasPrefix(b, "id_rsa"), strings.HasPrefix(b, "credentials"):
		return true
	}
	return false
}

// fitDiff renders the files within budget bytes of hunk text. Headers are
// always kept; hunk text is dropped, last files first-come first-served, once
// the budget is spent.
func fitDiff(files []fileDiff, budget int) (string, bool, []string) {
	var sb strings.Builder
	var omitted []string
	truncated := false
	remaining := budget
	for _, f := range files {
		sb.WriteString(f.header)
		if secretName(f.name) {
			omitted = append(omitted, f.name)
			fmt.Fprintf(&sb, "[contents of %s omitted: likely secret]\n", f.name)
			continue
		}
		switch {
		case len(f.body) <= remaining:
			sb.WriteString(f.body)
			remaining -= len(f.body)
		default:
			truncated = true
			keep := cutRunes(f.body, remaining)
			sb.WriteString(keep)
			if keep != "" && !strings.HasSuffix(keep, "\n") {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "[... %d bytes omitted ...]\n", len(f.body)-len(keep))
			remaining = 0
		}
	}
	return sb.String(), truncated, omitted
}

// cutRunes returns the longest prefix of s of at most n bytes that ends on a
// rune boundary.
func cutRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
