package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

func TestLearningGateConfigAndHumanSkills(t *testing.T) {
	l := config.DefaultLearning()
	l.Similarity = config.LearningSimilarity{Duplicate: 0.9, Possible: 0.7}
	if g := learningGateConfig(l); g.Duplicate != 0.9 || g.Possible != 0.7 || g.Mode != gate.ModeReflect {
		t.Fatalf("gate config = %+v", g)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
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
	setHome(t, t.TempDir())
	got := loadHumanSkills(db, ws.ID)
	if len(got) != 1 || got[0].Name != "deploy" || !got[0].Human || got[0].Body != "" || got[0].Description != "Use when deploying." {
		t.Fatalf("human skills = %+v", got)
	}
}
