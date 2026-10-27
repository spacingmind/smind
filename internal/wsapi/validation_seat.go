package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
	"github.com/spacingmind/smind/internal/workspace"
)

// validatorProfileName is the role pack's validator seat (ADR-0045 §4).
const validatorProfileName = "validator"

// validationWatchPeriod is how often a running validation is supervised.
const validationWatchPeriod = 2 * time.Second

// validationSeat starts validator runs for validation.request and keeps each
// run's one-time verdict token in memory only: it reaches the validator's MCP
// entry through the runs registry's env hook and nothing else.
type validationSeat struct {
	svc     *validation.Service
	wm      *workspace.Manager
	db      *store.Store
	reg     *runs.Registry
	runner  *taskrunner.Runner
	profReg *profiles.Registry

	mu     sync.Mutex
	tokens map[int64]seatToken // by validator task id

	watchPeriod time.Duration
	// ctx bounds the watchers' lifetime (the daemon's).
	ctx context.Context
}

type seatToken struct {
	validationID int64
	contractID   int64
	token        string
}

func newValidationSeat(svc *validation.Service, wm *workspace.Manager, db *store.Store, reg *runs.Registry, runner *taskrunner.Runner, profReg *profiles.Registry) *validationSeat {
	s := &validationSeat{svc: svc, wm: wm, db: db, reg: reg, runner: runner, profReg: profReg,
		tokens: map[int64]seatToken{}, watchPeriod: validationWatchPeriod, ctx: context.Background()}
	reg.SetRunEnvSource(s.runEnv)
	return s
}

// runEnv is the runs registry hook: the extra MCP environment of a validator
// task's runs, only while its validation has no verdict.
func (s *validationSeat) runEnv(taskID int64) (map[string]string, bool) {
	s.mu.Lock()
	t, ok := s.tokens[taskID]
	s.mu.Unlock()
	if !ok {
		return nil, false
	}
	if val, err := s.db.GetValidation(t.validationID); err != nil || val.VerdictJSON != "" {
		return nil, false
	}
	return map[string]string{
		"SMIND_VALIDATION_ID": fmt.Sprint(t.validationID),
		"SMIND_CONTRACT_ID":   fmt.Sprint(t.contractID),
		"SMIND_VERDICT_TOKEN": t.token,
	}, true
}

func (s *validationSeat) forget(taskID int64) {
	s.mu.Lock()
	delete(s.tokens, taskID)
	s.mu.Unlock()
}

type requestResult struct {
	ValidationID    int64  `json:"validationId"`
	ContractID      int64  `json:"contractId"`
	CommitSHA       string `json:"commitSha"`
	ValidatorTaskID int64  `json:"validatorTaskId"`
	ChatID          int64  `json:"chatId"`
	RunID           string `json:"runId"`
}

