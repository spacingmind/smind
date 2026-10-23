package forge

import "fmt"

// CherryPickBranchPrefix starts the name of the throwaway branch Create PR
// pushes when the task's base has diverged from origin (smind/pr-task-<id>).
// A PR-start task's own branch is smind/pr-<n>-<slug>, hence "pr-task".
const CherryPickBranchPrefix = "smind/pr-task-"

// Codes of the Create PR / Update PR actions that are not forge_* codes
// (ADR-0039 section 6). The forge_* codes are Error's.
const (
	CodeTaskNoWorktree     = "task_no_worktree"
	CodeBaseUnknown        = "base_unknown"
	CodeBaseNotFound       = "base_not_found"
	CodeRunActive          = "run_active"
	CodeNoCommits          = "no_commits"
	CodePRExists           = "pr_exists"
	CodePRNotOpen          = "pr_not_open"
	CodeNoPR               = "no_pr"
	CodeForkPRReadonly     = "fork_pr_readonly"
	CodeUpdateUnsupported  = "update_unsupported"
	CodePushRejected       = "push_rejected"
	CodePushAuth           = "push_auth"
	CodeCherryPickConflict = "cherry_pick_conflict"
	CodeInvalidTitle       = "invalid_title"
	CodeInvalidBody        = "invalid_body"
	CodeInvalidBranch      = "invalid_branch"
	CodePRBusy             = "pr_busy"
)

// Codes of Push and Update from base (ADR-0039 sections 3-6).
const (
	CodeNonFastForward  = "non_fast_forward"
	CodeConflict        = "conflict"
	CodeMergeInProgress = "merge_in_progress"
	CodeWorktreeDirty   = "worktree_dirty"
	CodeNothingToPush   = "nothing_to_push"
	CodeInvalidStrategy = "invalid_strategy"
)

// Operations a worktree can be in the middle of (Update from base).
const (
	OpMerge  = "merge"
	OpRebase = "rebase"
)

// Codes of the archive branch cleanup and Restore (ADR-0039 section 9).
const (
	CodeBranchUnmerged   = "branch_unmerged"
	CodeBranchDirty      = "branch_dirty"
	CodeBranchNotOwned   = "branch_not_owned"
	CodeBranchCheckedOut = "branch_checked_out"
	CodeBranchGone       = "branch_gone"
	CodeNotArchived      = "not_archived"
)

// PRError is a typed failure of task.createPr / task.updatePr: a wire code, a
// fixed sentence naming the next step, and optional data (the scrubbed git
// stderr tail as "detail", or {url, number} for pr_exists). It never carries
// argv, a token or an environment value.
type PRError struct {
	Code    string
	Message string
	Data    map[string]any
}

// Error returns the fixed message only.
func (e *PRError) Error() string { return e.Message }

// RPCCode is the wire error code.
func (e *PRError) RPCCode() string { return e.Code }

// RPCData is the wire error data; nil when there is none.
func (e *PRError) RPCData() any {
	if len(e.Data) == 0 {
		return nil
	}
	return e.Data
}

// NewPRError builds the error for code with its standard sentence. detail is
// scrubbed and clipped before it is attached. A code with no standard
// sentence here (the forge_* ones are built by NewError) gets a generic one.
func NewPRError(code, detail string) *PRError {
	e := &PRError{Code: code, Message: prMessages[code]}
	if e.Message == "" {
		e.Message = "The pull request action failed."
	}
	return e.WithDetail(detail)
}

// WithDetail attaches the scrubbed, clipped detail (a no-op when empty).
func (e *PRError) WithDetail(detail string) *PRError {
	if d := ClipDetail(detail); d != "" {
		e.set("detail", d)
	}
	return e
}

// With attaches one data value.
func (e *PRError) With(key string, v any) *PRError {
	e.set(key, v)
	return e
}

func (e *PRError) set(key string, v any) {
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	e.Data[key] = v
}

// CherryPickConflict is the error for a replay of the task's commits onto
// base that did not apply cleanly.
func CherryPickConflict(base, detail string) *PRError {
	return NewPRError(CodeCherryPickConflict, detail).withMessage(
		fmt.Sprintf("smind couldn't replay your commits onto `%s` without conflicts. Update this branch from `%s` first.", base, base))
}

// UpdateUnsupported is the error for Update PR on a pull request opened from
// the rebased copy head.
func UpdateUnsupported(head string) *PRError {
	return NewPRError(CodeUpdateUnsupported, "").withMessage(
		fmt.Sprintf("This pull request was opened from a rebased copy (`%s`), so smind can't update it automatically.", head))
}

// BranchGone is the error for a Restore whose branch no longer exists
// locally or on origin.
func BranchGone(branch string) *PRError {
	return NewPRError(CodeBranchGone, "").withMessage(
		fmt.Sprintf("The branch `%s` no longer exists, so there is nothing to restore.", branch))
}

// RestoreCheckedOut is the error for a Restore whose branch is checked out in
// another worktree.
func RestoreCheckedOut() *PRError {
	return NewPRError(CodeBranchCheckedOut, "").withMessage(
		"The branch is checked out in another worktree, so this task can't be restored until that worktree lets go of it.")
}

