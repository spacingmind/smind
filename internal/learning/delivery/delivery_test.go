package delivery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type fixture struct {
	t    *testing.T
	st   *store.Store
	svc  *Service
	root string
	ws   store.Workspace
	task store.Task
	cfg  config.LearningConfig

	mu       sync.Mutex
	notified []string
	runSeq   int
}

func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{t: t, st: st, root: filepath.Join(t.TempDir(), "projection"), cfg: config.DefaultLearning()}
	f.ws, err = st.CreateWorkspace(store.Workspace{Path: "/ws", Title: "ws", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		if err := st.SetWorkspaceLearningMode(f.ws.ID, mode); err != nil {
			t.Fatal(err)
		}
	}
	f.task, err = st.CreateTask(store.Task{WorkspaceID: f.ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = New(Deps{Store: st, Root: f.root, Config: func() config.LearningConfig { return f.cfg },
		Notify: func(_ int64, reason string) {
			f.mu.Lock()
			f.notified = append(f.notified, reason)
			f.mu.Unlock()
		}})
	t.Cleanup(f.svc.Close)
	// Projections are read-only trees: Remove them before TempDir cleanup.
	t.Cleanup(func() { _ = f.svc.Remove(f.ws.ID) })
	return f
}

func (f *fixture) land(name string) store.LearnedSkill {
	f.t.Helper()
	k, err := f.st.LandSkill(store.LandSkillInput{
		Skill: store.LearnedSkill{WorkspaceID: f.ws.ID, Name: "learned-" + name, Description: "d " + name, Body: "body " + name, Source: "human"},
		Actor: "human",
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return k
}

func (f *fixture) setStatus(k store.LearnedSkill, status string) {
	f.t.Helper()
	if err := f.st.SetSkillStatus(k.ID, status, "human", "", ""); err != nil {
		f.t.Fatal(err)
	}
}

// run creates a run row with the given tool-call events and returns the id.
func (f *fixture) run(provider string, events ...taskrunner.Event) string {
	f.t.Helper()
	f.runSeq++
	id := "run-" + strconv.Itoa(f.runSeq)
	if _, err := f.st.CreateRun(store.Run{ID: id, TaskID: f.task.ID, Provider: provider, Prompt: "p", Status: "done", StartedAt: time.Now()}); err != nil {
		f.t.Fatal(err)
	}
	for i, e := range events {
		b, _ := json.Marshal(map[string]any{"type": e.Type, "toolName": e.ToolName, "toolInput": e.ToolInput})
		if _, err := f.st.AppendRunEvent(id, int64(i), string(b)); err != nil {
			f.t.Fatal(err)
		}
	}
	return id
}

func toolCall(tool, input string) taskrunner.Event {
	return taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolName: tool, ToolInput: json.RawMessage(input)}
}

// finish notifies the service and waits for its worker to drain.
func (f *fixture) finish(fin runs.RunFinished) {
	f.t.Helper()
	f.svc.OnRunFinished(fin)
	f.svc.Close()
	f.svc = New(f.svc.d) // fresh worker for the next call
	f.t.Cleanup(f.svc.Close)
}

func (f *fixture) finished(runID, provider string, ids []int64, measured bool) runs.RunFinished {
	return runs.RunFinished{RunID: runID, TaskID: f.task.ID, Provider: provider, Purpose: "user", Status: "done", OfferedSkillIDs: ids, OfferedMeasured: measured}
}

func (f *fixture) skill(id int64) store.LearnedSkill {
	f.t.Helper()
	k, err := f.st.GetLearnedSkill(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return k
}

func prepare(f *fixture, p taskrunner.Provider) (taskrunner.Delivery, runs.OfferedInfo) {
	return f.svc.Prepare(context.Background(), f.ws.ID, "r", p, "")
}

func TestProjection_OnlyActiveSkills(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	active := f.land("active-one")
	disabled := f.land("disabled-one")
	f.setStatus(disabled, store.SkillDisabled)
	archived := f.land("archived-one")
	f.setStatus(archived, store.SkillArchived)
	// A pending proposal never reaches delivery either.
	if _, err := f.st.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: "create", Source: "reflector", Name: "proposed-one", Intents: "[]", Status: "pending"}); err != nil {
		t.Fatal(err)
	}

	d, info := prepare(f, taskrunner.ProviderClaudeNative)
	if len(d.AddDirs) != 1 || len(info.SkillIDs) != 1 || info.SkillIDs[0] != active.ID || !info.Measured {
		t.Fatalf("claude delivery = %+v info = %+v", d, info)
	}
	entries, err := os.ReadDir(filepath.Join(d.AddDirs[0], ".claude", "skills"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "learned-active-one" {
		t.Fatalf("projected skills = %v err=%v, want only active-one", entries, err)
	}

	d, info = prepare(f, taskrunner.ProviderGLM)
	for _, n := range []string{"disabled-one", "archived-one", "proposed-one"} {
		if strings.Contains(d.Preface, n) {
			t.Fatalf("preface contains %s: %q", n, d.Preface)
		}
	}
	if !strings.Contains(d.Preface, "active-one") || info.Measured {
		t.Fatalf("glm delivery = %+v info = %+v", d, info)
	}
}

func TestProposedSkillNeverDelivered(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	if _, err := f.st.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: "create", Source: "reflector", Name: "only-proposed", Intents: "[]", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []taskrunner.Provider{taskrunner.ProviderClaudeNative, taskrunner.ProviderGLM, taskrunner.ProviderKimi} {
		if d, info := prepare(f, p); !d.Empty() || len(info.SkillIDs) != 0 {
			t.Fatalf("%s: delivered %+v %+v with nothing approved", p, d, info)
		}
	}
	if _, err := os.Stat(f.root); !os.IsNotExist(err) {
		t.Fatalf("projection root created with nothing to project: %v", err)
	}
}

func TestDelivery_OffWhenModeOffOrDisabled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mode    string
		enabled bool
	}{{"mode off", store.LearningModeOff, true}, {"disabled", store.LearningModePropose, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tc.mode)
			f.land("a")
			f.cfg.Enabled = tc.enabled
			for _, p := range []taskrunner.Provider{taskrunner.ProviderClaudeNative, taskrunner.ProviderGLM} {
				if d, info := prepare(f, p); !d.Empty() || len(info.SkillIDs) != 0 {
					t.Fatalf("%s delivered %+v", p, d)
				}
			}
			if _, err := os.Stat(f.root); !os.IsNotExist(err) {
				t.Fatalf("projection dir created: %v", err)
			}
		})
	}
}

