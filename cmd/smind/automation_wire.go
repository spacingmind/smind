package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/spacingmind/smind/internal/automation"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// automationTickPeriod is how often the engine ticks (ADR-0044 section 3).
const automationTickPeriod = 30 * time.Second

// automationStarter launches the task of a firing through the same workspace
// and runs path as the task.create / run.start RPCs. It adds no authority:
// the permission mode is the template's (never autoAccept), the run origin is
// "automation", and the seat policy, depth guard and tree budget apply as for
// any task.
type automationStarter struct {
	wm     *workspace.Manager
	reg    *runs.Registry
	runner *taskrunner.Runner
	db     *store.Store
}

func (s automationStarter) Start(ctx context.Context, req automation.StartRequest) (automation.StartResult, error) {
	prof, err := s.db.GetAgentProfile(req.Def.Template.ProfileID)
	if err != nil {
		return automation.StartResult{}, fmt.Errorf("profile %d: %w", req.Def.Template.ProfileID, err)
	}
	opts := []workspace.CreateTaskOption{}
	if req.Def.Template.BaseRef != "" {
		opts = append(opts, workspace.WithBaseRef(req.Def.Template.BaseRef))
	}
	task, err := s.wm.CreateTask(req.Automation.WorkspaceID, nil, req.Title, opts...)
	if err != nil {
		return automation.StartResult{}, fmt.Errorf("create task: %w", err)
	}
	budget := req.Def.Template.BudgetUSD
	if err := s.db.SetTaskBudget(task.ID, &budget); err != nil {
		return automation.StartResult{}, fmt.Errorf("task %d created but its budget was not saved: %w", task.ID, err)
	}
	chat, err := s.wm.DefaultChat(task.ID)
	if err != nil {
		return automation.StartResult{}, err
	}
	mode := req.Def.Template.Mode
	if mode == "" {
		mode = prof.PermissionMode
	}
	runID, err := s.reg.Start(ctx, s.wm, s.runner, task.ID, chat.ID, taskrunner.Provider(prof.Provider), req.Prompt,
		taskrunner.PermissionSettings{Mode: mode, AutoAccept: false}, taskrunner.ThinkingLevel(prof.ThinkingLevel),
		runs.WithModel(req.Def.Template.Model), runs.WithOrigin(automation.OriginAutomation), runs.WithProfile(prof.ID))
	if err != nil {
		return automation.StartResult{TaskID: task.ID}, fmt.Errorf("start run: %w", err)
	}
	return automation.StartResult{TaskID: task.ID, RunID: runID}, nil
}

func (s automationStarter) Status(taskID int64) (automation.RunStatus, error) {
	if f, ok := s.reg.TaskRunFacts()[taskID]; ok && len(f.RunningRunIDs) > 0 {
		st := "running"
		if f.PendingPermissions > 0 {
			st = "waiting_permission"
		}
		return automation.RunStatus{State: st}, nil
	}
	hist := s.reg.ListTaskHistory(taskID)
	if len(hist) == 0 {
		return automation.RunStatus{State: "running"}, nil // not recorded yet
	}
	latest := hist[0]
	for _, h := range hist[1:] {
		if h.StartedAt.After(latest.StartedAt) {
			latest = h
		}
	}
	out := automation.RunStatus{State: string(latest.Status)}
	if latest.Status == runs.StatusRunning {
		return out, nil
	}
	if events, _, err := s.reg.History(latest.ID); err == nil {
		out.FinalText = taskrunner.FinalText(events)
	}
	if known, unknown, err := s.db.TreeKnownCost(taskID); err == nil && (known > 0 || unknown == 0) {
		out.CostUSD = &known
	}
	return out, nil
}

func (s automationStarter) Stop(taskID int64, reason string) error {
	f, ok := s.reg.TaskRunFacts()[taskID]
	if !ok {
		return nil
	}
	log.Printf("automation: stopping task %d (%s)", taskID, reason)
	for _, id := range f.RunningRunIDs {
		if err := s.reg.Stop(id); err != nil {
			return err
		}
	}
	return nil
}

// automationConfig reads config.yaml on every tick: a human edit applies
// without a restart and an unreadable or invalid file is OFF (fail closed).
func automationConfig() automation.Config {
	cfg, err := config.Load()
	if err != nil {
		return automation.Config{Limits: automation.Limits{MaxBudgetUSD: config.DefaultMaxAutomationBudgetUSD}}
	}
	a := cfg.Automations
	return automation.Config{Enabled: a.Enabled, Paused: a.Paused,
		Limits: automation.Limits{MaxBudgetUSD: a.MaxBudgetUSD, AllowUnattendedModes: a.AllowUnattendedModes}}
}

// wireAutomations builds the engine, binds it to the API (automation.fire) and
// starts its ticker. The ticker idles while automations.enabled is false:
// nothing polls and nothing fires. The returned func stops it.
func wireAutomations(ctx context.Context, db *store.Store, api *wsapi.API, wm *workspace.Manager, runner *taskrunner.Runner, sources ...automation.Source) (stop func()) {
	eng := automation.New(automation.Deps{
		Store:   db,
		Starter: automationStarter{wm: wm, reg: api.Runs, runner: runner, db: db},
		Memory:  automation.Memory{Home: config.Dir()},
		Sources: sources,
		Config:  automationConfig,
		Sink:    api.AutomationSink(),
	})
	api.SetAutomations(eng)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		eng.Run(ctx, automationTickPeriod)
	}()
	return func() {
		cancel()
		<-done
	}
}
