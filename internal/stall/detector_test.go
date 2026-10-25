package stall

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

const testMinutes = 15

type env struct {
	t      *testing.T
	s      *store.Store
	ws     int64
	base   time.Time // real wall time at setup; the fake clock is base + offset
	offset time.Duration
	alerts []Episode
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ws, err := s.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return &env{t: t, s: s, ws: ws.ID, base: time.Now().UTC().Truncate(time.Second)}
}

func (e *env) now() time.Time { return e.base.Add(e.offset) }

func (e *env) at(d time.Duration) { e.offset = d }

func (e *env) detector(minutes int) *Detector {
	return New(Deps{
		Snapshot: e.s.StallSnapshot,
		Mark:     e.s.MarkStallAlerted,
		Alert:    func(ep Episode, _ int) { e.alerts = append(e.alerts, ep) },
		Now:      e.now,
		Minutes:  func() int { return minutes },
	})
}

func (e *env) task(title string, parent *int64) store.Task {
	e.t.Helper()
	tk, err := e.s.CreateTask(store.Task{WorkspaceID: e.ws, Title: title, Status: "created", ParentTaskID: parent})
	if err != nil {
		e.t.Fatalf("CreateTask() error = %v", err)
	}
	return tk
}

// run creates a run for the task started at start (relative to base) and, for
// a terminal status, finished a second later.
func (e *env) run(id string, taskID int64, status string, start time.Duration, profile int64) {
	e.t.Helper()
	started := e.base.Add(start)
	if _, err := e.s.CreateRun(store.Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: started, ProfileID: profile}); err != nil {
		e.t.Fatalf("CreateRun() error = %v", err)
	}
	if status != "running" {
		fin := started.Add(time.Second)
		if _, err := e.s.UpdateRunStatus(id, status, &fin, "", ""); err != nil {
			e.t.Fatalf("UpdateRunStatus() error = %v", err)
		}
	}
}

// pair returns a root seat and one child.
func (e *env) pair() (root, child store.Task) {
	root = e.task("lead", nil)
	child = e.task("worker", &root.ID)
	return root, child
}

func (e *env) wantAlerts(n int) {
	e.t.Helper()
	if len(e.alerts) != n {
		e.t.Fatalf("alerts = %d (%+v), want %d", len(e.alerts), e.alerts, n)
	}
}

func TestIdleSeatPastThresholdAlertsOnce(t *testing.T) {
	e := newEnv(t)
	root, _ := e.pair()
	d := e.detector(testMinutes)
	e.at(10 * time.Minute)
	d.Tick()
	e.wantAlerts(0)
	e.at(20 * time.Minute)
	d.Tick()
	e.wantAlerts(1)
	if e.alerts[0].TaskID != root.ID || e.alerts[0].Title != "lead" {
		t.Fatalf("alert = %+v, want seat %d", e.alerts[0], root.ID)
	}
}

func TestSecondTickSameEpisodeDoesNotReAlert(t *testing.T) {
	e := newEnv(t)
	e.pair()
	d := e.detector(testMinutes)
	e.at(20 * time.Minute)
	d.Tick()
	e.at(40 * time.Minute)
	d.Tick()
	d.Tick()
	e.wantAlerts(1)
}

func TestRunStartingInSubtreeClearsEpisodeAndLaterStallAlertsAgain(t *testing.T) {
	e := newEnv(t)
	root, child := e.pair()
	d := e.detector(testMinutes)
	e.at(20 * time.Minute)
	d.Tick()
	e.wantAlerts(1)
	first := e.alerts[0].Since

	e.run("c1", child.ID, "running", 25*time.Minute, 0)
	e.at(30 * time.Minute)
	d.Tick()
	e.wantAlerts(1) // running child suppresses
	if got := d.Current(); len(got) != 0 {
		t.Fatalf("Current() = %v while a child runs", got)
	}

	fin := e.base.Add(26 * time.Minute)
	if _, err := e.s.UpdateRunStatus("c1", "stopped", &fin, "", ""); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	// Stopped counts as finished, so give the tree unfinished work again.
	e.task("another", &root.ID)
	e.at(60 * time.Minute)
	d.Tick()
	e.wantAlerts(2)
	if !e.alerts[1].Since.After(first) {
		t.Fatalf("second episode since %v not after first %v", e.alerts[1].Since, first)
	}
}

func TestRunningChildRunSuppresses(t *testing.T) {
	e := newEnv(t)
	_, child := e.pair()
	e.run("c1", child.ID, "running", 0, 0)
	d := e.detector(testMinutes)
	e.at(5 * time.Hour)
	d.Tick()
	e.wantAlerts(0)
}

func TestQueuedItemSuppresses(t *testing.T) {
	e := newEnv(t)
	root, _ := e.pair()
	chat, err := e.s.CreateChat(root.ID, "q")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := e.s.EnqueueChatPromptAt(chat.ID, "go", "{}", store.ChatQueueSourceHuman, 0, 0, 0, "", nil); err != nil {
		t.Fatalf("EnqueueChatPromptAt() error = %v", err)
	}
	d := e.detector(testMinutes)
	// The item (created at ~base) is younger than N: the tree is not stalled.
	e.at(10 * time.Minute)
	d.Tick()
	e.wantAlerts(0)
	// Older than N, it no longer suppresses.
	e.at(20 * time.Minute)
	d.Tick()
	e.wantAlerts(1)
}

func TestRateLimitedWaitSeatFutureResetDoesNotAlert(t *testing.T) {
	e := newEnv(t)
	root, prof := e.waitSeat(30 * time.Minute)
	_ = root
	_ = prof
	d := e.detector(testMinutes)
	e.at(20 * time.Minute) // reset is still 10 min away
	d.Tick()
	e.wantAlerts(0)
}

