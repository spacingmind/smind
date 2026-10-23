package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// The git steps behind task.prPreview, task.createPr and task.updatePr
// (ADR-0039 section 5). The Manager owns everything git does here -- fetch,
// strategy detection, the cherry-pick replay, the push -- and returns
// *forge.PRError for every failure, built from git's stderr alone (scrubbed
// and clipped) so no argv, token or env value ever reaches a caller. Opening
// the pull request itself is the forge provider's job (wsapi).

// PR head strategies.
const (
	// PRStrategyDirect pushes the task's own branch.
	PRStrategyDirect = "direct"
	// PRStrategyCherryPick replays the task's commits onto a clean branch
	// forked from the base and pushes that.
	PRStrategyCherryPick = "cherry-pick"
)

const (
	prFetchTimeout = 20 * time.Second
	prPushTimeout  = 60 * time.Second

	// prCommitLimit bounds the commits a preview lists.
	prCommitLimit = 50
)

// PRCommit is one commit of a pull request preview.
type PRCommit struct {
	SHA     string
	Subject string
}

// PRPreviewInfo is the local half of a Create PR preview.
type PRPreviewInfo struct {
	Base     string // the chosen base branch
	BaseSha  string // tip of origin/<base> (else the local branch); "" when neither exists
	Branch   string // the task's branch
	Head     string // the branch Create PR will push
	Strategy string // PRStrategyDirect or PRStrategyCherryPick
	// Commits are the task's own commits (fork point to branch tip), oldest
	// first, at most prCommitLimit; CommitCount is the full count.
	Commits     []PRCommit
	CommitCount int
	// Diff is the size of the committed change only (fork point to branch
	// tip): what the pull request will contain, not the uncommitted files.
	Diff PRDiffStat
}

// PRDiffStat is a `git diff --shortstat` summary.
type PRDiffStat struct{ Files, Insertions, Deletions int }

// PRPush is the outcome of PushForPR: the branch now on origin and how it
// was made.
type PRPush struct {
	Base     string // the resolved base branch the PR targets
	Branch   string
	Strategy string
}

// prTarget is a task resolved for pull request work.
type prTarget struct {
	taskID    int64
	repoDir   string
	worktree  string
	branch    string
	base      string
	forkPoint string
}

// cherryPickBranchName is the throwaway branch the diverged-base path pushes.
// It is distinct from a PR-start task's own smind/pr-<n>-<slug> branch.
func cherryPickBranchName(taskID int64) string {
	return forge.CherryPickBranchPrefix + strconv.FormatInt(taskID, 10)
}

// prTarget resolves task id and the base branch (baseBranch, else the task's
// base_ref, else the workspace default). It reads the store and validates
// names; it runs no network git.
func (m *Manager) prTarget(id int64, baseBranch string) (prTarget, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return prTarget{}, fmt.Errorf("pull request: %w", err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return prTarget{}, forge.NewPRError(forge.CodeTaskNoWorktree, "")
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return prTarget{}, fmt.Errorf("pull request: %w", err)
	}
	base := strings.TrimSpace(baseBranch)
	if base == "" {
		base = t.BaseRef
	}
	if base == "" {
		base = workspaceDefaultBase(ws)
	}
	if base == "" {
		return prTarget{}, forge.NewPRError(forge.CodeBaseUnknown, "")
	}
	if err := validateRefName(ws.Path, base, true); err != nil {
		return prTarget{}, forge.NewPRError(forge.CodeInvalidBranch, "")
	}
	if t.BaseSha == "" {
		return prTarget{}, forge.NewPRError(forge.CodeBaseUnknown, "")
	}
	return prTarget{
		taskID: id, repoDir: ws.Path, worktree: *t.WorktreePath, branch: *t.Branch,
		base: base, forkPoint: t.BaseSha,
	}, nil
}

