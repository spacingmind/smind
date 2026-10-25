package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// CommitDraft is the result of task.draftCommitMessage.
type CommitDraft struct {
	Message   string   `json:"message"`
	Model     string   `json:"model"`
	Truncated bool     `json:"truncated"`
	Omitted   []string `json:"omitted"`
}

// CommitDrafter drafts a commit message from a task's staged diff (ADR-0043).
// Its errors carry the wire code (RPCCode): nothing_staged, no_draft_provider,
// rate_limited, busy, empty_draft, draft_failed. wsapi holds only this
// interface; the daemon wiring supplies the model-backed implementation.
type CommitDrafter interface {
	DraftCommitMessage(ctx context.Context, taskID int64) (CommitDraft, error)
}

// commitDraftHost holds the drafter the daemon sets after construction.
type commitDraftHost struct {
	mu sync.RWMutex
	d  CommitDrafter
}

// SetCommitDrafter installs the drafter behind task.draftCommitMessage.
func (a *API) SetCommitDrafter(d CommitDrafter) {
	a.commitDraft.mu.Lock()
	a.commitDraft.d = d
	a.commitDraft.mu.Unlock()
}

// handleTaskDraftCommitMessage is task.draftCommitMessage {taskId}: a HUMAN asks
// for a drafted message for the staged diff. It never commits or stages. Agents
// are refused before anything else happens (ADR-0043 decision 2).
func handleTaskDraftCommitMessage(h *commitDraftHost) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := refuseRunScoped("task.draftCommitMessage", rc); err != nil {
			return nil, &rpcCodeError{code: "refused", msg: err.Error(), err: err}
		}
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.TaskID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: "task.draftCommitMessage: taskId is required"}
		}
		h.mu.RLock()
		d := h.d
		h.mu.RUnlock()
		if d == nil {
			return nil, &rpcCodeError{code: codeUnavailable, msg: "task.draftCommitMessage: not available", err: errors.New("no commit drafter")}
		}
		res, err := d.DraftCommitMessage(ctx, p.TaskID)
		if err != nil {
			return nil, err
		}
		if res.Omitted == nil {
			res.Omitted = []string{}
		}
		return res, nil
	}
}
