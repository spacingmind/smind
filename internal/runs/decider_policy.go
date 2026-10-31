package runs

import (
	"strings"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// SetPolicyEngine installs the permission policy engine (ADR-0047). Call it
// before runs start; nil removes it.
func (reg *Registry) SetPolicyEngine(e *permpolicy.Engine) {
	reg.mu.Lock()
	reg.policy = e
	reg.mu.Unlock()
}

func (reg *Registry) getPolicyEngine() *permpolicy.Engine {
	reg.mu.Lock()
	e := reg.policy
	reg.mu.Unlock()
	return e
}

// policyRequest maps a provider permission request to the engine's Request.
// Provider adapters that send no detail leave only the summary ("run Read"),
// which still names the tool so a deny/ask rule can match it; anything
// unknown reaches the engine with an empty tool and is `ask`. A task or
// profile that cannot be read leaves its fields empty, which makes the
// engine ask rather than allow (no worktree, no allow).
func (d runPermissionDecider) policyRequest(requestID, summary string, detail *taskrunner.PermissionDetail) permpolicy.Request {
	req := permpolicy.Request{
		RunID:     d.r.id,
		TaskID:    d.r.taskID,
		RequestID: requestID,
	}
	switch {
	case detail != nil && detail.Kind == "execute":
		req.Tool = "Bash"
		req.Command = detail.Command
		req.Truncated = detail.Truncated
	case detail != nil && detail.Kind == "write":
		req.Tool = "Write"
		req.Paths = []string{detail.Path}
	default:
		if rest, ok := strings.CutPrefix(summary, "run "); ok {
			req.Tool, _, _ = strings.Cut(rest, " ")
		}
	}
	if d.r.profileID != 0 {
		if p, err := d.reg.st.GetAgentProfile(d.r.profileID); err == nil {
			req.Seat = p.Name
		}
	}
	if task, err := d.reg.st.GetTask(d.r.taskID); err == nil {
		req.WorkspaceID = task.WorkspaceID
		if ws, err := d.reg.st.GetWorkspace(task.WorkspaceID); err == nil {
			req.Workspace = ws.Title
		}
		if task.WorktreePath != nil {
			req.Worktree = *task.WorktreePath
			req.Cwd = *task.WorktreePath
		}
		if task.Branch != nil {
			req.TaskBranch = *task.Branch
		}
	}
	return req
}
