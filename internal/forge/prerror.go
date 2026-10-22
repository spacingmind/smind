package forge

import "fmt"

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
	CodePRBusy:            "Another pull request action is already running for this task. Wait for it to finish.",
	CodeTimeout:           "git didn't finish in time. Check the network and try again.",
	CodeError:             "git couldn't complete the request. See Details.",
	CodeNoRemote:          msgNoRemote,
	CodeNotFound:          msgNotFound,
}
