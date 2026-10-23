package gitstate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	// ForgeCode/ForgeReason are the poller's host state (forge CLI missing or
	// not signed in), passed through to the createPr/updatePr actions.
	ForgeCode   string
	ForgeReason string
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
	noOrigin, notForge := false, false
	midOp := ""
	defer func() {
		g.ComputedAt = now().UTC()
		g.Actions = Actions(g, ActionCtx{
			RunActive: in.RunActive, NoOrigin: noOrigin, OriginNotForge: notForge,
			ForgeCode: in.ForgeCode, ForgeReason: in.ForgeReason, MidOperation: midOp,
		})
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
	switch originKind(ctx, run, repo) {
	case originNone:
		noOrigin = true
	case originNotForge:
		notForge = true
	}

	g.Worktree = worktreeStatus(ctx, run, in.WorktreePath)
	midOp = midOperation(ctx, run, in.WorktreePath)

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

type originState int

const (
	originForge    originState = iota // a URL a forge can host
	originNone                        // no `origin` remote at all
	originNotForge                    // an origin that is a local path, file:// or git:// URL
)

// originKind classifies the repository's `origin` remote for the Create PR
// preconditions (U7). Only an https, ssh or scp-like URL can open a pull
// request; the configured value is read without insteadOf rewriting, which is
// what the forge CLI matches on.
func originKind(ctx context.Context, run Runner, dir string) originState {
	out, err := run(ctx, dir, "config", "--get", "remote."+originRemote+".url")
	if err != nil || strings.TrimSpace(out) == "" {
		return originNone
	}
	if forgeRemoteURL(strings.TrimSpace(out)) {
		return originForge
	}
	return originNotForge
}

// forgeRemoteURL accepts https://, ssh:// and the scp-like host:path form
// (git@github.com:owner/repo.git); a plain path or any other scheme is not a
// forge remote.
func forgeRemoteURL(u string) bool {
	switch {
	case u == "" || strings.HasPrefix(u, "-") || strings.ContainsFunc(u, unicode.IsSpace) || strings.Contains(u, "::"):
		return false // "ext::<command>" and friends are git transports, not forges
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "ssh://"):
		rest := u[strings.Index(u, "://")+3:]
		return rest != "" && !strings.HasPrefix(rest, "/")
	case strings.Contains(u, "://"):
		return false
	}
	// scp-like: [user@]host:path. A one-letter host is a Windows drive.
	i := strings.Index(u, ":")
	if i <= 0 || i == len(u)-1 {
		return false
	}
	host := u[:i]
	if strings.Contains(host, "/") || len(host) == 1 {
		return false // C:\repo or a path containing a colon later on
	}
	return true
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

// midOperation reports "merge" or "rebase" when the worktree is in the middle
// of one: MERGE_HEAD, rebase-merge or rebase-apply exists in its git dir. ""
// when none does or git cannot say.
func midOperation(ctx context.Context, run Runner, dir string) string {
	out, err := run(ctx, dir, "rev-parse", "--git-path", "MERGE_HEAD", "--git-path", "rebase-merge", "--git-path", "rebase-apply")
	if err != nil {
		return ""
	}
	paths := strings.Split(strings.TrimSpace(out), "\n")
	if len(paths) != 3 {
		return ""
	}
	exists := func(p string) bool {
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		_, err := os.Stat(p)
		return err == nil
	}
	switch {
	case exists(paths[1]), exists(paths[2]):
		return "rebase"
	case exists(paths[0]):
		return "merge"
	}
	return ""
}
