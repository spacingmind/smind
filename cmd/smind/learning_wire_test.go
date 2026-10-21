package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
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
	approvals := newApprovalService(config.DefaultLearning(), db, t.TempDir(), []string{"secret"})
	sk := gate.Skill{Name: "learned-flaky-tempdir", Category: "go", Description: "Use when a Go test fails on a shared TempDir.",
		Body: "Run the failing test alone with -race.\nCheck for a shared t.TempDir across parallel tests.\nAdd t.Parallel guards or separate dirs."}
	p, err := approvals.CreateProposal(approval.NewProposal{
		WorkspaceID: w.ID, Source: "reflector", Kind: "create", Name: sk.Name,
		Items: []approval.Item{{Intent: gate.Intent{Action: gate.ActionCreate, Name: sk.Name, Reason: "because"}, Result: sk}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ge *approval.GateError
	if _, err := approvals.Approve(p.ID, approval.ApproveOptions{}); !errors.As(err, &ge) {
		t.Fatalf("Approve() err = %v, want *GateError for the human name collision", err)
	}
}

// TestWireLearning_ProbeServiceWired: the probe service is constructed and
// served even with learning disabled — the estimate answers eligible:false
// with a reason (learning_off), no model call — and stop is safe twice.
func TestWireLearning_ProbeServiceWired(t *testing.T) {
	f := newWiringFx(t, false)
	w, _, _ := f.workspaceWithChat("propose")
	p, err := f.db.CreateProposal(store.LearnedProposal{WorkspaceID: w.ID, Kind: "create", Source: "reflector", Name: "learned-x"})
	if err != nil {
		t.Fatal(err)
	}
	var est struct {
		Eligible bool `json:"eligible"`
		Reasons  []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"reasons"`
	}
	f.ok("learning.probe.estimate", map[string]any{"id": p.ID}, &est)
	if est.Eligible || len(est.Reasons) == 0 || est.Reasons[0].Code == "" {
		t.Fatalf("estimate = %+v, want eligible:false with a reason", est)
	}
	if f.exec.count() != 0 {
		t.Fatalf("estimate made %d executor call(s); it must call no model", f.exec.count())
	}
	f.stop()
	f.stop() // idempotent
}

// TestCachedQuotaPercent: the highest cached claude-account window usage,
// and nothing when no snapshot exists.
func TestCachedQuotaPercent(t *testing.T) {
	f := newWiringFx(t, false)
	if p, ok := cachedQuotaPercent(f.db); ok {
		t.Fatalf("cachedQuotaPercent() = %v, true; want (0, false) with nothing cached", p)
	}
	mk := func(provider string, pct float64) {
		t.Helper()
		acc, err := f.db.CreateAccount(store.Account{Provider: provider, Label: provider, CredentialType: "api_key"})
		if err != nil {
			t.Fatal(err)
		}
		data, err := quota.EncodeReport(quota.Report{Windows: []quota.Window{{ID: "five_hour", UsedPct: &pct}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.CreateQuotaSnapshot(store.QuotaSnapshot{AccountID: acc.ID, UsageData: data,
			PolledAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	mk(string(taskrunner.ProviderClaudeNative), 30)
	mk(string(taskrunner.ProviderClaudeNative), 72)
	mk("other-provider", 99)
	p, ok := cachedQuotaPercent(f.db)
	if !ok || p != 72 {
		t.Fatalf("cachedQuotaPercent() = %v, %v; want 72, true", p, ok)
	}
}
