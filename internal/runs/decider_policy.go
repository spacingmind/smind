package runs

import (
	"fmt"
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
		// The engine judges the RAW command. detail.Command is the elided
		// form for humans; its [heredoc N bytes] marker would re-scan as a
		// new heredoc and drop every command after it. A detail without
		// PolicyCommand was not built by an adapter and has no elided form.
		req.Command, req.Truncated = detail.Command, detail.Truncated
		if detail.PolicyCommand != "" || detail.PolicyTruncated {
			req.Command, req.Truncated = detail.PolicyCommand, detail.PolicyTruncated
		} else if len(detail.Heredocs) > 0 {
			// Fail closed: an adapter that elided bodies but kept no raw
			// command would feed the engine text whose heredoc marker hides
			// every command after it.
			req.Doubt = "detail has elided heredocs but no policy command"
		}
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

// optionByID returns the offered option with the given id.
func optionByID(options []taskrunner.PermissionOption, id string) (taskrunner.PermissionOption, bool) {
	for _, o := range options {
		if o.ID == id {
			return o, true
		}
	}
	return taskrunner.PermissionOption{}, false
}

// withPolicy copies detail (a fresh "other" detail when nil) and sets its
// policy footprint; the provider's own detail is never mutated.
func withPolicy(detail *taskrunner.PermissionDetail, info taskrunner.PolicyInfo) *taskrunner.PermissionDetail {
	out := &taskrunner.PermissionDetail{Kind: "other"}
	if detail != nil {
		c := *detail
		out = &c
	}
	out.Policy = &info
	return out
}

// applyPolicy consults the policy engine for a request that no earlier
// branch (system run, AutoAccept) answered. handled means the engine
// resolved it: the request and its resolution are recorded back to back like
// the AutoAccept branch, no pending channel is registered and no
// notification goes out, because no human is involved. An empty optionID
// with handled set means a deny the provider offered no reject option for.
// Otherwise (engine absent or off, or an `ask`) the request continues to the
// human path with detailOut, which carries the policy footprint when the
// engine was on. An allow the provider gave no allow_once option for is an
// ask: the engine never picks an "always" option.
func (d runPermissionDecider) applyPolicy(requestID, summary string, detail *taskrunner.PermissionDetail, options []taskrunner.PermissionOption) (optionID, denyReason string, handled bool, detailOut *taskrunner.PermissionDetail) {
	engine := d.reg.getPolicyEngine()
	if engine == nil {
		return "", "", false, detail
	}
	dec, on := engine.Evaluate(d.policyRequest(requestID, summary, detail))
	if !on {
		return "", "", false, detail
	}
	info := taskrunner.PolicyInfo{RuleID: dec.RuleID, Why: dec.Why, Normalized: dec.Normalized, Action: string(dec.Action)}
	if dec.RuleID == "" {
		info.RuleID = dec.FloorID
	}
	switch dec.Action {
	case permpolicy.ActionAllow:
		if id, ok := firstOptionByKind(options, "allow_once"); ok {
			d.recordPolicyResolved(requestID, summary, withPolicy(detail, info), options, id, taskrunner.PermissionResolvedByPolicyAllow, dec.RuleID)
			return id, "", true, detail
		}
	case permpolicy.ActionDeny:
		ruleID := dec.FloorID
		reason := fmt.Sprintf("floor %s: %s", dec.FloorID, dec.Why)
		if ruleID == "" {
			ruleID = dec.RuleID
			reason = fmt.Sprintf("policy rule %s: %s", dec.RuleID, dec.Why)
		}
		id, _ := firstOptionByKind(options, "reject_once", "reject_always")
		if id == "" {
			return "", "", true, detail
		}
		d.recordPolicyResolved(requestID, summary, withPolicy(detail, info), options, id, taskrunner.PermissionResolvedByPolicyDeny, ruleID)
		return id, reason, true, detail
	}
	info.Action = string(permpolicy.ActionAsk)
	return "", "", false, withPolicy(detail, info)
}

func (d runPermissionDecider) recordPolicyResolved(requestID, summary string, detail *taskrunner.PermissionDetail, options []taskrunner.PermissionOption, optionID string, resolution taskrunner.PermissionResolution, ruleID string) {
	d.reg.record(d.r, taskrunner.Event{
		Type:                taskrunner.EventTypePermissionRequest,
		PermissionRequestID: requestID,
		PermissionSummary:   summary,
		PermissionOptions:   options,
		PermissionDetail:    detail,
	})
	d.reg.record(d.r, taskrunner.Event{
		Type:                 taskrunner.EventTypePermissionResolved,
		PermissionRequestID:  requestID,
		PermissionOptionID:   optionID,
		PermissionResolution: resolution,
		PermissionRuleID:     ruleID,
	})
}