// PRPreview computes the local facts of a Create PR preview: base tip, head
// strategy and the task's commits. It never fetches, so the strategy reads
// the last fetched origin/<base>; PushForPR re-decides after a fresh fetch.
func (m *Manager) PRPreview(ctx context.Context, id int64, baseBranch string) (PRPreviewInfo, error) {
	tg, err := m.prTarget(id, baseBranch)
	if err != nil {
		return PRPreviewInfo{}, err
	}
	info := PRPreviewInfo{Base: tg.base, Branch: tg.branch, Head: tg.branch, Strategy: PRStrategyDirect}
	remoteBase := "refs/remotes/" + originRemote + "/" + tg.base
	haveRemote := prRevParse(ctx, tg.worktree, remoteBase) != ""
	switch {
	case haveRemote:
		info.BaseSha = prRevParse(ctx, tg.worktree, remoteBase)
	default:
		info.BaseSha = prRevParse(ctx, tg.worktree, "refs/heads/"+tg.base)
	}
	if haveRemote {
		clean, err := prIsAncestor(ctx, tg.worktree, tg.forkPoint, remoteBase)
		if err != nil {
			return PRPreviewInfo{}, prGitError(ctx, err, "")
		}
		if !clean {
			info.Strategy, info.Head = PRStrategyCherryPick, cherryPickBranchName(id)
		}
	}
	shas, err := prCommits(ctx, tg)
	if err != nil {
		return PRPreviewInfo{}, err
	}
	info.CommitCount = len(shas)
	info.Commits, err = prSubjects(ctx, tg.worktree, shas[:min(len(shas), prCommitLimit)])
	if err != nil {
		return PRPreviewInfo{}, err
	}
	if len(shas) > 0 {
		// Best effort: a failing stat leaves the preview without it.
		if out, _, derr := execGit(ctx, tg.worktree, nil, "diff", "--shortstat", tg.forkPoint, "refs/heads/"+tg.branch); derr == nil {
			if m := shortstatRe.FindStringSubmatch(out); m != nil {
				info.Diff.Files, _ = strconv.Atoi(m[1])
				info.Diff.Insertions, _ = strconv.Atoi(m[2])
				info.Diff.Deletions, _ = strconv.Atoi(m[3])
			}
		}
	}
	return info, nil
}

