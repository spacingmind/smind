package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/store"
)

// The policy.* RPCs (ADR-0047 §2): read-only views of the permission policy
// engine for the Human's console. The policy file is edited by a human on
// disk and the audit is append-only, so there is no setter, no way to edit or
// clear either, and no run-scoped agent may call any of them: an agent never
// reads or probes the rules that judge it.

// configStewardPolicyEnabled reads steward.policy.enabled on every call, so a
// human edit applies without a restart. An unreadable or invalid file is OFF.
// Tests replace it.
var configStewardPolicyEnabled = func() bool {
	cfg, err := config.Load()
	return err == nil && cfg.Steward.Policy.Enabled
}

const (
	policyTestMaxCommand = 8 << 10
	policyAuditDefault   = 50
	policyAuditMax       = 200
	policySweepPeriod    = time.Hour
)

// policyAuditDecisions is the closed decision vocabulary a policy.audit
// filter may name.
var policyAuditDecisions = map[string]bool{
	"allow": true, "deny": true, "ask": true,
	"ask_human_allow": true, "ask_human_deny": true, "ask_timeout_deny": true,
}

// policySweeper deletes audit rows past their retention once at start and
// then every hour, until stopped.
type policySweeper struct {
	done chan struct{}
	once sync.Once
}

func startPolicySweeper(db *store.Store) *policySweeper {
	s := &policySweeper{done: make(chan struct{})}
	sweep := func() { _, _ = db.SweepPolicyAudit(time.Now().Add(-store.PolicyAuditRetention)) }
	sweep()
	go func() {
		t := time.NewTicker(policySweepPeriod)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				sweep()
			case <-s.done:
				return
			}
		}
	}()
	return s
}

func (s *policySweeper) stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.done) })
}

type policyRuleView struct {
	ID         string   `json:"id"`
	Action     string   `json:"action"`
	Why        string   `json:"why"`
	Seats      []string `json:"seats"`
	Workspaces []string `json:"workspaces"`
	Tool       string   `json:"tool"`
	Argv0      string   `json:"argv0"`
	Subcommand string   `json:"subcommand"`
}

type policyStatusView struct {
	Enabled bool   `json:"enabled"`
	Valid   bool   `json:"valid"`
	Exists  bool   `json:"exists"`
	Error   string `json:"error"`
	Path    string `json:"path"`
	Sha     string `json:"sha"`
	Rules   int    `json:"rules"`
}

type policySegmentView struct {
	Index   int      `json:"index"`
	Text    string   `json:"text"`
	Action  string   `json:"action"`
	RuleID  string   `json:"ruleId"`
	FloorID string   `json:"floorId"`
	Reasons []string `json:"reasons"`
}

type policyDecisionView struct {
	Action     string              `json:"action"`
	RuleID     string              `json:"ruleId"`
	FloorID    string              `json:"floorId"`
	Why        string              `json:"why"`
	Reasons    []string            `json:"reasons"`
	Normalized string              `json:"normalized"`
	Paths      []string            `json:"paths"`
	Segments   []policySegmentView `json:"segments"`
}

func statusView(s permpolicy.Status) policyStatusView {
	return policyStatusView{Enabled: s.Enabled, Valid: s.Valid, Exists: s.Exists, Error: s.Error, Path: s.Path, Sha: s.Sha, Rules: s.Rules}
}

func ruleViews(rules []permpolicy.Rule) []policyRuleView {
	out := make([]policyRuleView, 0, len(rules))
	for _, r := range rules {
		v := policyRuleView{ID: r.ID, Action: string(r.Action), Why: r.Why, Seats: nonNil(r.Match.Seats), Workspaces: nonNil(r.Match.Workspaces), Tool: r.Match.Tool}
		if c := r.Match.Command; c != nil {
			v.Argv0, v.Subcommand = c.Argv0, c.Subcommand
		}
		out = append(out, v)
	}
	return out
}