// Turning learning off keeps every row and counter; only delivery stops.
func TestLearningOff_KeepsDataStopsDelivery(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	if d, _ := prepare(f, taskrunner.ProviderClaudeNative); d.Empty() {
		t.Fatal("no delivery while on")
	}
	if err := f.st.SetWorkspaceLearningMode(f.ws.ID, store.LearningModeOff); err != nil {
		t.Fatal(err)
	}
	if d, _ := prepare(f, taskrunner.ProviderClaudeNative); !d.Empty() {
		t.Fatalf("delivery after off: %+v", d)
	}
	if got := f.skill(k.ID); got.Status != store.SkillActive {
		t.Fatalf("skill status = %s, want active (data kept)", got.Status)
	}
}

func TestDelivery_CodexAndSystemUntouched(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	f.land("a")
	if d, info := prepare(f, taskrunner.ProviderCodexNative); !d.Empty() || len(info.SkillIDs) != 0 {
		t.Fatalf("codex delivered %+v", d)
	}
	if d, _ := f.svc.Prepare(context.Background(), f.ws.ID, "r", taskrunner.ProviderClaudeNative, "reflect"); !d.Empty() {
		t.Fatalf("system run delivered %+v", d)
	}
}

// ClaudeIndexPreface (D8) is off by default and a no-op in v1 either way.
func TestDelivery_ClaudeIndexPrefaceOffAndNoOp(t *testing.T) {
	t.Parallel()
	if config.DefaultLearning().ClaudeIndexPreface {
		t.Fatal("ClaudeIndexPreface defaults to on")
	}
	f := newFixture(t, store.LearningModePropose)
	f.land("a")
	off, _ := prepare(f, taskrunner.ProviderClaudeNative)
	f.cfg.ClaudeIndexPreface = true
	on, _ := prepare(f, taskrunner.ProviderClaudeNative)
	if off.Preface != "" || on.Preface != "" || len(on.AddDirs) != 1 {
		t.Fatalf("off=%+v on=%+v, want no preface and a projection dir either way", off, on)
	}
}

func TestDelivery_PrefaceBudgetAndLoadRateOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	low := f.land("low-rate")
	high := f.land("high-rate")
	// low: offered 10 / loaded 0; high: offered 10 / loaded 9.
	for i := 0; i < 10; i++ {
		id := "x" + strconv.Itoa(i)
		if err := f.st.RecordOffered(id, []int64{low.ID, high.ID}, true); err != nil {
			t.Fatal(err)
		}
		if i < 9 {
			if err := f.st.RecordLoad(id, high.ID, store.LoadViaSkill); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.land("filler")
	f.cfg.PrefaceBudgetBytes = 400
	d, _ := prepare(f, taskrunner.ProviderGLM)
	if len(d.Preface) > 400 {
		t.Fatalf("preface %d bytes exceeds the 400-byte budget", len(d.Preface))
	}
	hi, lo := strings.Index(d.Preface, "high-rate"), strings.Index(d.Preface, "low-rate")
	if hi < 0 || (lo >= 0 && hi > lo) {
		t.Fatalf("high-rate skill must come first (hi=%d lo=%d):\n%s", hi, lo, d.Preface)
	}
}

func TestCounters_ReflectorRunsNeverCount(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	id := f.run("claude-native")
	fin := f.finished(id, "claude-native", []int64{k.ID}, true)
	fin.Purpose = "reflect"
	f.finish(fin)
	if got := f.skill(k.ID); got.OfferedRuns != 0 {
		t.Fatalf("offered_runs = %d, want 0", got.OfferedRuns)
	}
}

func TestCounters_OfferedOncePerRunE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	id := f.run("claude-native")
	fin := f.finished(id, "claude-native", []int64{k.ID}, true)
	f.finish(fin)
	f.finish(fin) // re-notified
	if got := f.skill(k.ID); got.OfferedRuns != 1 || !got.Measured {
		t.Fatalf("offered_runs = %d measured=%v, want 1/true", got.OfferedRuns, got.Measured)
	}
}

func TestCounters_SkillToolLoadE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	other := f.land("b")
	id := f.run("claude-native",
		toolCall("Skill", `{"skill":"learned-a"}`), toolCall("Skill", `{"skill":"learned-a"}`), toolCall("Skill", `{"skill":"unlearned"}`))
	f.finish(f.finished(id, "claude-native", []int64{k.ID, other.ID}, true))
	got := f.skill(k.ID)
	if got.OfferedRuns != 1 || got.LoadedRuns != 1 || got.Loads != 1 || got.LoadsViaSkill != 1 {
		t.Fatalf("a = %+v", got)
	}
	if o := f.skill(other.ID); o.OfferedRuns != 1 || o.LoadedRuns != 0 {
		t.Fatalf("b = %+v", o)
	}
}

func TestCounters_ReadUnderProjectionLoadE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	d, info := prepare(f, taskrunner.ProviderClaudeNative)
	read := filepath.Join(d.AddDirs[0], ".claude", "skills", "learned-a", "SKILL.md")
	outside := filepath.Join(f.root, "elsewhere", ".claude", "skills", "learned-a", "SKILL.md")
	id := f.run("claude-native",
		toolCall("Read", `{"file_path":`+strconv.Quote(outside)+`}`),
		toolCall("Read", `{"file_path":`+strconv.Quote(read)+`}`))
	f.finish(f.finished(id, "claude-native", info.SkillIDs, info.Measured))
	got := f.skill(k.ID)
	if got.LoadsViaRead != 1 || got.LoadsViaSkill != 0 || got.LoadedRuns != 1 {
		t.Fatalf("counters = %+v", got)
	}
}

func TestCounters_UnmeasuredGlmOffer(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	d, info := prepare(f, taskrunner.ProviderGLM)
	if d.Preface == "" || info.Measured {
		t.Fatalf("glm delivery = %+v info = %+v", d, info)
	}
	// Even a (hypothetical) Skill-looking tool call must not count for an
	// unmeasured provider.
	id := f.run("glm", toolCall("Skill", `{"skill":"learned-a"}`))
	f.finish(f.finished(id, "glm", info.SkillIDs, info.Measured))
	got := f.skill(k.ID)
	if got.OfferedRuns != 1 || got.Measured || got.Loads != 0 || got.LoadedRuns != 0 {
		t.Fatalf("counters = %+v", got)
	}
}

