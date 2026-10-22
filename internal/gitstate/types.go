// Package gitstate computes the per-task git status payload (ADR-0039 §3, §4):
// local status from a hardened git runner, the merge-tree conflict token, the
// merged verdict and the createPr/updatePr action policy. It must not import
// internal/workspace; callers hand it what it needs through Input.
package gitstate

import (
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// TaskGit is the one status payload per task. Every nested object is optional:
// an absent object or pointer means "not known", never zero.
type TaskGit struct {
	TaskID     int64             `json:"taskId"`
	Forge      string            `json:"forge,omitempty"`
	Branch     *Branch           `json:"branch,omitempty"`
	Base       *Base             `json:"base,omitempty"`
	Diff       *Diff             `json:"diff,omitempty"`
	Worktree   *Worktree         `json:"worktree,omitempty"`
	Commits    *Commits          `json:"commits,omitempty"`
	Origin     *Origin           `json:"origin,omitempty"`
	Conflict   *Conflict         `json:"conflict,omitempty"`
	Merged     *Merged           `json:"merged,omitempty"`
	PR         *PRInfo           `json:"pr,omitempty"`
	Actions    map[string]Action `json:"actions,omitempty"`
	Untrusted  bool              `json:"untrusted"`
	ComputedAt time.Time         `json:"computedAt"`
}

type Branch struct {
	Name  string `json:"name"`
	Owned bool   `json:"owned"`
}

// Base describes the branch the task is based on. Tip is the commit of the
// compare ref (origin/<ref>, else the local branch); empty when neither exists.
type Base struct {
	Ref   string `json:"ref,omitempty"`
	Sha   string `json:"sha,omitempty"`
	Tip   string `json:"tip,omitempty"`
	Known bool   `json:"known"`
}

type Diff struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

type Worktree struct {
	Dirty     bool `json:"dirty"`
	Staged    int  `json:"staged"`
	Unstaged  int  `json:"unstaged"`
	Untracked int  `json:"untracked"`
}

// Commits: AheadOfBase/BehindBase are against the compare ref; SinceFork counts
// the branch's commits past base_sha (what Create PR needs).
type Commits struct {
	AheadOfBase *int `json:"aheadOfBase,omitempty"`
	BehindBase  *int `json:"behindBase,omitempty"`
	SinceFork   *int `json:"sinceFork,omitempty"`
}

type Origin struct {
	Upstream  string `json:"upstream,omitempty"`
	Published bool   `json:"published"`
	Ahead     *int   `json:"ahead,omitempty"`
	Behind    *int   `json:"behind,omitempty"`
}

const (
	ConflictNone      = "none"
	ConflictConflicts = "conflicts"
	ConflictUnknown   = "unknown"

	MergedMerged   = "merged"
	MergedUnmerged = "unmerged"
	MergedUnknown  = "unknown"

	MergedViaAncestor = "ancestor"
	MergedViaPR       = "pr"

	ReasonGitTooOld = "git_too_old"
)

type Conflict struct {
	State     string    `json:"state"`
	Files     []string  `json:"files"`
	Against   string    `json:"against,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
	Reason    string    `json:"reason,omitempty"`
}

type Merged struct {
	State string `json:"state"`
	Via   string `json:"via"`
}

// PRInfo is the forge-neutral PR snapshot (ADR §3); forge.PR is the single
// definition, so the wire shape cannot drift from what the provider returns.
// It is always nil in gitstate's local pass; the poller fills it later.
type PRInfo = forge.PR

// Checks is PRInfo's CI rollup.
type Checks = forge.Checks

// Action is one advisory entry of TaskGit.Actions. The RPC re-checks.
type Action struct {
	Enabled bool   `json:"enabled"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
