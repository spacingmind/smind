package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func newProbeFixture(t *testing.T) (*Store, Workspace, Task) {
	t.Helper()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	return s, ws, task
}

func TestProbeSchemaExists(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	for _, table := range []string{"learning_probes", "learning_probe_arms"} {
		if n := countRows(t, s, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table); n != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
}

func TestProbe_CreateGetUpdateRoundTrip(t *testing.T) {
	t.Parallel()
	s, ws, _ := newProbeFixture(t)

	p, err := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 7, Quick: true, Status: "queued",
		SourceRunID: "run-1", BaseSHA: "abc", EstUSD: 0.5})
	if err != nil {
		t.Fatalf("CreateProbe() error = %v", err)
	}
	if p.ID == 0 || p.StartedAt.IsZero() {
		t.Fatalf("CreateProbe() = %+v, want id and StartedAt set", p)
	}

	got, err := s.GetProbe(p.ID)
	if err != nil {
		t.Fatalf("GetProbe() error = %v", err)
	}
	if got.WorkspaceID != ws.ID || got.ProposalID != 7 || !got.Quick || got.Status != "queued" ||
		got.SourceRunID != "run-1" || got.BaseSHA != "abc" || got.EstUSD != 0.5 {
		t.Fatalf("round trip = %+v", got)
	}

	fin := time.Now().UTC()
	got.Status = "done"
	got.Verdict = "helped"
	got.VerdictReason = "canary improved"
	got.CanaryCostUSD = 0.01
	got.Contrast = `{"k":1}`
	got.FinishedAt = &fin
	if err := s.UpdateProbe(got); err != nil {
		t.Fatalf("UpdateProbe() error = %v", err)
	}
	got2, _ := s.GetProbe(p.ID)
	if got2.Status != "done" || got2.Verdict != "helped" || got2.VerdictReason != "canary improved" ||
		got2.CanaryCostUSD != 0.01 || got2.Contrast != `{"k":1}` || got2.FinishedAt == nil {
		t.Fatalf("after update = %+v", got2)
	}
	// EstUSD and other immutable fields survive the partial update.
	if got2.EstUSD != 0.5 || got2.SourceRunID != "run-1" {
		t.Fatalf("immutable fields changed: %+v", got2)
	}

	if _, err := s.GetProbe(99999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetProbe(missing) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.UpdateProbe(LearningProbe{ID: 99999}); err == nil {
		t.Fatal("UpdateProbe(missing) accepted")
	}
}

func TestProbe_ArmUpsertAndOrder(t *testing.T) {
	t.Parallel()
	s, ws, _ := newProbeFixture(t)
	p, err := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 1, Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []LearningProbeArm{
		{ProbeID: p.ID, Role: "A", Seq: 0, Status: "pending"},
		{ProbeID: p.ID, Role: "B", Seq: 2, Status: "pending"},
		{ProbeID: p.ID, Role: "A2", Seq: 1, Status: "pending"},
	} {
		if err := s.UpsertProbeArm(a); err != nil {
			t.Fatalf("UpsertProbeArm(%s) error = %v", a.Role, err)
		}
	}
	// Upsert replaces the same (probe, role) row.
	now := time.Now().UTC()
	if err := s.UpsertProbeArm(LearningProbeArm{ProbeID: p.ID, Role: "A", Seq: 0, Status: "done",
		RunID: "ra", Metrics: `{"m":1}`, Diff: "d", FinalMessage: "fm", CheckResult: `{"ok":true}`,
		CostUSD: 0.1, StartedAt: &now, FinishedAt: &now}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "SELECT COUNT(*) FROM learning_probe_arms WHERE probe_id = ?", p.ID); n != 3 {
		t.Fatalf("arm rows = %d, want 3 (upsert replaces)", n)
	}
	arms, err := s.ListProbeArms(p.ID)
	if err != nil {
		t.Fatalf("ListProbeArms() error = %v", err)
	}
	if len(arms) != 3 || arms[0].Role != "A" || arms[1].Role != "A2" || arms[2].Role != "B" {
		t.Fatalf("arms order = %v %v %v, want A A2 B by seq", arms[0].Role, arms[1].Role, arms[2].Role)
	}
	a := arms[0]
	if a.RunID != "ra" || a.Status != "done" || a.CostUSD != 0.1 || a.Diff != "d" || a.FinalMessage != "fm" {
		t.Fatalf("updated arm = %+v", a)
	}
}

