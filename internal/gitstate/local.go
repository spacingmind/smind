package gitstate

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const originRemote = "origin"

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Input is what Compute needs about one task. The caller (wsapi/service) fills
// it from store.Task and the workspace; gitstate never imports those.
type Input struct {
	TaskID       int64
	WorktreePath string // "" = task has no worktree
	RepoDir      string // the shared repository (workspace checkout)
	Branch       string // "" = task has no branch
	BaseRef      string // branch name, e.g. "develop"
	BaseSha      string // fork commit; "" = unknown
	BranchOwned  bool
	Untrusted    bool
	Forge        string
	PR           *PRInfo // always nil in the local pass
	RunActive    bool
	// DiffStat returns the base→worktree diff size, or the typed base_unknown
	// error. Nil omits Diff.
	DiffStat  func() (files, insertions, deletions int, err error)
	Conflicts *ConflictCache
	Now       func() time.Time
}

// Compute builds the local TaskGit: status, base, commits, origin, diff,
// conflict, merged and actions. Each field whose git call fails is omitted.
// It never touches the network.
func Compute(ctx context.Context, run Runner, in Input) (g TaskGit) {
	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	g = TaskGit{TaskID: in.TaskID, Forge: in.Forge, Untrusted: in.Untrusted, PR: in.PR}
	defer func() {
		g.ComputedAt = now().UTC()
		g.Actions = Actions(g, ActionCtx{RunActive: in.RunActive})
	}()

	g.Base = &Base{Ref: in.BaseRef, Sha: in.BaseSha, Known: in.BaseSha != ""}
	if in.WorktreePath == "" || in.Branch == "" {
		return g // no worktree: nothing local to ask git
	}
	g.Branch = &Branch{Name: in.Branch, Owned: in.BranchOwned}
	repo := in.RepoDir
	if repo == "" {
		repo = in.WorktreePath
	}
	branchRef := "refs/heads/" + in.Branch

	g.Worktree = worktreeStatus(ctx, run, in.WorktreePath)

	cmp := compareRef(ctx, run, repo, in.BaseRef)
	if cmp != "" {
		g.Base.Tip = revParse(ctx, run, repo, cmp)
	}

	var ahead, behind *int
	commits := &Commits{}
	if cmp != "" {
		// `A...B --left-right --count` prints "<only in A>\t<only in B>":
		// the left count is how far B is behind A, the right how far ahead.
		if l, r, ok := leftRight(ctx, run, repo, cmp, branchRef); ok {
			commits.BehindBase, commits.AheadOfBase = &l, &r
			behind, ahead = &l, &r
		}
	}
	if in.BaseSha != "" {
		if out, err := run(ctx, repo, "rev-list", "--count", in.BaseSha+".."+branchRef); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
				commits.SinceFork = &n
			}
		}
	}
	if *commits != (Commits{}) {
		g.Commits = commits
	}

	g.Origin = originStatus(ctx, run, repo, in.Branch, branchRef)

	if in.DiffStat != nil {
		if f, i, d, err := in.DiffStat(); err == nil {
			g.Diff = &Diff{Files: f, Insertions: i, Deletions: d}
		}
	}

	if cmp != "" && ahead != nil && behind != nil {
		c := ComputeConflict(ctx, run, repo, cmp, branchRef, *ahead, *behind, in.Conflicts)
		g.Conflict = &c
	}

	m := ComputeMerged(ctx, run, repo, cmp, branchRef, commitsSinceFork(g.Commits), in.PR)
	g.Merged = &m
	return g
}

func commitsSinceFork(c *Commits) *int {
	if c == nil {
		return nil
	}
	return c.SinceFork
}

// compareRef returns the full ref the base branch is compared against:
// origin/<base> when origin has it, else the local branch, else "".
func compareRef(ctx context.Context, run Runner, dir, base string) string {
	if base == "" {
		return ""
	}
	for _, ref := range []string{"refs/remotes/" + originRemote + "/" + base, "refs/heads/" + base} {
		if revParse(ctx, run, dir, ref) != "" {
			return ref
		}
	}
	return ""
}

func shortRef(ref string) string {
	if s, ok := strings.CutPrefix(ref, "refs/remotes/"); ok {
		return s
	}
	return strings.TrimPrefix(ref, "refs/heads/")
}

// revParse resolves rev to a commit sha, or "" when it does not exist.
func revParse(ctx context.Context, run Runner, dir, rev string) string {
	out, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return ""
	}
	if sha := strings.TrimSpace(out); shaRE.MatchString(sha) {
		return sha
	}
	return ""
}

func leftRight(ctx context.Context, run Runner, dir, a, b string) (left, right int, ok bool) {
	out, err := run(ctx, dir, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, false
	}
	var l, r int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d\t%d", &l, &r); err != nil {
		return 0, 0, false
	}
	return l, r, true
}

func originStatus(ctx context.Context, run Runner, dir, branch, branchRef string) *Origin {
	remoteRef := "refs/remotes/" + originRemote + "/" + branch
	if revParse(ctx, run, dir, remoteRef) == "" {
		// Distinguish "never published" from "git failed": a failing
		// rev-parse of a missing ref and a broken repo look alike, so only
		// claim unpublished when the repo itself answers.
		if revParse(ctx, run, dir, "refs/heads/"+branch) == "" {
			return nil
		}
		return &Origin{Published: false}
	}
	o := &Origin{Upstream: originRemote + "/" + branch, Published: true}
	if l, r, ok := leftRight(ctx, run, dir, remoteRef, branchRef); ok {
		o.Behind, o.Ahead = &l, &r
	}
	return o
}

// worktreeStatus parses one `git status --porcelain=v2 -b`. nil when git fails.
func worktreeStatus(ctx context.Context, run Runner, dir string) *Worktree {
	out, err := run(ctx, dir, "--no-optional-locks", "status", "--porcelain=v2", "-b", "--untracked-files=all")
	if err != nil {
		return nil
	}
	w := &Worktree{}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "? "):
			w.Untracked++
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "u "):
			f := strings.SplitN(line, " ", 3)
			if len(f) < 3 || len(f[1]) != 2 {
				continue
			}
			if f[1][0] != '.' {
				w.Staged++
			}
			if f[1][1] != '.' {
				w.Unstaged++
			}
		}
	}
	w.Dirty = w.Staged+w.Unstaged+w.Untracked > 0
	return w
}
