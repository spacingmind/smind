package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// Push and Update from base (ADR-0039 sections 3-6): the git half of
// task.push and task.updateFromBase. Like pr.go, every failure is a
// *forge.PRError built from git's stderr alone (scrubbed and clipped), so no
// argv, token or env value reaches a caller. Nothing here ever forces a push
// or aborts a half-done merge: the human decides.

// Update from base strategies and results.
const (
	SyncStrategyMerge  = "merge"
	SyncStrategyRebase = "rebase"

	SyncUpToDate = "up_to_date"
	SyncMerged   = "merged"
	SyncRebased  = "rebased"
)

const (
	syncLocalTimeout = 60 * time.Second
	// syncConflictFileLimit bounds the conflicted paths a conflict error lists
	// (the same cap as TaskGit.conflict.files).
	syncConflictFileLimit = 100
)

// PushResult is the outcome of Push.
type PushResult struct {
	Branch          string
	Remote          string
	UpstreamCreated bool
	// Pushed counts the commits that were not on the remote-tracking ref
	// (when the branch is unpublished: not on the base compare ref) before the push.
	Pushed int
}

// UpdateResult is the outcome of UpdateFromBase.
type UpdateResult struct {
	Result  string // SyncUpToDate, SyncMerged or SyncRebased
	Commits int    // base commits brought in
}

// Push publishes the task's branch with a plain, never forced
// `git push [-u] <remote> refs/heads/<b>:refs/heads/<dest>`; -u only when the
// branch has no upstream yet. The remote is the branch's pushRemote, else
// remote.pushDefault, else origin. dest is the task's pull request head when it
// has one smind may push to (a same-repo PR-start task), else its own branch.
// A task started from a pull request on a fork is read-only.
func (m *Manager) Push(ctx context.Context, id int64) (PushResult, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return PushResult{}, fmt.Errorf("push: %w", err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return PushResult{}, forge.NewPRError(forge.CodeTaskNoWorktree, "")
	}
	if t.UntrustedSource && t.PRNumber > 0 {
		return PushResult{}, forge.ForkPushReadonly()
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return PushResult{}, fmt.Errorf("push: %w", err)
	}
	dir, branch := *t.WorktreePath, *t.Branch
	if err := validateRefName(ws.Path, branch, true); err != nil {
		return PushResult{}, forge.NewPRError(forge.CodeInvalidBranch, "")
	}
	remote, err := pushRemote(ctx, dir, branch)
	if err != nil {
		return PushResult{}, err
	}
	dest := pushDest(ws.Path, branch, t.PRHeadRef)
	remoteURL := remoteURLOf(ctx, dir, remote)
	if remoteURL == "" {
		return PushResult{}, forge.NewPRError(forge.CodeNoRemote, "")
	}

	remoteRef := "refs/remotes/" + remote + "/" + dest
	branchRef := "refs/heads/" + branch
	var pushed int
	if prRevParse(ctx, dir, remoteRef) != "" {
		pushed = syncCount(ctx, dir, remoteRef+".."+branchRef)
	} else {
		base := t.BaseRef
		if base == "" {
			base = workspaceDefaultBase(ws)
		}
		pushed = syncCount(ctx, dir, syncBaseRange(ctx, dir, base, t.BaseSha)+".."+branchRef)
	}
	res := PushResult{Branch: dest, Remote: remote, Pushed: pushed}

	args := []string{"push"}
	if _, _, err := execGit(ctx, dir, nil, "config", "--get", "branch."+branch+".merge"); err != nil {
		args = append(args, "-u")
		res.UpstreamCreated = true
	}
	args = append(args, remote, pushRefspec(branch, dest))

	ctx, cancel := context.WithTimeout(ctx, prPushTimeout)
	defer cancel()
	if _, stderr, err := execGit(ctx, dir, nil, netGitArgs(ctx, dir, remote, args...)...); err != nil {
		pe := pushError(ctx, err, stderr, remoteURL)
		if remote == originRemote && dest == branch {
			pe = m.rebasedPushError(ctx, id, pe, stderr)
		}
		return PushResult{}, pe
	}
	m.setRebasedOrigin(id, "")
	return res, nil
}

// pushRefspec is the one refspec Push sends: a plain ref to ref, never forced
// (no leading '+').
func pushRefspec(branch, dest string) string {
	return "refs/heads/" + branch + ":refs/heads/" + dest
}

// pushDest is the branch Push updates on the remote: the task's pull request
// head when that is a branch smind may push to (not its throwaway rebased
// copy, and a valid name), else the task's own branch.
func pushDest(repoDir, branch, prHeadRef string) string {
	if prHeadRef == "" || strings.HasPrefix(prHeadRef, forge.CherryPickBranchPrefix) || validateRefName(repoDir, prHeadRef, true) != nil {
		return branch
	}
	return prHeadRef
}

