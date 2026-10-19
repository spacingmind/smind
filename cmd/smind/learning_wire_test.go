package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

func TestHumanSkillDirs(t *testing.T) {
	got := humanSkillDirs("/ws", "/home/u")
	want := []string{"/ws/.claude/skills", "/ws/.agents/skills", "/home/u/.claude/skills", "/home/u/.agents/skills", "/home/u/.codex/skills"}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if filepath.ToSlash(got[i]) != want[i] {
			t.Fatalf("dirs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestLearningDeps_HumanSkillsReachTheGate: a workspace skill under
// .claude/skills counts as a human skill, so a proposal reusing its name is
// refused at landing.
func TestLearningDeps_HumanSkillsReachTheGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wsDir := t.TempDir()
	skillDir := filepath.Join(wsDir, ".claude", "skills", "learned-flaky-tempdir")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: learned-flaky-tempdir\ndescription: A human wrote this skill.\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := db.CreateWorkspace(store.Workspace{Path: wsDir, Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	deps := learningDeps(config.Config{Learning: config.DefaultLearning()}, db, []string{"secret"})
	if c := deps.Config(); !c.Enabled || c.ProbationRuns != 25 {
		t.Fatalf("config = %+v", c)
	}
	sk := gate.Skill{Name: "learned-flaky-tempdir", Category: "go", Description: "Use when a Go test fails on a shared TempDir.",
		Body: "Run the failing test alone with -race.\nCheck for a shared t.TempDir across parallel tests.\nAdd t.Parallel guards or separate dirs."}
	p, err := deps.Approval.CreateProposal(approval.NewProposal{
		WorkspaceID: w.ID, Source: "reflector", Kind: "create", Name: sk.Name,
		Items: []approval.Item{{Intent: gate.Intent{Action: gate.ActionCreate, Name: sk.Name}, Result: sk}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ge *approval.GateError
	if _, err := deps.Approval.Approve(p.ID, approval.ApproveOptions{}); !errors.As(err, &ge) {
		t.Fatalf("Approve() err = %v, want *GateError for the human name collision", err)
	}
}
