package workspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Branch-list limits for workspace.branches (ADR-0039 section 7).
const (
	defaultBranchLimit = 30
	maxBranchLimit     = 50
	branchesTimeout    = 15 * time.Second
)

// Branch remotes reported in BranchInfo.Remote.
const (
	BranchLocal  = "local"
	BranchOrigin = originRemote
)

// BranchInfo is one start-from candidate. Ahead/Behind count commits against
// the workspace's default base; they are nil (omitted on the wire) when that
// base cannot be resolved locally, never a made-up zero.
type BranchInfo struct {
	Name   string `json:"name"`
	Remote string `json:"remote"`
	Ahead  *int   `json:"ahead,omitempty"`
	Behind *int   `json:"behind,omitempty"`
}

// BranchList is the workspace.branches result.
type BranchList struct {
	DefaultBase string       `json:"defaultBase"`
	Current     string       `json:"current"`
	Branches    []BranchInfo `json:"branches"`
}

// Branches lists workspaceID's local branches and origin/* remote-tracking
// branches, newest commit first, for the New Task "Start from" picker. A
// branch present both locally and on origin is listed once, as local;
// origin/HEAD is hidden. query filters by case-insensitive substring and
// limit caps the page (default 30, max 50). Ahead/Behind are computed only
// for the returned page, against the default base, with local git only: no
// network, no fetch (ADR-0039 section 7).
func (m *Manager) Branches(workspaceID int64, query string, limit int) (BranchList, error) {
	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return BranchList{}, fmt.Errorf("workspace branches: %w", err)
	}
	if limit <= 0 {
		limit = defaultBranchLimit
	}
	if limit > maxBranchLimit {
		limit = maxBranchLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchesTimeout)
	defer cancel()

	// Two sort keys: the last --sort is the primary one, the first breaks
	// ties, so equal commit dates list in a stable name order.
	out, err := runGitOutputEnvCtx(ctx, ws.Path, nil, "for-each-ref", "--sort=refname", "--sort=-committerdate",
		"--format=%(refname)", "refs/heads", "refs/remotes/"+originRemote)
	if err != nil {
		return BranchList{}, fmt.Errorf("workspace branches: %w", err)
	}
	all := parseBranchRefs(out)
	q := strings.ToLower(strings.TrimSpace(query))
	page := make([]branchRef, 0, limit)
	for _, b := range all {
		if q != "" && !strings.Contains(strings.ToLower(b.name), q) {
			continue
		}
		page = append(page, b)
		if len(page) == limit {
			break
		}
	}

	base := workspaceDefaultBase(ws)
	res := BranchList{DefaultBase: base, Branches: make([]BranchInfo, 0, len(page))}
	if cur, err := runGitOutputEnvCtx(ctx, ws.Path, nil, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		res.Current = strings.TrimSpace(cur)
	}
	cmp := baseCompareRef(ws.Path, base)
	for _, b := range page {
		info := BranchInfo{Name: b.name, Remote: b.remote}
		if cmp != "" {
			info.Ahead, info.Behind = aheadBehind(ctx, ws.Path, cmp, b.ref)
		}
		res.Branches = append(res.Branches, info)
	}
	return res, nil
}

// branchCountCalls counts rev-list invocations so a test can prove ahead/behind
// is computed for the returned page only.
var branchCountCalls atomic.Int64

// branchRef is one parsed ref: its short name, where it lives, and its full
// refname (what rev-list is given).
type branchRef struct {
	name, remote, ref string
}

// parseBranchRefs turns for-each-ref's full refnames (already in display
// order) into branches: origin/HEAD is dropped and a local branch hides the
// origin branch of the same name. A name that exists only on origin keeps
// the position of its origin ref.
func parseBranchRefs(out string) []branchRef {
	const (
		headsPrefix  = "refs/heads/"
		originPrefix = "refs/remotes/" + originRemote + "/"
	)
	local := map[string]bool{}
	for _, ref := range strings.Split(out, "\n") {
		if n, ok := strings.CutPrefix(strings.TrimSpace(ref), headsPrefix); ok {
			local[n] = true
		}
	}
	var refs []branchRef
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		switch {
		case strings.HasPrefix(ref, headsPrefix):
			refs = append(refs, branchRef{name: ref[len(headsPrefix):], remote: BranchLocal, ref: ref})
		case strings.HasPrefix(ref, originPrefix):
			n := ref[len(originPrefix):]
			if n == "HEAD" || n == "" || local[n] {
				continue
			}
			refs = append(refs, branchRef{name: n, remote: BranchOrigin, ref: ref})
		}
	}
	return refs
}

// aheadBehind returns how many commits ref is ahead of and behind base, from
// one `rev-list --left-right --count base...ref` (left = only on base =
// behind, right = only on ref = ahead). nil, nil when git cannot say (no
// shared history is still a count; an unreadable ref is not).
func aheadBehind(ctx context.Context, dir, base, ref string) (ahead, behind *int) {
	branchCountCalls.Add(1)
	out, err := runGitOutputEnvCtx(ctx, dir, nil, "rev-list", "--left-right", "--count", base+"..."+ref)
	if err != nil {
		return nil, nil
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return nil, nil
	}
	b, err1 := strconv.Atoi(f[0])
	a, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	return &a, &b
}