// pushRemote is the remote a push of branch goes to, as `git push` would pick
// it: branch.<b>.pushRemote, else remote.pushDefault, else origin. The name
// must be a plain configured remote (no leading '-', no whitespace), never a
// URL.
func pushRemote(ctx context.Context, dir, branch string) (string, error) {
	remote := originRemote
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault"} {
		if out, _, err := execGit(ctx, dir, nil, "config", "--get", key); err == nil {
			if v := strings.TrimSpace(out); v != "" {
				remote = v
				break
			}
		}
	}
	if remote == originRemote {
		return remote, nil
	}
	if !validRemoteName(remote) {
		return "", forge.NewPRError(forge.CodeNoRemote, "the configured push remote has an invalid name")
	}
	out, _, err := execGit(ctx, dir, nil, "remote")
	if err != nil {
		return "", forge.NewPRError(forge.CodeNoRemote, "")
	}
	for _, name := range strings.Fields(out) {
		if name == remote {
			return remote, nil
		}
	}
	return "", forge.NewPRError(forge.CodeNoRemote, "the configured push remote isn't a remote of this repository")
}

// validRemoteName accepts what git allows in a remote name that is safe to
// hand to `git push` as a positional: nothing that starts with '-', and no
// whitespace, control or glob characters.
func validRemoteName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == '/':
		default:
			return false
		}
	}
	return true
}

// syncBaseRange is the revision "everything the base already has" that an
// unpublished branch is counted against: origin/<base>, else the local base
// branch, else the task's recorded fork commit.
func syncBaseRange(ctx context.Context, dir, base, forkSha string) string {
	if base != "" {
		for _, ref := range []string{"refs/remotes/" + originRemote + "/" + base, "refs/heads/" + base} {
			if prRevParse(ctx, dir, ref) != "" {
				return ref
			}
		}
	}
	return forkSha
}

// pushError maps a failed push. An origin that moved on is non_fast_forward
// (the typed code the card offers its next step for); a hook or protected
// branch decline stays push_rejected; an auth failure on an https origin is
// forge_unauthenticated (sign in on the daemon host), on ssh push_auth.
func pushError(ctx context.Context, err error, stderr, remoteURL string) *forge.PRError {
	pe := prGitError(ctx, err, stderr)
	low := strings.ToLower(stderr)
	switch pe.Code {
	case forge.CodePushRejected:
		if containsAny(low, "non-fast-forward", "fetch first", "updates were rejected") && !strings.Contains(low, "[remote rejected]") {
			return forge.NewPRError(forge.CodeNonFastForward, stderr)
		}
	case forge.CodePushAuth:
		if strings.HasPrefix(strings.ToLower(remoteURL), "https://") {
			return forge.NewPRError(forge.CodeUnauthenticated, stderr)
		}
	}
	return pe
}

// RebasedOrigin is the sha origin/<branch> pointed at when smind last rebased
// the task's published branch, "" when it has not (or since a push or merge).
func (m *Manager) RebasedOrigin(id int64) string {
	m.rebasedMu.Lock()
	defer m.rebasedMu.Unlock()
	return m.rebased[id]
}

func (m *Manager) setRebasedOrigin(id int64, sha string) {
	m.rebasedMu.Lock()
	defer m.rebasedMu.Unlock()
	if sha == "" {
		delete(m.rebased, id)
		return
	}
	if m.rebased == nil {
		m.rebased = map[int64]string{}
	}
	m.rebased[id] = sha
}

// rebasedPushError swaps a non_fast_forward refusal for the rebase sentence
// when smind rebased this published branch and origin still has the tip it had
// then (the remote-tracking ref git has just left alone).
func (m *Manager) rebasedPushError(ctx context.Context, id int64, pe *forge.PRError, stderr string) *forge.PRError {
	marker := m.RebasedOrigin(id)
	if pe.Code != forge.CodeNonFastForward || marker == "" {
		return pe
	}
	t, err := m.store.GetTask(id)
	if err != nil || t.WorktreePath == nil || t.Branch == nil {
		return pe
	}
	if prRevParse(ctx, *t.WorktreePath, "refs/remotes/"+originRemote+"/"+*t.Branch) != marker {
		return pe
	}
	return forge.RebasedDiverged().WithDetail(stderr)
}