func TestProbe_LatestForProposal(t *testing.T) {
	t.Parallel()
	s, ws, _ := newProbeFixture(t)
	for _, status := range []string{"failed", "queued"} {
		if _, err := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 42, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	p, ok, err := s.LatestProbeForProposal(42)
	if err != nil || !ok {
		t.Fatalf("LatestProbeForProposal() = %v, %v", ok, err)
	}
	if p.Status != "queued" {
		t.Fatalf("latest = %+v, want the newest (queued)", p)
	}
	if _, ok, err := s.LatestProbeForProposal(99999); err != nil || ok {
		t.Fatalf("LatestProbeForProposal(missing) = %v, %v, want false", ok, err)
	}

	// ListProbes: newest first, default limit.
	for i := 0; i < 3; i++ {
		if _, err := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 43, Status: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListProbes(ws.ID, 0)
	if err != nil {
		t.Fatalf("ListProbes() error = %v", err)
	}
	if len(list) != 5 || list[0].ID <= list[1].ID {
		t.Fatalf("ListProbes() = %d rows, newest %d", len(list), list[0].ID)
	}
}

func TestProbe_ActiveAndFailInterrupted(t *testing.T) {
	t.Parallel()
	s, ws, _ := newProbeFixture(t)
	if _, ok, err := s.ActiveProbe(); err != nil || ok {
		t.Fatalf("ActiveProbe() on empty = %v, %v", ok, err)
	}
	p1, _ := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 1, Status: "running"})
	p2, _ := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 2, Status: "done"})
	p3, _ := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 3, Status: "queued"})
	if err := s.UpsertProbeArm(LearningProbeArm{ProbeID: p1.ID, Role: "A", Seq: 0, Status: "running", RunID: "r-a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProbeArm(LearningProbeArm{ProbeID: p1.ID, Role: "B", Seq: 1, Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProbeArm(LearningProbeArm{ProbeID: p2.ID, Role: "A", Seq: 0, Status: "done"}); err != nil {
		t.Fatal(err)
	}

	act, ok, err := s.ActiveProbe()
	if err != nil || !ok || act.ID != p3.ID {
		t.Fatalf("ActiveProbe() = %+v, %v, %v", act, ok, err)
	}

	n, err := s.FailInterruptedProbes("interrupted by restart")
	if err != nil || n != 2 {
		t.Fatalf("FailInterruptedProbes() = %d, %v; want 2", n, err)
	}
	for _, id := range []int64{p1.ID, p3.ID} {
		got, _ := s.GetProbe(id)
		if got.Status != "failed" || got.Error != "interrupted by restart" || got.FinishedAt == nil {
			t.Fatalf("probe %d after fail = %+v", id, got)
		}
	}
	if got, _ := s.GetProbe(p2.ID); got.Status != "done" {
		t.Fatalf("done probe was touched: %+v", got)
	}
	arms, _ := s.ListProbeArms(p1.ID)
	for _, a := range arms {
		if a.Status != "killed" {
			t.Fatalf("arm %s = %s, want killed", a.Role, a.Status)
		}
	}
	if arms2, _ := s.ListProbeArms(p2.ID); arms2[0].Status != "done" {
		t.Fatalf("done probe's arm was touched: %+v", arms2[0])
	}
	if _, ok, _ := s.ActiveProbe(); ok {
		t.Fatal("ActiveProbe() still reports one after fail")
	}
}

// probeRun seeds a probe-purpose run (in the workspace's task) with usage.
func probeRun(t *testing.T, s *Store, taskID int64, wsID int64, id string, started time.Time, purpose string, cost *float64) {
	t.Helper()
	if _, err := s.CreateRun(Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "done",
		StartedAt: started, Purpose: purpose}); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", id, err)
	}
	if cost != nil {
		if err := s.UpsertRunUsage(RunUsage{RunID: id, TaskID: taskID, Provider: "glm", Source: "test", CostUSD: cost}); err != nil {
			t.Fatalf("UpsertRunUsage(%s) error = %v", id, err)
		}
	}
}

