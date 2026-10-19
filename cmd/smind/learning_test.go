package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

func learningTestDeps(t *testing.T) (*store.Store, *workspace.Manager, *runs.Registry) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	reg, err := runs.New(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, workspace.New(db), reg
}

// TestServe_LearningWiringOffByDefault: with the kill switch off nothing is
// constructed and nothing is registered as an observer; the stop func is a
// safe no-op (it is deferred unconditionally by cmdServe).
func TestServe_LearningWiringOffByDefault(t *testing.T) {
	db, wm, reg := learningTestDeps(t)
	cfg := config.Config{Learning: config.DefaultLearning()}
	cfg.Learning.Enabled = false

	if svc := newLearningService(cfg, db, wm, nil, reg, t.TempDir()); svc != nil {
		t.Fatalf("service constructed with learning disabled: %v", svc)
	}
	stop := startLearning(context.Background(), cfg, db, wm, nil, reg)
	if stop == nil {
		t.Fatal("stop func is nil")
	}
	stop()
}

func TestServe_LearningWiringEnabled(t *testing.T) {
	db, wm, reg := learningTestDeps(t)
	t.Setenv("SMIND_HOME", t.TempDir())
	cfg := config.Config{Learning: config.DefaultLearning()}
	if svc := newLearningService(cfg, db, wm, nil, reg, t.TempDir()); svc == nil {
		t.Fatal("no service with learning enabled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := startLearning(ctx, cfg, db, wm, nil, reg)
	stop()
	stop() // idempotent
}

func TestLearningGateConfigAndHumanSkills(t *testing.T) {
	l := config.DefaultLearning()
	l.Similarity = config.LearningSimilarity{Duplicate: 0.9, Possible: 0.7}
	if g := learningGateConfig(l); g.Duplicate != 0.9 || g.Possible != 0.7 || g.Mode != gate.ModeReflect {
		t.Fatalf("gate config = %+v", g)
	}

	db, _, _ := learningTestDeps(t)
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".claude", "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: deploy\ndescription: Use when deploying.\n---\nSECRET BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := db.CreateWorkspace(store.Workspace{Path: repo, Title: "r", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	got := loadHumanSkills(db, ws.ID)
	if len(got) != 1 || got[0].Name != "deploy" || !got[0].Human || got[0].Body != "" || got[0].Description != "Use when deploying." {
		t.Fatalf("human skills = %+v", got)
	}
}