// PushForPR does the git half of Create PR: fetch origin/<base> (20 s),
// decide the strategy against the fresh base, replay the commits onto a clean
// branch when the base has diverged, and push the head branch (60 s) with a
// plain, never forced, `git push origin <branch>:refs/heads/<branch>`.
//
// The task's fork point is its stored base_sha (ADR-0039). It is not
// necessarily an ancestor of the base branch: if the workspace checkout was
// ahead of the base with unrelated, not-yet-pushed commits when the task's
// worktree was created, pushing the task branch as-is would stuff that
// unrelated history into the PR. So when the fork point is an ancestor of the
// freshly fetched base the task branch is pushed directly; otherwise only the
// task's own commits (fork point..branch) are cherry-picked onto a clean
// smind/pr-task-<id> branch forked from the base, and that is pushed. (A
// PR-start task's own branch is smind/pr-<n>-<slug>, hence the distinct
// "pr-task" name.)
//
// The task needs at least one commit beyond its fork point: uncommitted
// worktree changes are not swept in.
func (m *Manager) PushForPR(ctx context.Context, id int64, baseBranch string) (PRPush, error) {
	tg, err := m.prTarget(id, baseBranch)
	if err != nil {
		return PRPush{}, err
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", tg.base, originRemote, tg.base)
	if err := prGit(ctx, prFetchTimeout, tg.worktree, netGitArgs(ctx, tg.worktree, originRemote, "fetch", "--no-tags", originRemote, refspec)...); err != nil {
		var pe *forge.PRError
		if errors.As(err, &pe) && pe.Code == forge.CodeError && prCouldntFindRef(pe) {
			return PRPush{}, forge.NewPRError(forge.CodeBaseNotFound, prDetail(pe))
		}
		return PRPush{}, err
	}
	remoteBase := "refs/remotes/" + originRemote + "/" + tg.base
	commits, err := prCommits(ctx, tg)
	if err != nil {
		return PRPush{}, err
	}
	if len(commits) == 0 {
		return PRPush{}, forge.NewPRError(forge.CodeNoCommits, "")
	}
	clean, err := prIsAncestor(ctx, tg.worktree, tg.forkPoint, remoteBase)
	if err != nil {
		return PRPush{}, prGitError(ctx, err, "")
	}
	if clean {
		if err := prPush(ctx, tg.worktree, tg.branch, tg.branch); err != nil {
			return PRPush{}, err
		}
		return PRPush{Base: tg.base, Branch: tg.branch, Strategy: PRStrategyDirect}, nil
	}
	head := cherryPickBranchName(id)
	if err := pushCleanBranch(ctx, tg, remoteBase, head, commits); err != nil {
		return PRPush{}, err
	}
	return PRPush{Base: tg.base, Branch: head, Strategy: PRStrategyCherryPick}, nil
}

// PushUpdate does the git half of Update PR: a fast-forward push of the task's
// branch to remoteBranch (the pull request's head ref) on origin, never
// forced. A branch that is behind or diverged is push_rejected.
func (m *Manager) PushUpdate(ctx context.Context, id int64, remoteBranch string) error {
	t, err := m.store.GetTask(id)
	if err != nil {
		return fmt.Errorf("pull request: %w", err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return forge.NewPRError(forge.CodeTaskNoWorktree, "")
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return fmt.Errorf("pull request: %w", err)
	}
	if err := validateRefName(ws.Path, remoteBranch, true); err != nil {
		return forge.NewPRError(forge.CodeInvalidBranch, "")
	}
	return prPush(ctx, *t.WorktreePath, *t.Branch, remoteBranch)
}

// prPush runs the one push command: `git push origin
// refs/heads/<local>:refs/heads/<remote>`. No force flag, no `+` in the
// refspec, so a remote that moved on is rejected rather than overwritten.
func prPush(ctx context.Context, dir, local, remote string) error {
	return prGit(ctx, prPushTimeout, dir, netGitArgs(ctx, dir, originRemote, "push", originRemote, "refs/heads/"+local+":refs/heads/"+remote)...)
}

// pushCleanBranch materializes a throwaway worktree checked out on a fresh
// head branch forked from remoteBase, cherry-picks commits (oldest first)
// onto it and pushes it. The worktree and the local branch it created are
// always removed before returning, success or failure: the branch lives on
// origin from then on. A branch that already existed under that name is not
// smind's to delete, so the add fails and it is left alone.
//
// Each pick keeps the original committer date, so replaying the same commits
// onto the same base yields the same commits: a retry after a failure that
// came later (the forge call) pushes an identical branch instead of a
// rejected one.
func pushCleanBranch(ctx context.Context, tg prTarget, remoteBase, head string, commits []string) (err error) {
	container, err := os.MkdirTemp("", "smind-pr-")
	if err != nil {
		return prGitError(ctx, fmt.Errorf("create throwaway worktree dir: %w", err), "")
	}
	wt := filepath.Join(container, "wt")
	created := false
	defer func() {
		_ = gitWorktreeRemove(tg.repoDir, wt)
		_ = os.RemoveAll(container)
		// The branch is checked out in the throwaway worktree, so it can only
		// be deleted once that is gone.
		if created {
			_ = runGit(tg.repoDir, "branch", "-D", head)
		}
	}()

	if _, stderr, aerr := execGitWorktreeAdd(ctx, tg.repoDir, wt, head, remoteBase); aerr != nil {
		return prGitError(ctx, aerr, stderr)
	}
	created = true
	for _, sha := range commits {
		date, _, derr := execGit(ctx, wt, nil, "show", "-s", "--format=%cI", sha)
		env := append(os.Environ(), "GIT_COMMITTER_DATE="+strings.TrimSpace(date))
		if derr != nil {
			env = nil
		}
		if _, stderr, cerr := execGit(ctx, wt, env, "cherry-pick", sha); cerr != nil {
			_, _, _ = execGit(ctx, wt, nil, "cherry-pick", "--abort")
			if ctx.Err() != nil {
				return prGitError(ctx, cerr, stderr)
			}
			return forge.CherryPickConflict(tg.base, stderr)
		}
	}
	return prPush(ctx, wt, head, head)
}

// execGitWorktreeAdd is `git worktree add -b <branch> <path> <start>` under
// the per-repo lock (see runWorktreeGit), returning stderr for the caller's
// error.
func execGitWorktreeAdd(ctx context.Context, repoPath, worktreePath, branch, startPoint string) (string, string, error) {
	unlock := lockRepoWorktrees(repoPath)
	defer unlock()
	return execGit(ctx, repoPath, nil, "worktree", "add", "-b", branch, worktreePath, startPoint)
}

// prGit runs git with a deadline, mapping a failure to a *forge.PRError.
func prGit(ctx context.Context, timeout time.Duration, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, stderr, err := execGit(ctx, dir, nil, args...)
	if err != nil {
		return prGitError(ctx, err, stderr)
	}
	return nil
}

// prGitError maps a failed git call to its typed error. Only stderr (never
// argv) can reach Detail, and it is scrubbed and clipped there.
func prGitError(ctx context.Context, err error, stderr string) *forge.PRError {
	low := strings.ToLower(stderr)
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		return forge.NewPRError(forge.CodeTimeout, "")
	case containsAny(low, "authentication failed", "could not read username", "could not read password",
		"terminal prompts disabled", "permission denied (publickey", "invalid username or password",
		"the requested url returned error: 401", "the requested url returned error: 403",
		"repository not found", "support for password authentication was removed"):
		return forge.NewPRError(forge.CodePushAuth, stderr)
	case containsAny(low, "non-fast-forward", "[rejected]", "fetch first", "updates were rejected", "[remote rejected]"):
		return forge.NewPRError(forge.CodePushRejected, stderr)
	case containsAny(low, "no such remote", "does not appear to be a git repository"):
		return forge.NewPRError(forge.CodeNoRemote, stderr)
	}
	return forge.NewPRError(forge.CodeError, stderr)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// prCouldntFindRef reports whether a fetch failure was "the base branch isn't
// on origin".
func prCouldntFindRef(e *forge.PRError) bool {
	return strings.Contains(strings.ToLower(prDetail(e)), "couldn't find remote ref")
}

func prDetail(e *forge.PRError) string {
	d, _ := e.Data["detail"].(string)
	return d
}

// prRevParse resolves rev to a commit sha, or "" when it does not exist.
func prRevParse(ctx context.Context, dir, rev string) string {
	out, _, err := execGit(ctx, dir, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return ""
	}
	if sha := strings.TrimSpace(out); shaRE.MatchString(sha) {
		return sha
	}
	return ""
}

// prIsAncestor reports whether ancestor is an ancestor of (or equal to)
// descendant. `git merge-base --is-ancestor` answers with its exit code
// (0 yes, 1 no); any other failure is an error.
func prIsAncestor(ctx context.Context, dir, ancestor, descendant string) (bool, error) {
	_, stderr, err := execGit(ctx, dir, nil, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("%w: %s", err, stderr)
}

// prCommits lists the task's own commits (fork point..branch), oldest first.
func prCommits(ctx context.Context, tg prTarget) ([]string, error) {
	out, stderr, err := execGit(ctx, tg.worktree, nil, "rev-list", "--reverse", tg.forkPoint+"..refs/heads/"+tg.branch)
	if err != nil {
		return nil, prGitError(ctx, err, stderr)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// prSubjects returns shas with their one-line subjects, in the order given.
func prSubjects(ctx context.Context, dir string, shas []string) ([]PRCommit, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	args := append([]string{"show", "-s", "--no-walk=unsorted", "--format=%H%x1f%s"}, shas...)
	out, stderr, err := execGit(ctx, dir, nil, args...)
	if err != nil {
		return nil, prGitError(ctx, err, stderr)
	}
	commits := make([]PRCommit, 0, len(shas))
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		sha, subject, _ := strings.Cut(line, "\x1f")
		if shaRE.MatchString(sha) {
			commits = append(commits, PRCommit{SHA: sha, Subject: subject})
		}
	}
	return commits, nil
}
