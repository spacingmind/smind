package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// remoteFetchTimeout bounds the best-effort `git fetch <remote> <branch>`
// CreateTask performs before resolving a `<remote>/<branch>` baseRef, so a
// hung remote can never hang task creation. A fetch that fails (timeout or
// otherwise) is not fatal: the existing remote-tracking ref is used as-is.
const remoteFetchTimeout = 20 * time.Second

// WithBaseRef makes CreateTask fork the new task branch smind/task-<slug>
// at baseRef's commit (e.g. "origin/develop") instead of the workspace
// checkout's current HEAD. If baseRef looks like <remote>/<branch> for a
// configured remote, a bounded best-effort fetch of that one branch runs
// first; a fetch failure falls back to the existing remote-tracking ref.
// The ref is validated (check-ref-format + rev-parse) before any worktree
// is created.
func WithBaseRef(ref string) CreateTaskOption {
	return func(p *createTaskParams) { p.baseRef = ref }
}

// WithExistingBranch makes CreateTask check out the existing local branch
// name (or origin/<name>, creating a local tracking branch) into the new
// worktree, without creating a new branch. If git refuses (branch already
// checked out elsewhere, unknown branch), CreateTask fails with git's
// reason and leaves no task row and no directory behind.
func WithExistingBranch(name string) CreateTaskOption {
	return func(p *createTaskParams) { p.branch = name }
}

// WithPullRequest makes CreateTask build the task from GitHub pull request
// n's head: `git fetch origin refs/pull/<n>/head`, then branch
// smind/pr-<n>-<slug> from FETCH_HEAD. GitHub-hosted remotes only; a
// non-GitHub remote or missing PR is a clear error with nothing left
// behind. n must be > 0.
func WithPullRequest(n int) CreateTaskOption {
	return func(p *createTaskParams) { p.prNumber = n; p.prSupplied = true }
}

// validateRefName rejects anything that could smuggle an option or a
// malformed ref into a git argv before git is invoked at all: empty
// values, a leading '-' (option position), embedded whitespace or control
// characters, and '..'. Only then does it hand the ref to
// `git check-ref-format` (with --branch for branch names) as a discrete
// argv element, so a value can never be parsed as an option.
func validateRefName(dir, ref string, isBranch bool) error {
	if ref == "" {
		return fmt.Errorf("ref is empty")
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref %q must not begin with '-'", ref)
	}
	if strings.Contains(ref, "..") {
		return fmt.Errorf("ref %q must not contain '..'", ref)
	}
	for _, r := range ref {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("ref %q contains whitespace or control characters", ref)
		}
	}
	args := []string{"check-ref-format"}
	if isBranch {
		args = append(args, "--branch")
	}
	args = append(args, ref)
	if err := runGit(dir, args...); err != nil {
		return fmt.Errorf("ref %q is not a valid git ref: %w", ref, err)
	}
	return nil
}

// gitRefCommitExists reports whether ref resolves to a commit, via
// `git rev-parse --verify --quiet --end-of-options <ref>^{commit}`. The
// --end-of-options separator (plus validateRefName's leading '-' check)
// guarantees the user-supplied ref can only ever be parsed as a rev, never
// as an option.
func gitRefCommitExists(dir, ref string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// splitRemoteBranch splits a "<remote>/<branch>" ref into its parts. ok is
// false for refs with no slash (plain branch/tag names); a branch part
// beginning with '-' is also rejected so the derived refspec handed to
// `git fetch` can never look like an option.
func splitRemoteBranch(ref string) (remote, branch string, ok bool) {
	i := strings.Index(ref, "/")
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	remote, branch = ref[:i], ref[i+1:]
	if strings.HasPrefix(branch, "-") {
		return "", "", false
	}
	return remote, branch, true
}

// remoteConfigured reports whether remote is a configured git remote.
func remoteConfigured(dir, remote string) bool {
	cmd := exec.Command("git", "remote", "get-url", "--", remote)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// fetchRemoteBranchBestEffort runs a bounded `git fetch <remote> <branch>`
// (explicit refspec, no shell) and discards the outcome: refreshing the
// remote-tracking ref is best-effort, and a failure (offline, timeout,
// auth) must fall back to the ref that's already there rather than fail
// task creation.
func fetchRemoteBranchBestEffort(dir, remote, branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteFetchTimeout)
	defer cancel()
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
	cmd := exec.CommandContext(ctx, "git", "fetch", remote, refspec)
	cmd.Dir = dir
	_ = cmd.Run()
}

// fetchPRHead fetches a GitHub pull request's head into FETCH_HEAD. It's a
// package-level var (like runGH) so tests can substitute a local fetch
// without a real GitHub round trip.
var fetchPRHead = func(dir, remote string, n int) error {
	return runGit(dir, "fetch", remote, fmt.Sprintf("refs/pull/%d/head", n))
}

// createTaskFromBaseRef validates baseRef, best-effort refreshes it when
// it names a configured remote's branch, and creates the worktree on a new
// branch forked at that ref's commit.
func createTaskFromBaseRef(repoPath, worktreePath, branch, baseRef string) error {
	if err := validateRefName(repoPath, baseRef, false); err != nil {
		return err
	}
	if remote, b, ok := splitRemoteBranch(baseRef); ok && remoteConfigured(repoPath, remote) {
		fetchRemoteBranchBestEffort(repoPath, remote, b)
	}
	if !gitRefCommitExists(repoPath, baseRef) {
		return fmt.Errorf("base ref %q does not resolve to a commit", baseRef)
	}
	return gitWorktreeAddFrom(repoPath, worktreePath, branch, baseRef)
}

// createTaskFromBranch checks out an existing local branch (or, failing
// that, origin/<branch> as a new local tracking branch) into the worktree
// without creating a new branch. If the branch is already checked out in
// another worktree, git refuses and that error is returned verbatim; no
// directory is left behind either way.
func createTaskFromBranch(repoPath, worktreePath, branch string) error {
	if err := validateRefName(repoPath, branch, true); err != nil {
		return err
	}
	if gitRefCommitExists(repoPath, "refs/heads/"+branch) {
		return runGit(repoPath, "worktree", "add", worktreePath, branch)
	}
	const remote = "origin"
	if gitRefCommitExists(repoPath, "refs/remotes/"+remote+"/"+branch) {
		return runGit(repoPath, "worktree", "add", "--track", "-b", branch, worktreePath, remote+"/"+branch)
	}
	return fmt.Errorf("branch %q not found as a local branch or %s/%s", branch, remote, branch)
}

// createTaskFromPR fetches pull request n's head from origin (GitHub only)
// and creates the worktree on a new branch forked from FETCH_HEAD.
func createTaskFromPR(repoPath, worktreePath, branch string, n int) error {
	const remote = "origin"
	if err := fetchPRHead(repoPath, remote, n); err != nil {
		return fmt.Errorf("fetch pull request %d from %s: %w (tasks from pull requests require a GitHub-hosted %s remote)", n, remote, err, remote)
	}
	return gitWorktreeAddFrom(repoPath, worktreePath, branch, "FETCH_HEAD")
}
