package runs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type fakeDelivery struct {
	mu      sync.Mutex
	calls   []string // "<ws>/<provider>/<purpose>"
	d       taskrunner.Delivery
	info    OfferedInfo
	panicky bool
}

func (f *fakeDelivery) Prepare(_ context.Context, ws int64, runID string, p taskrunner.Provider, purpose string) (taskrunner.Delivery, OfferedInfo) {
	f.mu.Lock()
	f.calls = append(f.calls, strings.Join([]string{strconv.FormatInt(ws, 10), string(p), purpose}, "/"))
	f.mu.Unlock()
	if f.panicky {
		panic("boom")
	}
	return f.d, f.info
}

func recordPrompts(t *testing.T, task store.Task) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "record-prompt-blocks"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func promptBlockLines(t *testing.T, worktree string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktree, "prompt-blocks"))
	if err != nil {
		t.Fatalf("read prompt-blocks: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestRegistry_OfferedInfoReachesObserver(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	fd := &fakeDelivery{d: taskrunner.Delivery{Preface: "SKILL-INDEX"}, info: OfferedInfo{SkillIDs: []int64{3, 4}, Measured: true}}
	reg.SetDeliveryProvider(fd)
	rec := newFinishRecorder()
	reg.AddObserver(rec)

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "the user text", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	f := rec.wait(t)
	if len(f.OfferedSkillIDs) != 2 || f.OfferedSkillIDs[0] != 3 || !f.OfferedMeasured {
		t.Fatalf("RunFinished offered = %v measured=%v", f.OfferedSkillIDs, f.OfferedMeasured)
	}
	if len(fd.calls) != 1 || fd.calls[0] != strconv.FormatInt(task.WorkspaceID, 10)+"/glm/" {
		t.Fatalf("Prepare calls = %v, want exactly one for the task's workspace", fd.calls)
	}
}

// TestACPRun_PersistedPromptUnchanged: the preface reaches the agent, but
// runs.prompt and the run's history show only the user's text.
func TestACPRun_PersistedPromptUnchanged(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	recordPrompts(t, task)
	reg := newTestRegistry(t, st)
	reg.SetDeliveryProvider(&fakeDelivery{d: taskrunner.Delivery{Preface: "SKILL-INDEX"}, info: OfferedInfo{SkillIDs: []int64{1}}})

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "the user text", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	lines := promptBlockLines(t, *task.WorktreePath)
	if len(lines) != 1 || !strings.Contains(lines[0], "SKILL-INDEX") || !strings.Contains(lines[0], "the user text") {
		t.Fatalf("agent saw %q, want preface + user text", lines)
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Prompt != "the user text" {
		t.Fatalf("runs.prompt = %q", row.Prompt)
	}
	hist, _, _ := reg.History(runID)
	for _, e := range hist {
		if strings.Contains(e.Text, "SKILL-INDEX") {
			t.Fatalf("history event leaks the preface: %+v", e)
		}
	}
	stored, _ := st.ListRunEvents(runID)
	for _, e := range stored {
		if strings.Contains(e.EventData, "SKILL-INDEX") {
			t.Fatalf("stored event leaks the preface: %+v", e)
		}
	}
}

func TestRegistry_DeliveryProviderErrorRunsWithoutDelivery(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	recordPrompts(t, task)
	reg := newTestRegistry(t, st)
	reg.SetDeliveryProvider(&fakeDelivery{panicky: true, d: taskrunner.Delivery{Preface: "X"}, info: OfferedInfo{SkillIDs: []int64{1}}})
	rec := newFinishRecorder()
	reg.AddObserver(rec)

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	if f := rec.wait(t); len(f.OfferedSkillIDs) != 0 || f.Status != "done" {
		t.Fatalf("finished = %+v, want done with nothing offered", f)
	}
	if lines := promptBlockLines(t, *task.WorktreePath); len(lines) != 1 || strings.Contains(lines[0], `"text":"X"`) {
		t.Fatalf("agent saw %q, want only the user's block", lines)
	}
}

func TestRegistry_NoDeliveryForSystemRuns(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	fd := &fakeDelivery{d: taskrunner.Delivery{Preface: "X"}, info: OfferedInfo{SkillIDs: []int64{1}}}
	reg.SetDeliveryProvider(fd)
	rec := newFinishRecorder()
	reg.AddObserver(rec)

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WithPurpose("reflect"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	if f := rec.wait(t); len(f.OfferedSkillIDs) != 0 {
		t.Fatalf("system run offered %v", f.OfferedSkillIDs)
	}
	if len(fd.calls) != 0 {
		t.Fatalf("Prepare called for a system run: %v", fd.calls)
	}
}