func (s *validationSeat) request(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const m = "validation.request"
	if rc == nil {
		return nil, forbidden(m + " is not available")
	}
	var p struct {
		TaskID     int64  `json:"taskId"`
		ContractID int64  `json:"contractId"`
		CommitSHA  string `json:"commitSha"`
	}
	if err := pluginParams(m, raw, &p); err != nil {
		return nil, err
	}
	if rc.runScoped {
		if p.TaskID == 0 {
			p.TaskID = rc.ident.taskID
		}
		if p.TaskID != rc.ident.taskID {
			return nil, forbidden(m + " is only available for the caller's own task")
		}
	}
	if p.TaskID <= 0 {
		return nil, &rpcCodeError{code: codeInvalid, msg: m + ": taskId is required"}
	}
	lead, err := s.db.GetTask(p.TaskID)
	if err != nil || lead.WorktreePath == nil {
		return nil, &rpcCodeError{code: codeNotFound, msg: fmt.Sprintf("%s: task %d has no worktree", m, p.TaskID)}
	}

	var rec validation.ContractRecord
	if p.ContractID > 0 {
		rec, err = s.svc.GetContract(p.ContractID)
		if err == nil && rec.TaskID != p.TaskID {
			err = validation.ErrNotFound
		}
	} else {
		rec, err = s.svc.LatestContract(p.TaskID)
	}
	if err != nil {
		return nil, validationErr(m, fmt.Errorf("submit a contract first (validation.contract.submit): %w", err))
	}

	head, err := validation.GitHead(ctx, *lead.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("%s: read the branch head: %w", m, err)
	}
	sha := p.CommitSHA
	if sha == "" {
		sha = head
	} else {
		if !validation.IsSHA(sha) {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": commitSha must be a full 40-hex commit id"}
		}
		ok, err := validation.IsAncestor(ctx, *lead.WorktreePath, sha, head)
		if err != nil || !ok {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": commitSha is not on the task branch"}
		}
	}

	prof, err := s.validatorProfile()
	if err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: m + ": " + err.Error()}
	}

	// The validator is an agent launch under the tree budget (ADR-0041 A5); a
	// human-requested validation is never refused.
	if rc.runScoped {
		if err := s.reg.CheckTreeBudget(p.TaskID, store.ChatQueueSourceAgent); err != nil {
			return nil, err
		}
	}

	val, token, err := s.svc.CreateValidation(p.TaskID, rec.ID, sha)
	if err != nil {
		return nil, validationErr(m, err)
	}
	fail := func(err error) (any, error) {
		_ = s.svc.WriteBlocked(val.ID, validation.ReasonCrashed, "the validator could not be started")
		return nil, fmt.Errorf("%s: %w", m, err)
	}

	before, _ := validation.Take(ctx, *lead.WorktreePath)

	title := fmt.Sprintf("validate #%d: %s", val.ID, rec.Contract.Milestone)
	if len(title) > 120 {
		title = title[:120]
	}
	vtask, err := s.wm.CreateTask(lead.WorkspaceID, lead.SpaceID, title, workspace.WithParentTask(lead.ID), workspace.WithBaseRef(sha))
	if err != nil {
		return fail(err)
	}
	if vtask.WorktreePath == nil {
		return fail(errors.New("the validator worktree was not created"))
	}
	chat, err := s.wm.DefaultChat(vtask.ID)
	if err != nil {
		return fail(err)
	}
	if err := s.db.SetValidationValidatorTask(val.ID, vtask.ID, *vtask.WorktreePath); err != nil {
		return fail(err)
	}
	s.mu.Lock()
	s.tokens[vtask.ID] = seatToken{validationID: val.ID, contractID: rec.ID, token: token}
	s.mu.Unlock()

	var replyChat int64
	if rc.runScoped {
		replyChat = rc.ident.chatID
	}
	origin := store.ChatQueueSourceHuman
	if rc.runScoped {
		origin = store.ChatQueueSourceAgent
	}
	opts := []runs.StartOption{
		runs.WithOrigin(origin), runs.WithProfile(prof.ID), runs.WithReplyChat(replyChat),
		runs.WithNotifyOnFinish(replyChat != 0),
	}
	if prof.Model != "" {
		opts = append(opts, runs.WithModel(prof.Model))
	}
	runID, err := s.reg.Start(context.Background(), s.wm, s.runner, vtask.ID, chat.ID, taskrunner.Provider(prof.Provider),
		validation.BuildPrompt(rec, val.ID, sha),
		taskrunner.PermissionSettings{Mode: prof.PermissionMode, AutoAccept: prof.AutoAccept},
		taskrunner.ThinkingLevel(prof.ThinkingLevel), opts...)
	if err != nil {
		s.forget(vtask.ID)
		return fail(err)
	}
	_ = s.svc.BindRun(val.ID, runID, vtask.ID, *vtask.WorktreePath)

	w := &validation.Watch{
		Svc: s.svc, ValidationID: val.ID, ValidatorTaskID: vtask.ID,
		MaxMinutes: rec.Contract.Limits.MaxMinutes, BudgetUSD: rec.Contract.Limits.BudgetUSD,
		Probe: s.probe, Stop: func() { s.stopTask(vtask.ID) },
		Before: before, LeadSnapshot: func() (validation.Snapshot, error) { return validation.Take(context.Background(), *lead.WorktreePath) },
	}
	go func() {
		defer s.forget(vtask.ID)
		w.Run(s.ctx, s.watchPeriod)
	}()
	return requestResult{ValidationID: val.ID, ContractID: rec.ID, CommitSHA: sha, ValidatorTaskID: vtask.ID, ChatID: chat.ID, RunID: runID}, nil
}

func (s *validationSeat) validatorProfile() (store.AgentProfile, error) {
	ps, err := s.profReg.List()
	if err != nil {
		return store.AgentProfile{}, err
	}
	for _, p := range ps {
		if p.Name == validatorProfileName {
			return p, nil
		}
	}
	return store.AgentProfile{}, fmt.Errorf("no %q profile: run `smind profile seed roles`", validatorProfileName)
}

// probe reads the validator task's runs and queue.
func (s *validationSeat) probe(taskID int64) validation.ProbeState {
	var ps validation.ProbeState
	rows, err := s.db.ListRunsByTask(taskID, 50)
	if err != nil {
		return ps
	}
	ps.Started = len(rows) > 0
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		if r.Status == string(runs.StatusRunning) {
			ps.Running = true
		}
	}
	if usage, err := s.db.ListRunUsage(ids); err == nil {
		for _, u := range usage {
			if u.CostUSD != nil {
				ps.CostUSD += *u.CostUSD
			}
		}
	}
	if chat, err := s.wm.DefaultChat(taskID); err == nil {
		if items, err := s.db.ListChatQueue(chat.ID); err == nil && len(items) > 0 {
			ps.Queued = true
		}
	}
	return ps
}

func (s *validationSeat) stopTask(taskID int64) {
	rows, err := s.db.ListRunsByTask(taskID, 50)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Status == string(runs.StatusRunning) {
			_ = s.reg.Stop(r.ID)
		}
	}
}
