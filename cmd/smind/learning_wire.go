package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/delivery"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/humanskills"
	"github.com/spacingmind/smind/internal/learning/probe/prevalence"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/learning/redact"
	learningservice "github.com/spacingmind/smind/internal/learning/service"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// humanSkillDirs lists the skill roots whose human/third-party skills the
// gate compares against: the workspace's own and the user-level ones.
func humanSkillDirs(workspacePath, home string) []string {
	var dirs []string
	if workspacePath != "" {
		dirs = append(dirs, filepath.Join(workspacePath, ".claude", "skills"), filepath.Join(workspacePath, ".agents", "skills"))
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".codex", "skills"))
	}
	return dirs
}

// loadHumanSkills lists the human/third-party skills visible to the
// workspace (names and descriptions only): its repo-level and the user-level
// skill directories.
func loadHumanSkills(db *store.Store, workspaceID int64) []gate.LiveSkill {
	var path string
	if ws, err := db.GetWorkspace(workspaceID); err == nil {
		path = ws.Path
	}
	home, _ := os.UserHomeDir()
	skills, _ := humanskills.LoadDirs(humanSkillDirs(path, home)...)
	out := make([]gate.LiveSkill, 0, len(skills))
	for _, s := range skills {
		out = append(out, gate.LiveSkill{Name: s.Name, Description: s.Description, Human: true})
	}
	return out
}

// learningGateConfig maps the learning knobs onto the gate's thresholds.
func learningGateConfig(l config.LearningConfig) gate.Config {
	g := gate.DefaultConfig()
	if l.Similarity.Duplicate > 0 {
		g.Duplicate = l.Similarity.Duplicate
	}
	if l.Similarity.Possible > 0 {
		g.Possible = l.Similarity.Possible
	}
	return g
}

func userHomeDirs(home string) []string {
	dirs := []string{home}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, h)
	}
	return dirs
}

// newLearningRedactor returns the one redactor every learning writer shares.
// It scrubs the exact secrets given (the daemon token), the MCP server
// secrets and the home directories. The MCP list is read per call, so a
// server added after startup is covered at once.
func newLearningRedactor(db *store.Store, secrets, homes []string) func(string) string {
	return func(s string) string {
		all := append([]string(nil), secrets...)
		if rows, err := db.ListMcpServers(); err == nil {
			all = append(all, taskrunner.McpSecrets(rows)...)
		}
		return redact.New(all, homes).String(s)
	}
}

// newApprovalService builds THE approval service of the daemon (ADR-0026):
// the wsapi RPCs, the collector and any other writer share it, so they gate
// and redact identically.
func newApprovalService(lc config.LearningConfig, db *store.Store, home string, secrets []string) *approval.Service {
	return approval.New(approval.Deps{
		Store:      db,
		Human:      func(workspaceID int64) []gate.LiveSkill { return loadHumanSkills(db, workspaceID) },
		Redact:     newLearningRedactor(db, secrets, userHomeDirs(home)),
		GateConfig: func() gate.Config { return learningGateConfig(lc) },
	})
}

// collectorNotifier adapts the wsapi event publisher to the collector's
// Notifier (which has no skill/proposal ids on NotifyChanged).
type collectorNotifier struct{ n wsapi.LearningNotifier }

func (c collectorNotifier) NotifyProposed(ws, id int64, name, kind string) {
	c.n.NotifyProposed(ws, id, name, kind)
}
func (c collectorNotifier) NotifyJob(ws, id int64, status string) { c.n.NotifyJob(ws, id, status) }
func (c collectorNotifier) NotifyChanged(ws int64, reason string) {
	c.n.NotifyChanged(ws, 0, 0, reason)
}

// learningWiring is everything wireLearning needs.
type learningWiring struct {
	Cfg    config.Config
	DB     *store.Store
	API    *wsapi.API
	WM     *workspace.Manager
	Runner *taskrunner.Runner
	Reg    *runs.Registry
	// Home is $SMIND_HOME.
	Home string
	// Secrets are exact strings (the daemon token) the redactor always scrubs.
	Secrets []string
	// Exec overrides the isolated executor (tests); nil builds the real one.
	Exec learningservice.Executor
}

// wireLearning connects the learning pieces (ADR-0026): ONE approval service
// shared by the wsapi RPCs, the collector and delivery; the collector's and
// delivery's notifications onto the event bus; the wsapi reflect/curate
// trigger onto the collector. With learning.enabled false only the wsapi
// RPCs are wired (data stays manageable), the collector and delivery are not
// constructed. The returned stop func closes what was started (idempotent).
func wireLearning(ctx context.Context, w learningWiring) (stop func()) {
	lc := w.Cfg.Learning
	approvals := newApprovalService(lc, w.DB, w.Home, w.Secrets)
	root := filepath.Join(w.Home, "learning", "projection")
	deps := wsapi.LearningDeps{
		Approval: approvals,
		Config: func() wsapi.LearningConfig {
			return wsapi.LearningConfig{
				Enabled: lc.Enabled, ProbationRuns: lc.ProbationRuns,
				DailyBudgetUSD: lc.DailyBudgetUSD, DailyBudgetTokens: lc.DailyBudgetTokens,
			}
		},
	}
	// The prevalence scan is read-only and calls no model, so it is wired
	// even with the collector off (data stays manageable).
	deps.Prevalence = prevalence.New(prevalence.Deps{
		Store:  w.DB,
		Config: func() config.LearningConfig { return lc },
		Home:   w.Home,
	})
	var deliv *delivery.Service
	var coll *learningservice.Service
	if lc.Enabled {
		notifier := w.API.LearningNotifier()
		deliv = delivery.New(delivery.Deps{
			Store:  w.DB,
			Config: func() config.LearningConfig { return lc },
			Root:   root,
			// Delivery only announces its lifecycle archives.
			Notify: func(ws int64, _ string) { notifier.NotifyChanged(ws, 0, 0, "archived") },
		})
		w.Reg.SetDeliveryProvider(deliv)
		w.Reg.AddObserver(deliv)

		exec := w.Exec
		if exec == nil {
			exec = executor.New(w.DB, w.WM, w.Runner, w.Reg, w.Home)
		}
		coll = learningservice.New(learningservice.Deps{
			Store:    w.DB,
			Exec:     exec,
			Approval: approvals,
			Human:    func(workspaceID int64) []gate.LiveSkill { return loadHumanSkills(w.DB, workspaceID) },
			Config:   func() config.LearningConfig { return lc },
			Notify:   collectorNotifier{notifier},
			Home:     w.Home,
		})
		w.Reg.AddObserver(coll)
		coll.Start(ctx)
		deps.Trigger = coll
	}
	// A forgotten or switched-off workspace must not keep a stale projection,
	// also when delivery is not running (data stays manageable).
	deps.OnWorkspaceReset = func(workspaceID int64) {
		var err error
		if deliv != nil {
			err = deliv.Remove(workspaceID)
		} else {
			err = projection.Remove(root, workspaceID)
		}
		if err != nil {
			slog.Warn("learning: remove projection", "workspace", workspaceID, "err", err)
		}
	}
	w.API.SetLearning(deps)

	return func() {
		if coll != nil {
			coll.Close()
		}
		if deliv != nil {
			deliv.Close()
		}
	}
}