func TestProbe_UsageBucketSeparateFromLearning(t *testing.T) {
	t.Parallel()
	s, ws, task := newProbeFixture(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	one, quarter, probeCost := 1.0, 0.25, 0.40
	probeRun(t, s, task.ID, ws.ID, "ru", base, "user", &one)
	probeRun(t, s, task.ID, ws.ID, "rr", base, "reflect", &quarter)
	probeRun(t, s, task.ID, ws.ID, "rp", base, "probe", &probeCost)

	since := base.Add(-time.Hour)
	got, _, err := s.SumProbeUsageForWorkspace(ws.ID, since)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.40 {
		t.Fatalf("SumProbeUsageForWorkspace() = %v, want 0.40", got)
	}
	// The canary spend joins the probe bucket.
	if _, err := s.CreateProbe(LearningProbe{WorkspaceID: ws.ID, ProposalID: 1, Status: "done",
		CanaryCostUSD: 0.01, StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.SumProbeUsageForWorkspace(ws.ID, since)
	if err != nil {
		t.Fatal(err)
	}
	if got < 0.4099 || got > 0.4101 {
		t.Fatalf("SumProbeUsageForWorkspace() with canary = %v, want 0.41", got)
	}
}

func TestProbe_UsageUnreportedCounted(t *testing.T) {
	t.Parallel()
	s, ws, task := newProbeFixture(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	probeRun(t, s, task.ID, ws.ID, "r1", base, "probe", nil) // no usage row
	probeRun(t, s, task.ID, ws.ID, "r2", base, "probe", nil)

	_, unreported, err := s.SumProbeUsageForWorkspace(ws.ID, base.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if unreported != 2 {
		t.Fatalf("unreported = %d, want 2", unreported)
	}
}

func TestProbe_UsageWindowAndWorkspaceScoped(t *testing.T) {
	t.Parallel()
	s, ws, task := newProbeFixture(t)
	other := newTestWorkspace(t, s, "/other")
	otherTask, err := s.CreateTask(Task{WorkspaceID: other.ID, Title: "t2", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := 0.5
	probeRun(t, s, task.ID, ws.ID, "in", base, "probe", &c)
	probeRun(t, s, task.ID, ws.ID, "out", base.Add(-2*time.Hour), "probe", &c) // before window
	probeRun(t, s, otherTask.ID, other.ID, "ws2", base, "probe", &c)           // other workspace

	got, _, err := s.SumProbeUsageForWorkspace(ws.ID, base.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.5 {
		t.Fatalf("SumProbeUsageForWorkspace() = %v, want 0.5 (window + workspace scoped)", got)
	}
}

func TestLearningBucket_ExcludesProbeRuns(t *testing.T) {
	t.Parallel()
	s, ws, task := newProbeFixture(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	one, quarter, probeCost := 1.0, 0.25, 0.40
	probeRun(t, s, task.ID, ws.ID, "ru", base, "user", &one)
	probeRun(t, s, task.ID, ws.ID, "rr", base, "reflect", &quarter)
	probeRun(t, s, task.ID, ws.ID, "rp", base, "probe", &probeCost)
	since := base.Add(-time.Hour)

	lc, _, _, err := s.SumLearningUsageForWorkspace(ws.ID, since)
	if err != nil {
		t.Fatal(err)
	}
	if lc != 0.25 {
		t.Fatalf("SumLearningUsageForWorkspace() = %v, want 0.25 (probe runs excluded)", lc)
	}

	lRuns, _, _, lCost, err := s.SummarizeLearningUsageWindow(&since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lRuns != 1 || lCost != 0.25 {
		t.Fatalf("SummarizeLearningUsageWindow() = %d runs, %v USD; want 1, 0.25", lRuns, lCost)
	}
	pRuns, _, _, pCost, err := s.SummarizeProbeUsageWindow(&since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pRuns != 1 || pCost != 0.40 {
		t.Fatalf("SummarizeProbeUsageWindow() = %d runs, %v USD; want 1, 0.40", pRuns, pCost)
	}
}
