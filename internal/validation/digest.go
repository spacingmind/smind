package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Snapshot is a worktree's HEAD and its `git status --porcelain` text. The seat
// takes one of the lead's worktree before the validator starts and another
// after: a difference means the worktree changed while the validator ran (a
// scope violation). It only reads.
//
// Every git call runs with --no-optional-locks. A plain `git status` refreshes
// and REWRITES .git/index (it takes index.lock) even though it is "read-only",
// which would make this check itself modify the lead's worktree state and race
// the lead's own git commands.
type Snapshot struct {
	Head   string
	Status string
}

// Take reads dir's Snapshot.
func Take(ctx context.Context, dir string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	head, err := gitOut(ctx, dir, "--no-optional-locks", "rev-parse", "HEAD")
	if err != nil {
		return Snapshot{}, err
	}
	status, err := gitOut(ctx, dir, "--no-optional-locks", "status", "--porcelain", "--ignore-submodules=all", "--untracked-files=all")
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Head: strings.TrimSpace(head), Status: status}, nil
}

// IsZero reports whether s was never taken (the post-check is then skipped).
func (s Snapshot) IsZero() bool { return s.Head == "" }

// Hash fingerprints the snapshot.
func (s Snapshot) Hash() string {
	sum := sha256.Sum256([]byte(s.Head + "\n" + s.Status))
	return hex.EncodeToString(sum[:])
}

// Changed lists what differs between s and after: "HEAD" when the commit moved,
// then every path whose porcelain line was added, removed or altered, in sorted
// order. Empty means identical.
func (s Snapshot) Changed(after Snapshot) []string {
	var out []string
	if s.Head != after.Head {
		out = append(out, "HEAD")
	}
	before := statusLines(s.Status)
	now := statusLines(after.Status)
	seen := map[string]bool{}
	for line := range before {
		if !now[line] {
			seen[porcelainPath(line)] = true
		}
	}
	for line := range now {
		if !before[line] {
			seen[porcelainPath(line)] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return append(out, paths...)
}

func statusLines(status string) map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(status, "\n") {
		if strings.TrimSpace(l) != "" {
			m[l] = true
		}
	}
	return m
}

// porcelainPath is the path part of one `XY path` porcelain line (for a rename,
// the new path).
func porcelainPath(line string) string {
	if len(line) > 3 {
		line = line[3:]
	}
	if i := strings.LastIndex(line, " -> "); i >= 0 {
		line = line[i+4:]
	}
	return strings.Trim(line, `"`)
}

// Digest is Take(...).Hash().
func Digest(ctx context.Context, dir string) (string, error) {
	s, err := Take(ctx, dir)
	if err != nil {
		return "", err
	}
	return s.Hash(), nil
}

// IsAncestor reports whether commit is an ancestor of (or equal to) of in dir.
// commit must be a full 40-hex id.
func IsAncestor(ctx context.Context, dir, commit, of string) (bool, error) {
	if !IsSHA(commit) {
		return false, fmt.Errorf("%q is not a full commit id", commit)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := gitCmd(ctx, dir, "merge-base", "--is-ancestor", commit, of)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") || strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	return cmd
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitCmd(ctx, dir, args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// GitHead returns dir's HEAD commit id.
func GitHead(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := gitOut(ctx, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