// ConflictError is the error for an Update from base that stopped on
// conflicts. The worktree is left in the middle of the operation (nothing is
// aborted); the sentence names the command that undoes it. files is sorted by
// the caller; filesTruncated says the list was capped.
func ConflictError(operation, base string, files []string, filesTruncated bool) *PRError {
	var m string
	if operation == OpRebase {
		m = fmt.Sprintf("Rebasing onto `%s` hit conflicts, and the worktree is now in the middle of the rebase. Resolve the conflicts and continue, or ask the agent to resolve them; `git rebase --abort` in the worktree undoes it.", base)
	} else {
		operation = OpMerge
		m = fmt.Sprintf("Merging `%s` into this branch hit conflicts, and the worktree is now in the middle of the merge. Resolve the conflicts and commit, or ask the agent to resolve them; `git merge --abort` in the worktree undoes it.", base)
	}
	if files == nil {
		files = []string{}
	}
	return (&PRError{Code: CodeConflict, Message: m}).
		With("files", files).With("operation", operation).With("base", base).With("filesTruncated", filesTruncated)
}

// Sentences of a push smind will not attempt (shared by the gitstate policy
// and the typed errors, so the card and the RPC never disagree).
const (
	// MsgRebasedDiverged: smind rebased a published branch, so a plain push
	// is refused by origin and smind never forces.
	MsgRebasedDiverged = "This branch was rebased and has diverged from origin; smind can't force-push. Push it manually (`git push --force-with-lease`) or reset to origin."
	// MsgForkPushReadonly: the task came from a pull request on a fork.
	MsgForkPushReadonly = "This task was started from a pull request on a fork, so smind can't push to it. Push it yourself or open a new pull request from your own branch."
)

// RebasedDiverged is the non_fast_forward error for a branch smind rebased
// after it was published.
func RebasedDiverged() *PRError {
	return NewPRError(CodeNonFastForward, "").withMessage(MsgRebasedDiverged)
}

// ForkPushReadonly is the fork_pr_readonly error for a push from a task that
// was started from a pull request on a fork.
func ForkPushReadonly() *PRError {
	return NewPRError(CodeForkPRReadonly, "").withMessage(MsgForkPushReadonly)
}

// MergeInProgress is the error for an Update from base on a worktree that is
// already in the middle of a merge or rebase.
func MergeInProgress(operation string) *PRError {
	if operation != OpRebase {
		operation = OpMerge
	}
	abort := "git merge --abort"
	if operation == OpRebase {
		abort = "git rebase --abort"
	}
	return (&PRError{Code: CodeMergeInProgress, Message: fmt.Sprintf(
		"This worktree is already in the middle of a %s. Finish it (resolve the conflicts and commit or continue), or undo it with `%s`, then try again.", operation, abort)}).
		With("operation", operation)
}

func (e *PRError) withMessage(m string) *PRError {
	e.Message = m
	return e
}

var prMessages = map[string]string{
	CodeTaskNoWorktree:    "This task has no worktree, so there is nothing to open a pull request from.",
	CodeBaseUnknown:       "This task's base commit is unknown. Set the base branch first.",
	CodeBaseNotFound:      "That base branch wasn't found on `origin`. Pick an existing branch.",
	CodeRunActive:         "An agent is still working in this task. Wait for the run to finish, or stop it.",
	CodeNoCommits:         "This branch has no commits beyond its base. Commit your changes first.",
	CodePRExists:          "A pull request is already open for this branch.",
	CodePRNotOpen:         "That pull request is no longer open. Create a new one from this branch instead.",
	CodeNoPR:              "This branch has no pull request. Create one first.",
	CodeForkPRReadonly:    "This pull request comes from a fork, and smind can't push to forks yet.",
	CodeUpdateUnsupported: "This pull request was opened from a rebased copy, so smind can't update it automatically.",
	CodePushRejected:      "GitHub rejected the push because the branch there has changes this branch doesn't. Update this branch first.",
	CodePushAuth:          "Git couldn't sign in to push. Check the daemon host's git credentials, or run `gh auth login` there.",
	CodeInvalidTitle:      "The pull request title must be 1 to 256 characters on a single line.",
	CodeInvalidBody:       "The pull request description must be at most 64 KiB and can't contain NUL characters.",
	CodeInvalidBranch:     "That branch name isn't valid. Pick an existing branch.",
	CodeNonFastForward:    "Push was refused because origin has commits this branch doesn't. Bring those changes in first (pull in the worktree, or ask the agent), then push again.",
	CodeConflict:          "Updating this branch from its base hit conflicts.",
	CodeMergeInProgress:   "This worktree is already in the middle of a merge or rebase. Finish or undo it first.",
	CodeWorktreeDirty:     "This worktree has uncommitted changes. Commit or stash them first, then update from the base branch.",
	CodeNothingToPush:     "Nothing to push.",
	CodeInvalidStrategy:   "The update strategy must be \"merge\" or \"rebase\".",
	CodeUnauthenticated:   msgUnauthenticated,
	CodePRBusy:            "Another pull request action is already running for this task. Wait for it to finish.",
	CodeBranchUnmerged:    "The branch is not merged anywhere yet.",
	CodeBranchDirty:       "The worktree has uncommitted changes; they are saved to the branch, so it is kept.",
	CodeBranchNotOwned:    "This is your own branch; smind never deletes it.",
	CodeBranchCheckedOut:  "The branch is checked out in another worktree, so it is kept.",
	CodeNotArchived:       "This task isn't archived, so there is nothing to restore.",
	CodeTimeout:           "git didn't finish in time. Check the network and try again.",
	CodeError:             "git couldn't complete the request. See Details.",
	CodeNoRemote:          msgNoRemote,
	CodeNotFound:          msgNotFound,
}
