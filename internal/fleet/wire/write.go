package wire

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Method names of the fleet write methods.
const (
	MethodTaskCreate = "fleet.task.create"
	MethodTaskSend   = "fleet.task.send"
	MethodTaskStop   = "fleet.task.stop"
	MethodRunWait    = "fleet.run.wait"
)

// WriteMethods lists every write method.
var WriteMethods = []string{
	MethodTaskCreate,
	MethodTaskSend,
	MethodTaskStop,
	MethodRunWait,
}

// Refusal codes only the write path returns.
const (
	CodeBudgetExceeded        = "budget_exceeded"
	CodeProviderNotConfigured = "provider_not_configured"
	CodeBranchNotFound        = "branch_not_found"
	CodeNotOwner              = "not_owner"
	CodeInternal              = "internal_error"
)

// The `which` of a budget_exceeded refusal: which limit the origin host hit.
const (
	BudgetLiveAgents = "live_agents"
	BudgetCost       = "cost"
	BudgetWall       = "wall"
)

// StopReasonWallLimit is stamped on a run the callee stopped because it
// exceeded the grant's wall budget.
const StopReasonWallLimit = "wall_limit"

// Limits of the write methods. IdempotencyWindow is how long the callee
// remembers an idempotencyKey; MaxRunWait bounds the fleet.run.wait long poll.
const (
	MaxPromptBytes         = 64 * 1024
	MaxIdempotencyKeyBytes = 128
	MaxRunWait             = 25 * time.Second
	IdempotencyWindow      = 24 * time.Hour
)

// maxTitleBytes and maxRefNameBytes keep a title or ref name from carrying a
// prompt-sized payload of its own.
const (
	maxTitleBytes   = 200
	maxRefNameBytes = 200
)

// TaskCreateParams are the params of fleet.task.create. The shape is closed
// on purpose (AC17): no permissionMode, profileId, env, cwd, attachment, mcp,
// grant or secret field exists, so a remote caller can say what to run but
// never how the callee's own agents are configured or authenticated.
type TaskCreateParams struct {
	Prov           Prov   `json:"prov"`
	IdempotencyKey string `json:"idempotencyKey"`
	WorkspaceID    int64  `json:"workspaceId"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Title          string `json:"title"`
	Prompt         string `json:"prompt"`
	Branch         string `json:"branch,omitempty"`
	BaseRef        string `json:"baseRef,omitempty"`
}

func (p TaskCreateParams) Validate() error {
	if err := validateIdempotencyKey(p.IdempotencyKey); err != nil {
		return err
	}
	if p.WorkspaceID <= 0 {
		return errors.New("workspaceId must be > 0")
	}
	if strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.Model) == "" {
		return errors.New("provider and model are required")
	}
	if len(p.Title) > maxTitleBytes {
		return fmt.Errorf("title must be at most %d bytes", maxTitleBytes)
	}
	if err := validatePrompt(p.Prompt); err != nil {
		return err
	}
	if p.Branch != "" && p.BaseRef != "" {
		return errors.New("branch and baseRef are mutually exclusive")
	}
	if p.Branch != "" && !validRefName(p.Branch) {
		return errors.New("branch is not a valid ref name")
	}
	if p.BaseRef != "" && !validRefName(p.BaseRef) {
		return errors.New("baseRef is not a valid ref name")
	}
	return nil
}

// TaskCreateResult is the result of fleet.task.create. Replayed is true when
// the idempotencyKey matched a stored create and the same task is returned.
type TaskCreateResult struct {
	TaskID   int64  `json:"taskId"`
	RunID    string `json:"runId"`
	Branch   string `json:"branch"`
	Replayed bool   `json:"replayed,omitempty"`
}

// TaskSendParams are the params of fleet.task.send.
type TaskSendParams struct {
	Prov           Prov   `json:"prov"`
	IdempotencyKey string `json:"idempotencyKey"`
	TaskID         int64  `json:"taskId"`
	Prompt         string `json:"prompt"`
}

func (p TaskSendParams) Validate() error {
	if err := validateIdempotencyKey(p.IdempotencyKey); err != nil {
		return err
	}
	if p.TaskID <= 0 {
		return errors.New("taskId must be > 0")
	}
	return validatePrompt(p.Prompt)
}

// TaskSendResult is the result of fleet.task.send. Replayed has the same
// meaning as in TaskCreateResult.
type TaskSendResult struct {
	TaskID   int64  `json:"taskId"`
	RunID    string `json:"runId"`
	Replayed bool   `json:"replayed,omitempty"`
}

// TaskStopParams are the params of fleet.task.stop. RunID is optional: with
// it the callee stops that run, without it the task's live run.
type TaskStopParams struct {
	Prov   Prov   `json:"prov"`
	TaskID int64  `json:"taskId"`
	RunID  string `json:"runId,omitempty"`
}

func (p TaskStopParams) Validate() error {
	if p.TaskID <= 0 {
		return errors.New("taskId must be > 0")
	}
	return nil
}

// TaskStopResult is the result of fleet.task.stop.
type TaskStopResult struct {
	Stopped bool `json:"stopped"`
}

// RunWaitParams are the params of fleet.run.wait, a bounded long poll the
// caller resumes from Cursor (a run event seq).
type RunWaitParams struct {
	Prov           Prov   `json:"prov"`
	RunID          string `json:"runId"`
	Cursor         int64  `json:"cursor,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

func (p RunWaitParams) Validate() error {
	if p.RunID == "" {
		return errors.New("runId is required")
	}
	if p.TimeoutSeconds < 0 {
		return errors.New("timeoutSeconds must be >= 0")
	}
	return nil
}

// RunWaitResult is the result of fleet.run.wait. WaitingPermission is all the
// caller ever learns about a permission request: the request itself goes to
// the callee's inbox and no fleet method can answer it (AC19).
type RunWaitResult struct {
	RunID             string  `json:"runId"`
	State             string  `json:"state"`
	WaitingPermission bool    `json:"waitingPermission"`
	Events            []Event `json:"events"`
	NextCursor        int64   `json:"nextCursor"`
	Done              bool    `json:"done"`
	StopReason        string  `json:"stopReason,omitempty"`
}

func validateIdempotencyKey(key string) error {
	if key == "" {
		return errors.New("idempotencyKey is required")
	}
	if len(key) > MaxIdempotencyKeyBytes {
		return fmt.Errorf("idempotencyKey must be at most %d bytes", MaxIdempotencyKeyBytes)
	}
	return nil
}

func validatePrompt(prompt string) error {
	if prompt == "" {
		return errors.New("prompt is required")
	}
	if len(prompt) > MaxPromptBytes {
		return fmt.Errorf("prompt must be at most %d bytes", MaxPromptBytes)
	}
	return nil
}

// validRefName is the ref-name rule shared by branch and baseRef: it mirrors
// what git check-ref-format accepts, so a hostile name is refused here before
// it can reach a git argument list on the callee.
func validRefName(ref string) bool {
	if ref == "" || len(ref) > maxRefNameBytes {
		return false
	}
	if strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") {
		return false
	}
	if strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".lock") {
		return false
	}
	for _, r := range ref {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}