func TestRateLimitedWaitSeatAlertsNMinutesAfterReset(t *testing.T) {
	e := newEnv(t)
	root, _ := e.waitSeat(30 * time.Minute)
	d := e.detector(testMinutes)
	e.at(44 * time.Minute) // 14 min after the reset
	d.Tick()
	e.wantAlerts(0)
	e.at(46 * time.Minute)
	d.Tick()
	e.wantAlerts(1)
	if e.alerts[0].TaskID != root.ID || !e.alerts[0].Since.Equal(e.base.Add(30*time.Minute)) {
		t.Fatalf("alert = %+v, want since = reset", e.alerts[0])
	}
}

// waitSeat builds a seat whose latest run was rate limited under an
// on_rate_limit=wait profile with the reset resetIn after base, plus an idle
// child so the tree is not done.
func (e *env) waitSeat(resetIn time.Duration) (store.Task, store.AgentProfile) {
	e.t.Helper()
	prof, err := e.s.CreateAgentProfile(store.AgentProfile{Name: "lead", Provider: "glm", OnRateLimit: "wait"})
	if err != nil {
		e.t.Fatalf("CreateAgentProfile() error = %v", err)
	}
	root, _ := e.pair()
	e.run("r1", root.ID, "error", 0, prof.ID)
	reset := e.base.Add(resetIn)
	if err := e.s.UpdateRunFailure("r1", "rate_limited", "5h", &reset, false); err != nil {
		e.t.Fatalf("UpdateRunFailure() error = %v", err)
	}
	return root, prof
}

func TestStallAlertMinutesZeroNeverAlerts(t *testing.T) {
	e := newEnv(t)
	e.pair()
	d := e.detector(0)
	e.at(10 * time.Hour)
	d.Tick()
	e.wantAlerts(0)
	if got := d.Current(); len(got) != 0 {
		t.Fatalf("Current() = %v with minutes=0", got)
	}
}

func TestRestartDoesNotReAlertAlertedEpisode(t *testing.T) {
	e := newEnv(t)
	e.pair()
	e.at(20 * time.Minute)
	e.detector(testMinutes).Tick()
	e.wantAlerts(1)
	// A "restarted daemon": a fresh detector over the same database.
	e.detector(testMinutes).Tick()
	e.wantAlerts(1)
}

func TestDoneTreeNeverAlerts(t *testing.T) {
	e := newEnv(t)
	root, child := e.pair()
	e.run("r", root.ID, "done", 0, 0)
	e.run("c", child.ID, "done", 0, 0)
	d := e.detector(testMinutes)
	e.at(5 * time.Hour)
	d.Tick()
	e.wantAlerts(0)
	if got := d.Current(); len(got) != 0 {
		t.Fatalf("Current() = %v for a done tree", got)
	}
}

func TestArchivedSeatNeverAlerts(t *testing.T) {
	e := newEnv(t)
	root, child := e.pair()
	e.run("c", child.ID, "done", 0, 0)
	if _, err := e.s.ArchiveTask(root.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	d := e.detector(testMinutes)
	e.at(5 * time.Hour)
	d.Tick()
	e.wantAlerts(0)
}

func TestSystemTaskNeverAlerts(t *testing.T) {
	e := newEnv(t)
	yes := true
	prof, err := e.s.CreateAgentProfile(store.AgentProfile{Name: "lead", Provider: "glm", OrchestrationTools: &yes})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}
	sys, err := e.s.CreateTask(store.Task{WorkspaceID: e.ws, Title: "sys", Status: "created", Kind: "system"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	e.run("r", sys.ID, "error", 0, prof.ID)
	e.task("child of system", &sys.ID)
	d := e.detector(testMinutes)
	e.at(5 * time.Hour)
	d.Tick()
	e.wantAlerts(0)
}

func TestBudgetExceededRefusalAloneIsNotBusy(t *testing.T) {
	e := newEnv(t)
	// A tree over its budget refuses new runs without creating one: the tree
	// is just an unfinished tree with no activity, and it still stalls.
	budget := 0.01
	root, err := e.s.CreateTask(store.Task{WorkspaceID: e.ws, Title: "capped", Status: "created", BudgetUSD: &budget})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	e.task("worker", &root.ID)
	d := e.detector(testMinutes)
	e.at(20 * time.Minute)
	d.Tick()
	e.wantAlerts(1)
}

func TestTickIsOSeatsNoExternalCalls(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 50; i++ {
		e.pair()
	}
	var snapshots, marks, alerts int
	d := New(Deps{
		Snapshot: func() ([]store.StallRow, error) { snapshots++; return e.s.StallSnapshot() },
		Mark:     func(id int64, since time.Time) error { marks++; return e.s.MarkStallAlerted(id, since) },
		Alert:    func(Episode, int) { alerts++ },
		Now:      e.now,
		Minutes:  func() int { return testMinutes },
	})
	e.at(20 * time.Minute)
	d.Tick()
	if snapshots != 1 {
		t.Fatalf("snapshot calls = %d for 50 seats, want 1", snapshots)
	}
	if marks != 50 || alerts != 50 {
		t.Fatalf("marks=%d alerts=%d, want 50 each", marks, alerts)
	}
	d.Tick()
	if snapshots != 2 || marks != 50 || alerts != 50 {
		t.Fatalf("second tick: snapshots=%d marks=%d alerts=%d", snapshots, marks, alerts)
	}
}
