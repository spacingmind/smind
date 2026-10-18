package main

import (
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/humanskills"
	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/store"
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

// learningDeps builds the wsapi learning collaborators (ADR-0026): the
// approval service with its human-skill loader, redactor and gate
// thresholds. secrets are exact strings (e.g. the daemon token) the
// redactor must always scrub.
func learningDeps(cfg config.Config, db *store.Store, secrets []string) wsapi.LearningDeps {
	home, _ := os.UserHomeDir()
	lc := cfg.Learning
	svc := approval.New(approval.Deps{
		Store: db,
		Human: func(workspaceID int64) []gate.LiveSkill {
			var path string
			if w, err := db.GetWorkspace(workspaceID); err == nil {
				path = w.Path
			}
			skills, _ := humanskills.LoadDirs(humanSkillDirs(path, home)...)
			out := make([]gate.LiveSkill, 0, len(skills))
			for _, s := range skills {
				out = append(out, gate.LiveSkill{Name: s.Name, Description: s.Description, Human: true})
			}
			return out
		},
		Redact: func(s string) string { return redact.New(secrets, []string{home}).String(s) },
		GateConfig: func() gate.Config {
			g := gate.DefaultConfig()
			if lc.Similarity.Duplicate > 0 {
				g.Duplicate = lc.Similarity.Duplicate
			}
			if lc.Similarity.Possible > 0 {
				g.Possible = lc.Similarity.Possible
			}
			return g
		},
	})
	return wsapi.LearningDeps{
		Approval: svc,
		Config: func() wsapi.LearningConfig {
			return wsapi.LearningConfig{Enabled: lc.Enabled, ProbationRuns: lc.ProbationRuns}
		},
	}
}
