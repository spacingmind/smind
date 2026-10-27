package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/automation"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

type starterEnv struct {
	st      *store.Store
	reg     *runs.Registry
	starter automationStarter
	ws      store.Workspace
	lim     *automation.Limits
}

// newStarterEnv wires the REAL automationStarter over a real workspace manager,
// store, run registry and runner whose "glm" provider is the fake ACP agent
// the other cmd/smind tests use.
func newStarterEnv(t *testing.T) *starterEnv {
	t.Helper()
	fakeAgent := buildFakeAgent(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	wm := workspace.New(st)
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeAgent, "modes:session", "models"}))
	api, err := wsapi.New(wm, accounts.New(st), runner, st, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close) // LIFO: runs stop before the store closes

	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "core.autocrlf", "false"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}, {"commit", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	lim := &automation.Limits{MaxBudgetUSD: 10}
	return &starterEnv{st: st, reg: api.Runs, ws: ws, lim: lim,
		starter: automationStarter{wm: wm, reg: api.Runs, runner: runner, db: st, limits: func() automation.Limits { return *lim }}}
}

func (e *starterEnv) req(profile store.AgentProfile, tpl automation.Template) automation.StartRequest {
	tpl.ProfileID = profile.ID
	return automation.StartRequest{
		Automation: store.Automation{WorkspaceID: e.ws.ID},
		Def:        automation.Def{Name: "a", WorkspaceID: e.ws.ID, Template: tpl},
		Title:      "Windows full suite failed on develop @ abc1234",
		Prompt:     "hello from an automation",
	}
}

func (e *starterEnv) profile(t *testing.T, p store.AgentProfile) store.AgentProfile {
	t.Helper()
	p.Provider = "glm"
	if p.Name == "" {
		p.Name = "lead"
	}
	out, err := e.st.CreateAgentProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// settled waits for the task's run to leave "running" so cleanup is quiet.
func (e *starterEnv) summary(t *testing.T, taskID int64) runs.RunSummary {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		hist := e.reg.ListTaskHistory(taskID)
		if len(hist) == 1 && hist[0].Status != runs.StatusRunning {
			return hist[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not settle: %+v", hist)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAutomationStarter_StartsRunUnattendedNeverAutoAccept drives the real
// adapter: the run it starts carries the template's permission mode, never
// autoAccept (even for a profile that has it, when the opt-in is on), origin
// "automation", the template's model, and the budget is set on the root task.
func TestAutomationStarter_StartsRunUnattendedNeverAutoAccept(t *testing.T) {
	e := newStarterEnv(t)
	e.lim.AllowUnattendedModes = true // so a profile with autoAccept is even startable
	prof := e.profile(t, store.AgentProfile{PermissionMode: "default", AutoAccept: true})
	res, err := e.starter.Start(context.Background(), e.req(prof, automation.Template{Mode: "accept_edits", Model: "fake-model-b", BudgetUSD: 2.5}))
	if err != nil {
		t.Fatal(err)
	}
	got := e.summary(t, res.TaskID)
	if got.AutoAccept {
		t.Fatal("the automation's run was started with autoAccept: a permission request would be auto-approved")
	}
	if got.PermissionMode != "accept_edits" {
		t.Fatalf("permission mode = %q, want the template's accept_edits", got.PermissionMode)
	}
	if got.Origin != automation.OriginAutomation {
		t.Fatalf("origin = %q, want %q", got.Origin, automation.OriginAutomation)
	}
	if got.Model != "fake-model-b" {
		t.Fatalf("model = %q, want the template's", got.Model)
	}
	if got.ProfileID != prof.ID {
		t.Fatalf("profile = %d", got.ProfileID)
	}
	task, err := e.st.GetTask(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.BudgetUSD == nil || *task.BudgetUSD != 2.5 {
		t.Fatalf("budget on the root task = %v, want 2.5", task.BudgetUSD)
	}
	if task.ParentTaskID != nil {
		t.Fatalf("an automation task must be a root, parent = %v", *task.ParentTaskID)
	}
	if !strings.HasPrefix(task.Title, "Windows full suite failed") {
		t.Fatalf("title = %q", task.Title)
	}
}

func TestAutomationStarter_EmptyTemplateModeUsesTheProfilesNeverWider(t *testing.T) {
	e := newStarterEnv(t)
	prof := e.profile(t, store.AgentProfile{PermissionMode: "default"})
	res, err := e.starter.Start(context.Background(), e.req(prof, automation.Template{Model: "fake-model-a", BudgetUSD: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.summary(t, res.TaskID); got.PermissionMode != "default" || got.AutoAccept {
		t.Fatalf("mode = %q autoAccept = %v, want the profile's default and no autoAccept", got.PermissionMode, got.AutoAccept)
	}
}

// A row edited behind the RPCs' back (an unattended mode, or an autoAccept
// profile) must not start a task unless automations.allowUnattendedModes is
// on; the refusal happens before any task or worktree exists.
func TestAutomationStarter_RefusesUnattendedWithoutOptIn_NoTaskCreated(t *testing.T) {
	e := newStarterEnv(t)
	risky := e.profile(t, store.AgentProfile{Name: "risky", PermissionMode: "default", AutoAccept: true})
	safe := e.profile(t, store.AgentProfile{Name: "safe", PermissionMode: "default"})
	cases := map[string]automation.StartRequest{
		"autoAccept profile":      e.req(risky, automation.Template{Mode: "default", Model: "fake-model-a", BudgetUSD: 1}),
		"bypass template mode":    e.req(safe, automation.Template{Mode: "bypassPermissions", Model: "fake-model-a", BudgetUSD: 1}),
		"full-access mode":        e.req(safe, automation.Template{Mode: "full-access", Model: "fake-model-a", BudgetUSD: 1}),
		"inherited autoAccept":    e.req(risky, automation.Template{Model: "fake-model-a", BudgetUSD: 1}),
		"template autoAccept set": e.req(safe, automation.Template{Mode: "default", AutoAccept: true, Model: "fake-model-a", BudgetUSD: 1}),
	}
	for name, req := range cases {
		if _, err := e.starter.Start(context.Background(), req); err == nil || !strings.Contains(err.Error(), "allowUnattendedModes") {
			t.Errorf("%s: err = %v, want an allowUnattendedModes refusal", name, err)
		}
	}
	tasks, err := e.st.ListTasksByWorkspace(e.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("a refused launch left %d tasks behind", len(tasks))
	}
}

// Fire now reaches the adapter on an RPC context that is cancelled as soon as
// the handler returns; the run it started must not die with it (found by the
// live pilot run: fire-now tasks ended "initialize: context canceled").
func TestAutomationStarter_RunOutlivesTheCallersContext(t *testing.T) {
	e := newStarterEnv(t)
	prof := e.profile(t, store.AgentProfile{PermissionMode: "default"})
	ctx, cancel := context.WithCancel(context.Background())
	res, err := e.starter.Start(ctx, e.req(prof, automation.Template{Mode: "default", Model: "fake-model-a", BudgetUSD: 1}))
	cancel() // the RPC handler returned
	if err != nil {
		t.Fatal(err)
	}
	if got := e.summary(t, res.TaskID); got.Status != runs.StatusDone {
		t.Fatalf("run status = %q (%s), want done: the caller's context cancelled the agent", got.Status, got.Err)
	}
}