// UpdateFromBase brings the task's base branch into its branch: a merge (the
// default) or a rebase. It fetches origin/<base> when origin exists, targets
// that (else the local base branch), and does nothing when the branch already
// holds it. A conflict is left as it is -- the worktree stays in the merge or
// rebase -- and returned as the typed conflict error with the unmerged paths.
func (m *Manager) UpdateFromBase(ctx context.Context, id int64, strategy string) (UpdateResult, error) {
	switch strategy {
	case "", SyncStrategyMerge:
		strategy = SyncStrategyMerge
	case SyncStrategyRebase:
	default:
		return UpdateResult{}, forge.NewPRError(forge.CodeInvalidStrategy, "")
	}
	tg, err := m.prTarget(id, "")
	if err != nil {
		return UpdateResult{}, err
	}
	if op := worktreeOperation(ctx, tg.worktree); op != "" {
		return UpdateResult{}, forge.MergeInProgress(op)
	}

	if originURL(ctx, tg.worktree) != "" {
		fctx, cancel := context.WithTimeout(ctx, prFetchTimeout)
		_, stderr, ferr := execGit(fctx, tg.worktree, nil, netGitArgs(fctx, tg.worktree, originRemote, "fetch", "--no-tags", originRemote, tg.base)...)
		cancel()
		if ferr != nil {
			pe := prGitError(fctx, ferr, stderr)
			if pe.Code == forge.CodeError && prCouldntFindRef(pe) {
				return UpdateResult{}, forge.NewPRError(forge.CodeBaseNotFound, prDetail(pe))
			}
			return UpdateResult{}, pe
		}
	}
	target := ""
	for _, ref := range []string{"refs/remotes/" + originRemote + "/" + tg.base, "refs/heads/" + tg.base} {
		if prRevParse(ctx, tg.worktree, ref) != "" {
			target = ref
			break
		}
	}
	if target == "" {
		return UpdateResult{}, forge.NewPRError(forge.CodeBaseNotFound, "")
	}
	n := syncCount(ctx, tg.worktree, "HEAD.."+target)
	if n == 0 {
		return UpdateResult{Result: SyncUpToDate}, nil
	}

	// Where origin's copy of this branch stands before a rebase rewrites ours
	// ("" when the branch was never published).
	publishedTip := prRevParse(ctx, tg.worktree, "refs/remotes/"+originRemote+"/"+tg.branch)

	lctx, cancel := context.WithTimeout(ctx, syncLocalTimeout)
	defer cancel()
	env := append(os.Environ(), "GIT_EDITOR=true", "GIT_MERGE_AUTOEDIT=no")
	var (
		args   []string
		op     = forge.OpMerge
		result = SyncMerged
	)
	if strategy == SyncStrategyRebase {
		args, op, result = []string{"rebase", "--end-of-options", target}, forge.OpRebase, SyncRebased
	} else {
		args = []string{"merge", "-m", "Merge " + shortRemoteRef(target) + " into " + tg.branch, "--end-of-options", target}
	}
	if _, stderr, err := execGit(lctx, tg.worktree, env, args...); err != nil {
		if lctx.Err() == nil {
			if files, truncated := unmergedFiles(ctx, tg.worktree); len(files) > 0 {
				return UpdateResult{}, forge.ConflictError(op, shortRemoteRef(target), files, truncated)
			}
		}
		return UpdateResult{}, prGitError(lctx, err, stderr)
	}
	if result == SyncRebased {
		m.setRebasedOrigin(id, publishedTip)
	} else {
		m.setRebasedOrigin(id, "")
	}
	return UpdateResult{Result: result, Commits: n}, nil
}

func shortRemoteRef(ref string) string {
	if s, ok := strings.CutPrefix(ref, "refs/remotes/"); ok {
		return s
	}
	return strings.TrimPrefix(ref, "refs/heads/")
}

// unmergedFiles lists the paths git left unmerged, sorted and capped.
func unmergedFiles(ctx context.Context, dir string) (files []string, truncated bool) {
	out, _, err := execGit(ctx, dir, nil, "diff", "--name-only", "-z", "--diff-filter=U")
	if err != nil {
		return nil, false
	}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	if len(files) > syncConflictFileLimit {
		files, truncated = files[:syncConflictFileLimit], true
	}
	return files, truncated
}

// worktreeOperation is "merge" or "rebase" when the worktree is in the middle
// of one (MERGE_HEAD, rebase-merge or rebase-apply exists), else "".
func worktreeOperation(ctx context.Context, dir string) string {
	out, _, err := execGit(ctx, dir, nil, "rev-parse", "--git-path", "MERGE_HEAD", "--git-path", "rebase-merge", "--git-path", "rebase-apply")
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
		return forge.OpRebase
	case exists(paths[0]):
		return forge.OpMerge
	}
	return ""
}

// originURL is the configured url of `origin`, "" when there is none.
func originURL(ctx context.Context, dir string) string { return remoteURLOf(ctx, dir, originRemote) }

// remoteURLOf is the configured url of remote, "" when there is none.
func remoteURLOf(ctx context.Context, dir, remote string) string {
	out, _, err := execGit(ctx, dir, nil, "config", "--get", "remote."+remote+".url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// syncCount is `git rev-list --count <rng>`; 0 when git cannot answer.
func syncCount(ctx context.Context, dir, rng string) int {
	out, _, err := execGit(ctx, dir, nil, "rev-list", "--count", rng)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}