func decisionView(d permpolicy.Decision) policyDecisionView {
	v := policyDecisionView{
		Action: string(d.Action), RuleID: d.RuleID, FloorID: d.FloorID, Why: d.Why,
		Reasons: nonNil(d.Reasons), Normalized: d.Normalized, Paths: nonNil(d.Paths),
		Segments: make([]policySegmentView, 0, len(d.Segments)),
	}
	for _, s := range d.Segments {
		v.Segments = append(v.Segments, policySegmentView{
			Index: s.Index, Text: s.Text, Action: string(s.Action), RuleID: s.RuleID, FloorID: s.FloorID, Reasons: nonNil(s.Reasons),
		})
	}
	return v
}

func policyHandlers(engine *permpolicy.Engine, db *store.Store) map[string]handlerFunc {
	hs := map[string]handlerFunc{}

	hs["policy.get"] = func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if err := refuseAgent("policy.get", rc); err != nil {
			return nil, err
		}
		st := engine.Status()
		return struct {
			policyStatusView
			Rules []policyRuleView `json:"rules"`
		}{statusView(st), ruleViews(engine.Rules())}, nil
	}

	hs["policy.test"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "policy.test"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		if !permpolicy.Supported() {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": " + permpolicy.UnsupportedMessage}
		}
		var p struct {
			Tool       string   `json:"tool"`
			Command    string   `json:"command"`
			Paths      []string `json:"paths"`
			TaskID     int64    `json:"taskId"`
			Seat       string   `json:"seat"`
			Workspace  string   `json:"workspace"`
			Worktree   string   `json:"worktree"`
			TaskBranch string   `json:"taskBranch"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if len(p.Command) > policyTestMaxCommand {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": command is over 8 KiB"}
		}
		req := permpolicy.Request{
			Tool: p.Tool, Command: p.Command, Paths: p.Paths,
			Seat: p.Seat, Workspace: p.Workspace, Worktree: p.Worktree, Cwd: p.Worktree, TaskBranch: p.TaskBranch,
		}
		if p.TaskID != 0 {
			task, err := db.GetTask(p.TaskID)
			if err != nil {
				return nil, &rpcCodeError{code: codeInvalid, msg: m + ": unknown task", err: err}
			}
			req.TaskID, req.WorkspaceID = task.ID, task.WorkspaceID
			if req.Workspace == "" {
				if ws, err := db.GetWorkspace(task.WorkspaceID); err == nil {
					req.Workspace = ws.Title
				}
			}
			if req.Worktree == "" && task.WorktreePath != nil {
				req.Worktree, req.Cwd = *task.WorktreePath, *task.WorktreePath
			}
			if req.TaskBranch == "" && task.Branch != nil {
				req.TaskBranch = *task.Branch
			}
		}
		x := engine.Explain(req)
		return struct {
			Decision policyDecisionView `json:"decision"`
			Status   policyStatusView   `json:"status"`
		}{decisionView(x.Decision), statusView(x.Status)}, nil
	}

	hs["policy.audit"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "policy.audit"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			TaskID   int64  `json:"taskId"`
			Decision string `json:"decision"`
			Limit    int    `json:"limit"`
			BeforeID int64  `json:"beforeId"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.Decision != "" && !policyAuditDecisions[p.Decision] {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": unknown decision " + p.Decision}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = policyAuditDefault
		}
		if limit > policyAuditMax {
			limit = policyAuditMax
		}
		rows, err := db.ListPolicyAudit(store.PolicyAuditFilter{TaskID: p.TaskID, Decision: p.Decision, Limit: limit, BeforeID: p.BeforeID})
		if err != nil {
			return nil, errors.Join(errors.New(m+": list failed"), err)
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, map[string]any{
				"id": r.ID, "createdAt": r.CreatedAt, "runId": r.RunID, "taskId": r.TaskID, "requestId": r.RequestID,
				"seat": r.Seat, "workspaceId": r.WorkspaceID, "tool": r.Tool, "target": r.Target, "paths": nonNil(r.Paths),
				"decision": r.Decision, "ruleId": r.RuleID, "policySha": r.PolicySha, "reasons": r.Reasons, "latencyMs": r.LatencyMs,
			})
		}
		var next int64
		if len(rows) == limit {
			next = rows[len(rows)-1].ID
		}
		return map[string]any{"rows": out, "next": next}, nil
	}
	return hs
}
