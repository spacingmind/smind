package gitstate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// ConflictCache memoises merge-tree verdicts by the pair of tips and records
// whether this git understands `merge-tree --write-tree` (git >= 2.38).
type ConflictCache struct {
	Now func() time.Time

	mu      sync.Mutex
	entries map[conflictKey]conflictEntry
	tooOld  bool
}

type conflictKey struct{ dir, cmpRef, branchRef string }

type conflictEntry struct {
	cmpTip, branchTip string
	result            Conflict
}

// NewConflictCache returns an empty cache using the wall clock.
func NewConflictCache() *ConflictCache { return &ConflictCache{} }

func (c *ConflictCache) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// ComputeConflict reports whether merging branchRef into cmpRef conflicts,
// without a checkout (`git merge-tree --write-tree`, run in the shared repo
// dir). It runs nothing unless the branch is both ahead and behind.
func ComputeConflict(ctx context.Context, run Runner, repoDir, cmpRef, branchRef string, ahead, behind int, cache *ConflictCache) Conflict {
	res := Conflict{State: ConflictNone, Files: []string{}, Against: shortRef(cmpRef)}
	if ahead == 0 || behind == 0 {
		res.CheckedAt = cache.now()
		return res
	}
	if cache == nil {
		cache = &ConflictCache{}
	}

	cache.mu.Lock()
	tooOld := cache.tooOld
	cache.mu.Unlock()
	if tooOld {
		res.State, res.Reason, res.CheckedAt = ConflictUnknown, ReasonGitTooOld, cache.now()
		return res
	}

	cmpTip := revParse(ctx, run, repoDir, cmpRef)
	branchTip := revParse(ctx, run, repoDir, branchRef)
	if cmpTip == "" || branchTip == "" {
		res.State, res.Reason, res.CheckedAt = ConflictUnknown, "ref_unresolved", cache.now()
		return res
	}
	key := conflictKey{repoDir, cmpRef, branchRef}
	cache.mu.Lock()
	if e, ok := cache.entries[key]; ok && e.cmpTip == cmpTip && e.branchTip == branchTip {
		cache.mu.Unlock()
		return e.result
	}
	cache.mu.Unlock()

	out, err := run(ctx, repoDir, "merge-tree", "--write-tree", "--name-only", "--no-messages", cmpTip, branchTip)
	switch {
	case err == nil:
	case exitCode(err) == 1:
		res.State = ConflictConflicts
		res.Files = parseConflictFiles(out)
	case isTooOld(err):
		cache.mu.Lock()
		cache.tooOld = true
		cache.mu.Unlock()
		res.State, res.Reason, res.CheckedAt = ConflictUnknown, ReasonGitTooOld, cache.now()
		return res
	default:
		// Not cached: a transient failure should be retried next time.
		res.State, res.Reason, res.CheckedAt = ConflictUnknown, "merge_tree_failed", cache.now()
		return res
	}
	res.CheckedAt = cache.now()
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = map[conflictKey]conflictEntry{}
	}
	cache.entries[key] = conflictEntry{cmpTip: cmpTip, branchTip: branchTip, result: res}
	cache.mu.Unlock()
	return res
}

// isTooOld recognises git < 2.38 rejecting --write-tree: usage error, exit 129.
func isTooOld(err error) bool {
	var re *RunError
	if !errors.As(err, &re) {
		return false
	}
	return re.ExitCode == 129 || strings.Contains(re.Stderr, "unknown option")
}

// parseConflictFiles reads the file names after the first (tree id) line, up
// to the first blank line.
func parseConflictFiles(out string) []string {
	files := []string{}
	lines := strings.Split(out, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			break
		}
		files = append(files, lines[i])
	}
	return files
}