func offerMany(t *testing.T, f *fixture, ids []int64, n int, measured bool) {
	t.Helper()
	for i := 0; i < n; i++ {
		rid := f.run("claude-native")
		if err := f.st.RecordOffered(rid, ids, measured); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLifecycle_GraduationArchivesZeroLoadE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	offerMany(t, f, []int64{k.ID}, 24, true)
	id := f.run("claude-native")
	f.finish(f.finished(id, "claude-native", []int64{k.ID}, true)) // the 25th measured offer
	got := f.skill(k.ID)
	if got.OfferedRuns != 25 || got.Status != store.SkillArchived {
		t.Fatalf("skill = offered %d status %s, want 25/archived", got.OfferedRuns, got.Status)
	}
	led, _ := f.st.ListLedger(f.ws.ID, k.ID, 0)
	found := false
	for _, e := range led {
		if e.Action == "archive" && e.Actor == "system" && e.Reason == "graduation_zero_load" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no system archive ledger row: %+v", led)
	}
	if len(f.notified) != 1 || f.notified[0] != "graduation_zero_load" {
		t.Fatalf("notified = %v", f.notified)
	}
}

func TestLifecycle_PinnedExemptE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	if err := f.st.SetSkillPinned(k.ID, true, "human"); err != nil {
		t.Fatal(err)
	}
	offerMany(t, f, []int64{k.ID}, 24, true)
	f.finish(f.finished(f.run("claude-native"), "claude-native", []int64{k.ID}, true))
	if got := f.skill(k.ID); got.Status != store.SkillActive {
		t.Fatalf("pinned skill status = %s", got.Status)
	}
}

func TestLifecycle_UnmeasuredNeverArchivedE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	offerMany(t, f, []int64{k.ID}, 24, false)
	f.finish(f.finished(f.run("glm"), "glm", []int64{k.ID}, false))
	got := f.skill(k.ID)
	if got.OfferedRuns != 25 || got.Measured || got.Status != store.SkillActive {
		t.Fatalf("skill = offered %d measured %v status %s, want 25/false/active", got.OfferedRuns, got.Measured, got.Status)
	}
}

func TestLifecycle_CapacityE2E(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	f.cfg.Capacity = 1
	f.cfg.ProbationRuns = 2
	a, b := f.land("a"), f.land("b")
	// Both mature (2 offers), both loaded at least once; b loaded more.
	for i := 0; i < 2; i++ {
		rid := f.run("claude-native")
		_ = f.st.RecordOffered(rid, []int64{a.ID, b.ID}, true)
		_ = f.st.RecordLoad(rid, b.ID, store.LoadViaSkill)
		if i == 0 {
			_ = f.st.RecordLoad(rid, a.ID, store.LoadViaSkill)
		}
	}
	f.finish(f.finished(f.run("claude-native"), "claude-native", []int64{a.ID}, true))
	if got := f.skill(a.ID); got.Status != store.SkillArchived {
		t.Fatalf("a (lower load rate) status = %s, want archived by capacity", got.Status)
	}
	if got := f.skill(b.ID); got.Status != store.SkillActive {
		t.Fatalf("b status = %s, want active", got.Status)
	}
	led, _ := f.st.ListLedger(f.ws.ID, a.ID, 0)
	ok := false
	for _, e := range led {
		ok = ok || (e.Actor == "system" && e.Reason == "capacity")
	}
	if !ok {
		t.Fatalf("no capacity ledger row: %+v", led)
	}
}

func TestService_OnRunFinishedNonBlockingAndPanicSafe(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	f.svc.d.Notify = func(int64, string) { panic("notify boom") }
	k := f.land("a")
	f.cfg.ProbationRuns = 1
	rid := f.run("claude-native")
	fin := f.finished(rid, "claude-native", []int64{k.ID}, true)
	done := make(chan struct{})
	go func() { f.svc.OnRunFinished(fin); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnRunFinished blocked")
	}
	f.svc.Close()            // drains; the notify panic must not escape the worker
	f.svc.OnRunFinished(fin) // after Close: dropped, no panic
}

func TestService_Remove(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	f.land("a")
	d, _ := prepare(f, taskrunner.ProviderClaudeNative)
	if _, err := os.Stat(d.AddDirs[0]); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Remove(f.ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.AddDirs[0]); !os.IsNotExist(err) {
		t.Fatalf("projection survived Remove: %v", err)
	}
}
