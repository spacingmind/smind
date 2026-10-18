package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/humanskills"
	"github.com/spacingmind/smind/internal/learning/redact"
	learningservice "github.com/spacingmind/smind/internal/learning/service"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// newLearningService builds the learning collector (ADR-0026), or returns nil
// when the global kill switch (learning.enabled) is off, in which case
// nothing is constructed and nothing may observe runs. home is $SMIND_HOME.
func newLearningService(cfg config.Config, db *store.Store, wm *workspace.Manager, runner *taskrunner.Runner, reg *runs.Registry, home string) *learningservice.Service {
	if !cfg.Learning.Enabled {
		return nil
	}
	human := func(workspaceID int64) []gate.LiveSkill { return loadHumanSkills(db, workspaceID) }
	redactor := redact.New(nil, userHomeDirs(home))
	approvals := approval.New(approval.Deps{
		Store: db, Human: human, Redact: redactor.String,
		GateConfig: func() gate.Config { return learningGateConfig(cfg.Learning) },
	})
	return learningservice.New(learningservice.Deps{
		Store:    db,
		Exec:     executor.New(db, wm, runner, reg, home),
		Approval: approvals,
		Human:    human,
		Config:   func() config.LearningConfig { return cfg.Learning },
		Home:     home,
	})
}

// startLearning wires the collector into the daemon: it registers the
// service as a run observer and starts it. The returned func closes it; both
// are no-ops when learning is disabled.
func startLearning(ctx context.Context, cfg config.Config, db *store.Store, wm *workspace.Manager, runner *taskrunner.Runner, reg *runs.Registry) (stop func()) {
	svc := newLearningService(cfg, db, wm, runner, reg, config.Dir())
	if svc == nil {
		return func() {}
	}
	reg.AddObserver(svc)
	svc.Start(ctx)
	return svc.Close
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

// loadHumanSkills lists the human/third-party skills visible to the
// workspace (names and descriptions only): its repo-level and the user-level
// skill directories.
func loadHumanSkills(db *store.Store, workspaceID int64) []gate.LiveSkill {
	var dirs []string
	if ws, err := db.GetWorkspace(workspaceID); err == nil && ws.Path != "" {
		dirs = append(dirs, filepath.Join(ws.Path, ".claude", "skills"), filepath.Join(ws.Path, ".agents", "skills"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, ".claude", "skills"), filepath.Join(h, ".agents", "skills"), filepath.Join(h, ".codex", "skills"))
	}
	skills, _ := humanskills.LoadDirs(dirs...)
	out := make([]gate.LiveSkill, 0, len(skills))
	for _, s := range skills {
		out = append(out, gate.LiveSkill{Name: s.Name, Description: s.Description, Human: true})
	}
	return out
}
