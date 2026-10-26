package automation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

type fakeStarter struct {
	mu       sync.Mutex
	next     int64
	started  []StartRequest
	status   map[int64]RunStatus
	stopped  map[int64]string
	startErr error
	onStart  func()
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{status: map[int64]RunStatus{}, stopped: map[int64]string{}}
}

func (f *fakeStarter) Start(_ context.Context, req StartRequest) (StartResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onStart != nil {
		f.onStart()
	}
	if f.startErr != nil {
		return StartResult{}, f.startErr
	}
	f.next++
	f.started = append(f.started, req)
	f.status[f.next] = RunStatus{State: "running"}
	return StartResult{TaskID: f.next, RunID: fmt.Sprintf("run-%d", f.next)}, nil
}

func (f *fakeStarter) Status(id int64) (RunStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status[id], nil
}

func (f *fakeStarter) Stop(id int64, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped[id] = reason
	f.status[id] = RunStatus{State: "stopped"}
	return nil
}

func (f *fakeStarter) set(id int64, st RunStatus) {
	f.mu.Lock()
	f.status[id] = st
	f.mu.Unlock()
}

func (f *fakeStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

type fakeSink struct {
	mu     sync.Mutex
	fired  int
	paused []string
}

func (s *fakeSink) Fired(store.Automation, store.AutomationAudit) {
	s.mu.Lock()
	s.fired++
	s.mu.Unlock()
}
func (s *fakeSink) Paused(_ store.Automation, r string) {
	s.mu.Lock()
	s.paused = append(s.paused, r)
	s.mu.Unlock()
}
func (s *fakeSink) Updated(store.Automation) {}

// fakeSource is a polled source that replays queued signals.
type fakeSource struct {
	mu      sync.Mutex
	calls   int
	signals []Signal
	cursor  string
	err     error
}

func (f *fakeSource) Kind() string { return KindWorkflowRun }
func (f *fakeSource) Poll(_ context.Context, a store.Automation, _ Def, _ time.Time) (PollResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return PollResult{}, f.err
	}
	if a.PollCursor == "" {
		return PollResult{Cursor: f.cursor}, nil
	}
	out := f.signals
	f.signals = nil
	return PollResult{Signals: out, Cursor: f.cursor}, nil
}
func (f *fakeSource) Latest(context.Context, store.Automation, Def) (Signal, error) {
	return Signal{Kind: KindWorkflowRun, Payload: map[string]any{"runId": 99, "headSha": "abc1234"}}, nil
}

type env struct {
	t    *testing.T
	st   *store.Store
	home string
	now  time.Time
	cfg  Config
	fs   *fakeStarter
	sink *fakeSink
	src  *fakeSource
	eng  *Engine
	ws   store.Workspace
	prof store.AgentProfile
}

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/tmp/ws", Title: "ws"})
	if err != nil {
		t.Fatal(err)
	}
	prof, err := st.CreateAgentProfile(store.AgentProfile{Name: "lead", Provider: "claude-native", PermissionMode: "default"})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, home: home, now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
		cfg: Config{Enabled: true, Limits: Limits{MaxBudgetUSD: 10}},
		fs:  newFakeStarter(), sink: &fakeSink{}, src: &fakeSource{cursor: "100"}, ws: ws, prof: prof}
	e.eng = New(Deps{Store: st, Starter: e.fs, Memory: Memory{Home: home}, Sources: []Source{e.src},
		Config: func() Config { return e.cfg }, Sink: e.sink, Now: func() time.Time { return e.now }})
	return e
}

func (e *env) def(kind string) Def {
	d := Def{Name: "a-" + kind, WorkspaceID: e.ws.ID,
		Template: Template{ProfileID: e.prof.ID, Model: "claude-sonnet-5-5", Mode: "default", TitleTemplate: "t {{signal.headSha}}",
			PromptTemplate: "fix {{signal.runId}} on {{signal.branch}}"},
		Rails: Rails{MemoryEnabled: true}}
	if kind == KindCron {
		d.Signal = SignalSpec{Kind: KindCron, Cron: "0 * * * *"}
	} else {
		d.Signal = SignalSpec{Kind: KindWorkflowRun, Workflow: "Windows full suite"}
	}
	return d
}

func (e *env) create(d Def, enable bool) store.Automation {
	e.t.Helper()
	a, err := Create(e.st, d, e.cfg.Limits)
	if err != nil {
		e.t.Fatal(err)
	}
	if enable {
		if err := e.st.SetAutomationEnabled(a.ID, true); err != nil {
			e.t.Fatal(err)
		}
	}
	a, _ = e.st.GetAutomation(a.ID)
	return a
}

func (e *env) tick() { e.eng.Tick(context.Background()) }

func (e *env) advance(d time.Duration) { e.now = e.now.Add(d) }

func (e *env) audit(id int64) []store.AutomationAudit {
	rows, err := e.st.ListAutomationAudit(id, 500, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func decisions(rows []store.AutomationAudit) string {
	var out []string
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, rows[i].Decision)
	}
	return strings.Join(out, ",")
}

func ciSignal(sha string, run int) Signal {
	return Signal{Kind: KindWorkflowRun, Fingerprint: "gh:r/x:wf:develop:failure:" + sha,
		Payload: map[string]any{"repo": "r/x", "workflow": "wf", "branch": "develop", "headSha": sha, "runId": run, "conclusion": "failure"}}
}

// primed returns an enabled workflow automation whose cursor is primed.
func (e *env) primed(d Def) store.Automation {
	a := e.create(d, true)
	e.tick() // first poll primes the cursor, fires nothing
	if e.fs.count() != 0 {
		e.t.Fatal("priming fired")
	}
	a, _ = e.st.GetAutomation(a.ID)
	return a
}

func (e *env) pollSignal(s Signal) {
	e.src.mu.Lock()
	e.src.signals = append(e.src.signals, s)
	e.src.mu.Unlock()
	e.advance(6 * time.Minute)
	e.tick()
}

func TestKillSwitch_GlobalOffNoTickNoPoll(t *testing.T) {
	e := newEnv(t)
	e.create(e.def(KindWorkflowRun), true)
	e.cfg.Enabled = false
	for i := 0; i < 5; i++ {
		e.advance(10 * time.Minute)
		e.tick()
	}
	if e.src.calls != 0 || e.fs.count() != 0 {
		t.Fatalf("engine acted while disabled: polls=%d starts=%d", e.src.calls, e.fs.count())
	}
}

func TestPoll_ZeroEnabledZeroCalls(t *testing.T) {
	e := newEnv(t)
	e.create(e.def(KindWorkflowRun), false)
	e.tick()
	if e.src.calls != 0 {
		t.Fatalf("polled a disabled automation %d times", e.src.calls)
	}
}

func TestPoll_PrimesCursorFiresNothing(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	if a.PollCursor != "100" {
		t.Fatalf("cursor = %q", a.PollCursor)
	}
	if rows := e.audit(a.ID); len(rows) != 0 {
		t.Fatalf("priming wrote audit rows: %v", decisions(rows))
	}
}

func TestPausedGlobal_RecordsPaused(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.cfg.Paused = true
	e.pollSignal(ciSignal("abc1234", 1))
	if got := decisions(e.audit(a.ID)); got != "paused" || e.fs.count() != 0 {
		t.Fatalf("decisions = %q starts=%d", got, e.fs.count())
	}
}

func TestFire_ThenDedupe_SameShaDifferentRun(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.pollSignal(ciSignal("abc1234", 1))
	e.fs.set(1, RunStatus{State: "done"})
	e.pollSignal(ciSignal("abc1234", 2)) // re-run of the same commit
	if got := decisions(e.audit(a.ID)); got != "fire,deduped" || e.fs.count() != 1 {
		t.Fatalf("decisions = %q starts=%d", got, e.fs.count())
	}
}

func TestDedupe_DedupedRowDoesNotExtendWindow(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	w := 60
	d.Rails.DedupeWindowMinutes = &w
	d.Rails.MaxRunsPerHour = 12
	a := e.primed(d)
	e.pollSignal(ciSignal("abc1234", 1))
	e.fs.set(1, RunStatus{State: "done"})
	e.advance(40 * time.Minute)
	e.pollSignal(ciSignal("abc1234", 2)) // deduped at +46m
	e.advance(30 * time.Minute)
	e.pollSignal(ciSignal("abc1234", 3)) // +82m: outside the window of the FIRE row
	if got := decisions(e.audit(a.ID)); got != "fire,deduped,fire" {
		t.Fatalf("decisions = %q", got)
	}
}

func TestDedupe_NewShaIsNewSignal(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 12
	a := e.primed(d)
	e.pollSignal(ciSignal("abc1234", 1))
	e.fs.set(1, RunStatus{State: "done"})
	e.pollSignal(ciSignal("def5678", 2))
	if got := decisions(e.audit(a.ID)); got != "fire,fire" {
		t.Fatalf("decisions = %q", got)
	}
}

func TestDedupe_SurvivesRestart(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.pollSignal(ciSignal("abc1234", 1))
	e.fs.set(1, RunStatus{State: "done"})
	// A fresh engine over the same store: the window is in the audit table.
	e.eng = New(Deps{Store: e.st, Starter: e.fs, Memory: Memory{Home: e.home}, Sources: []Source{e.src},
		Config: func() Config { return e.cfg }, Now: func() time.Time { return e.now }})
	e.pollSignal(ciSignal("abc1234", 2))
	if got := decisions(e.audit(a.ID)); got != "fire,deduped" {
		t.Fatalf("decisions = %q", got)
	}
}

func TestCap_RunsPerHour_BurstOf50(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 2
	a := e.primed(d)
	for i := 0; i < 50; i++ {
		e.src.mu.Lock()
		e.src.signals = append(e.src.signals, ciSignal(fmt.Sprintf("%07d", i), i))
		e.src.mu.Unlock()
	}
	e.advance(6 * time.Minute)
	e.tick()
	fires := 0
	for _, r := range e.audit(a.ID) {
		if r.Decision == "fire" {
			fires++
		}
	}
	if fires != 1 || e.fs.count() != 1 { // single flight caps the rest as "running"
		t.Fatalf("fires = %d starts = %d, want 1 (single flight)", fires, e.fs.count())
	}
	// Finish the run each time: now only the hourly cap holds.
	for i := 0; i < 5; i++ {
		e.fs.set(int64(e.fs.count()), RunStatus{State: "done"})
		e.src.mu.Lock()
		e.src.signals = append(e.src.signals, ciSignal(fmt.Sprintf("a%06d", i), 100+i))
		e.src.mu.Unlock()
		e.advance(6 * time.Minute)
		e.tick()
	}
	if e.fs.count() != 2 {
		t.Fatalf("starts in an hour = %d, want 2 (maxRunsPerHour)", e.fs.count())
	}
	var capped *store.AutomationAudit
	rows := e.audit(a.ID)
	for i := range rows {
		if strings.HasPrefix(rows[i].Reason, "rate:") {
			capped = &rows[i]
		}
	}
	if capped == nil || capped.Decision != "capped" {
		t.Fatalf("no rate-capped row: %v", decisions(rows))
	}
}

func TestSingleFlight_RunningAndWaitingPermission(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 12
	a := e.primed(d)
	e.pollSignal(ciSignal("aaaaaaa", 1))
	e.fs.set(1, RunStatus{State: "waiting_permission"})
	e.pollSignal(ciSignal("bbbbbbb", 2))
	rows := e.audit(a.ID)
	if got := decisions(rows); got != "fire,capped" || !strings.HasPrefix(rows[0].Reason, "running") {
		t.Fatalf("decisions = %q reason=%q", got, rows[0].Reason)
	}
}

func TestAutoPause_TripsAtN_SuccessResets_ResumeClears(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 12
	d.Rails.PauseAfterFailures = 3
	a := e.primed(d)
	shas := []string{"1111111", "2222222", "3333333", "4444444", "5555555", "6666666", "7777777"}
	fail := func(i int, state string) {
		e.pollSignal(ciSignal(shas[i], i))
		e.fs.set(int64(e.fs.count()), RunStatus{State: state})
		e.advance(time.Minute)
		e.tick() // reconcile
	}
	fail(0, "error")
	fail(1, "error")
	fail(2, "done") // success resets the counter
	if cur, _ := e.st.GetAutomation(a.ID); cur.ConsecutiveFailures != 0 {
		t.Fatalf("counter not reset: %d", cur.ConsecutiveFailures)
	}
	fail(3, "error")
	fail(4, "error")
	fail(5, "error")
	cur, _ := e.st.GetAutomation(a.ID)
	if cur.Status != store.AutomationPaused || !strings.Contains(cur.PausedReason, "paused after 3 consecutive failed runs; last: task #") {
		t.Fatalf("status = %q reason = %q", cur.Status, cur.PausedReason)
	}
	if len(e.sink.paused) != 1 {
		t.Fatalf("paused events = %d", len(e.sink.paused))
	}
	n := e.fs.count()
	e.pollSignal(ciSignal(shas[6], 6))
	if e.fs.count() != n || decisions(e.audit(a.ID)[:1]) != "paused" {
		t.Fatalf("a paused automation fired or did not record paused")
	}
	if err := e.st.ResumeAutomation(a.ID, e.now); err != nil {
		t.Fatal(err)
	}
	e.pollSignal(ciSignal("8888888", 8))
	if e.fs.count() != n+1 {
		t.Fatalf("resume did not allow the next firing")
	}
}

func TestAutoPause_FireRowWithoutTaskCounts(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.PauseAfterFailures = 1
	a := e.primed(d)
	// A crash between the audit write and the launch: a fire row with no task.
	if _, err := e.st.InsertAutomationAudit(store.AutomationAudit{AutomationID: a.ID, SignalKind: KindWorkflowRun, Fingerprint: "x", Decision: store.AuditFire, CreatedAt: e.now}); err != nil {
		t.Fatal(err)
	}
	e.advance(5 * time.Minute)
	e.tick()
	cur, _ := e.st.GetAutomation(a.ID)
	if cur.Status != store.AutomationPaused {
		t.Fatalf("orphan fire row did not count as a failed run (status %q)", cur.Status)
	}
}

func TestStartFailed_CountsAsFailure(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.PauseAfterFailures = 1
	a := e.primed(d)
	e.fs.startErr = errors.New("boom")
	e.pollSignal(ciSignal("abc1234", 1))
	cur, _ := e.st.GetAutomation(a.ID)
	if cur.Status != store.AutomationPaused || !strings.Contains(cur.PausedReason, "boom") {
		t.Fatalf("status %q reason %q", cur.Status, cur.PausedReason)
	}
}

func TestWallClock_StopsRun(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Template.MaxRunMinutes = 30
	a := e.primed(d)
	e.pollSignal(ciSignal("abc1234", 1))
	e.advance(31 * time.Minute)
	e.tick()
	if e.fs.stopped[1] != "automation_timeout" {
		t.Fatalf("run not stopped: %v", e.fs.stopped)
	}
	rows := e.audit(a.ID)
	if rows[0].Outcome != "timeout" {
		t.Fatalf("outcome = %q", rows[0].Outcome)
	}
	if cur, _ := e.st.GetAutomation(a.ID); cur.ConsecutiveFailures != 1 {
		t.Fatalf("timeout did not count as a failed run")
	}
}

func TestBudget_NextFireCappedAfterOverspend(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Template.BudgetUSD = 3
	d.Rails.MaxRunsPerHour = 12
	a := e.primed(d)
	e.pollSignal(ciSignal("1111111", 1))
	cost := 3.5
	e.fs.set(1, RunStatus{State: "done", CostUSD: &cost})
	e.pollSignal(ciSignal("2222222", 2))
	rows := e.audit(a.ID)
	if got := decisions(rows); got != "fire,capped" || !strings.HasPrefix(rows[0].Reason, "budget") {
		t.Fatalf("decisions = %q reason %q", got, rows[0].Reason)
	}
	if err := e.st.ResumeAutomation(a.ID, e.now); err != nil {
		t.Fatal(err)
	}
	e.pollSignal(ciSignal("3333333", 3))
	if e.fs.count() != 2 {
		t.Fatalf("resume did not clear the budget block")
	}
}

func TestStarter_ReceivesTemplateModeAndOrigin(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Template.Mode = "acceptEdits"
	e.primed(d)
	e.pollSignal(ciSignal("abc1234", 7))
	req := e.fs.started[0]
	if req.Def.Template.Mode != "acceptEdits" || req.Def.Template.AutoAccept {
		t.Fatalf("template mode not carried: %+v", req.Def.Template)
	}
	if OriginAutomation != "automation" {
		t.Fatal("origin constant changed")
	}
	if req.Title != "t abc1234" || !strings.Contains(req.Prompt, "fix 7 on develop") {
		t.Fatalf("title %q prompt %q", req.Title, req.Prompt)
	}
}

func TestAudit_FireRowLinkedToTask_AndOutcomeBackfilled(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.pollSignal(ciSignal("abc1234", 1))
	rows := e.audit(a.ID)
	if rows[0].TaskID == nil || *rows[0].TaskID != 1 || rows[0].RunID != "run-1" {
		t.Fatalf("not linked: %+v", rows[0])
	}
	e.fs.set(1, RunStatus{State: "done"})
	e.advance(time.Minute)
	e.tick()
	rows = e.audit(a.ID)
	if rows[0].Outcome != "done" || rows[0].FinishedAt == nil {
		t.Fatalf("outcome not backfilled: %+v", rows[0])
	}
	if r, ok, _ := e.st.AutomationAuditForTask(1); !ok || r.AutomationID != a.ID {
		t.Fatal("task does not link back to its automation")
	}
}

func TestAudit_PayloadCappedAt4KiB(t *testing.T) {
	e := newEnv(t)
	a := e.create(e.def(KindWorkflowRun), false)
	r, err := e.st.InsertAutomationAudit(store.AutomationAudit{AutomationID: a.ID, SignalKind: "x", Fingerprint: "f", Decision: "paused", Payload: strings.Repeat("x", 10000)})
	if err != nil || len(r.Payload) > 4096 {
		t.Fatalf("payload len %d err %v", len(r.Payload), err)
	}
}

func TestAudit_RetentionKeepsLast100(t *testing.T) {
	e := newEnv(t)
	a := e.create(e.def(KindWorkflowRun), false)
	old := e.now.Add(-200 * 24 * time.Hour)
	for i := 0; i < 130; i++ {
		if _, err := e.st.InsertAutomationAudit(store.AutomationAudit{AutomationID: a.ID, SignalKind: "x", Fingerprint: "f", Decision: "paused", CreatedAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.st.PruneAutomationAudit(e.now.Add(-90 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := len(e.audit(a.ID)); n != 100 {
		t.Fatalf("rows after prune = %d, want the last 100", n)
	}
}

func TestMemory_StoredFromBlock_AndOversizeKeepsOld(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.pollSignal(ciSignal("1111111", 1))
	e.fs.set(1, RunStatus{State: "done", FinalText: "done.\n```automation-memory\n- flaky on windows\n```\n"})
	e.advance(time.Minute)
	e.tick()
	got, _ := Memory{Home: e.home}.Read(a.ID, 1<<20)
	if !strings.Contains(got, "flaky on windows") {
		t.Fatalf("memory = %q", got)
	}
	d, _ := DefOf(a)
	big := strings.Repeat("y", d.Rails.MemoryMaxBytes+10)
	e.pollSignal(ciSignal("2222222", 2))
	e.fs.set(2, RunStatus{State: "done", FinalText: "```automation-memory\n" + big + "\n```"})
	e.advance(time.Minute)
	e.tick()
	got2, _ := Memory{Home: e.home}.Read(a.ID, 1<<20)
	if got2 != got {
		t.Fatalf("oversize block replaced the memory")
	}
	if rows := e.audit(a.ID); !strings.Contains(rows[0].Note, "rejected, too_large") {
		t.Fatalf("note = %q", rows[0].Note)
	}
}

func TestFireNow_BypassesDedupeOnly(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 12
	a := e.primed(d)
	e.pollSignal(ciSignal("abc1234", 99))
	e.fs.set(1, RunStatus{State: "done"})
	row, err := e.eng.Fire(context.Background(), a.ID)
	if err != nil || row.Decision != "fire" || !row.Manual {
		t.Fatalf("manual fire = %+v err %v", row, err)
	}
	// Single flight still holds for a human.
	row, _ = e.eng.Fire(context.Background(), a.ID)
	if row.Decision != "capped" {
		t.Fatalf("second manual fire while running = %q", row.Decision)
	}
}

func TestFireNow_CountsTowardRunsPerHour(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 1
	a := e.primed(d)
	if row, _ := e.eng.Fire(context.Background(), a.ID); row.Decision != "fire" {
		t.Fatalf("first = %q", row.Decision)
	}
	e.fs.set(1, RunStatus{State: "done"})
	e.advance(time.Minute)
	e.tick()
	if row, _ := e.eng.Fire(context.Background(), a.ID); row.Decision != "capped" || !strings.HasPrefix(row.Reason, "rate") {
		t.Fatalf("second = %q %q", row.Decision, row.Reason)
	}
}

func TestPoll_BackoffOnError_AndDeferredNoFailure(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.src.err = errors.New("forge_unauthenticated")
	e.advance(6 * time.Minute)
	e.tick()
	calls := e.src.calls
	e.advance(time.Minute) // inside the backoff
	e.tick()
	if e.src.calls != calls {
		t.Fatalf("polled again inside the backoff window")
	}
	cur, _ := e.st.GetAutomation(a.ID)
	if cur.PollFailures != 1 {
		t.Fatalf("failures = %d", cur.PollFailures)
	}
	until := e.now.Add(50 * time.Minute)
	e.src.err = &ErrDeferred{Until: until}
	e.advance(20 * time.Minute)
	e.tick()
	cur, _ = e.st.GetAutomation(a.ID)
	if cur.PollFailures != 1 || cur.PollNextAllowedAt == nil || !cur.PollNextAllowedAt.Equal(until) {
		t.Fatalf("deferred state = %+v", cur)
	}
}

func TestPoll_CursorSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.src.cursor = "105"
	e.pollSignal(ciSignal("abc1234", 105))
	cur, _ := e.st.GetAutomation(a.ID)
	if cur.PollCursor != "105" {
		t.Fatalf("cursor = %q", cur.PollCursor)
	}
}

func TestCron_CatchupOnce_BacklogCollapses_DoubleTickOneFire(t *testing.T) {
	e := newEnv(t)
	d := e.def(KindCron)
	a := e.create(d, true)
	e.tick() // schedules
	next, _ := e.st.GetAutomation(a.ID)
	if next.NextRunAt == nil || !next.NextRunAt.Equal(time.Date(2026, 10, 27, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("next run = %v", next.NextRunAt)
	}
	// 10 minutes late: one catch-up fire.
	e.now = time.Date(2026, 10, 27, 13, 10, 0, 0, time.UTC)
	e.tick()
	e.tick() // double tick of the same instant
	if got := decisions(e.audit(a.ID)); got != "fire" || e.fs.count() != 1 {
		t.Fatalf("decisions = %q starts %d", got, e.fs.count())
	}
	if !e.audit(a.ID)[0].Catchup {
		t.Fatal("catchup flag not set")
	}
	e.fs.set(1, RunStatus{State: "done"})
	// The daemon was off for 5 hours: one missed row, no backlog of tasks.
	e.now = time.Date(2026, 10, 27, 18, 30, 0, 0, time.UTC)
	e.tick()
	if got := decisions(e.audit(a.ID)); got != "fire,missed" || e.fs.count() != 1 {
		t.Fatalf("decisions = %q starts %d", got, e.fs.count())
	}
}

func TestCron_FiresOnTime_NeverFiresThePastOnEnable(t *testing.T) {
	e := newEnv(t)
	e.now = time.Date(2026, 10, 27, 12, 59, 0, 0, time.UTC)
	a := e.create(e.def(KindCron), true)
	e.tick()
	if e.fs.count() != 0 {
		t.Fatal("fired on enable")
	}
	e.now = time.Date(2026, 10, 27, 13, 0, 30, 0, time.UTC)
	e.tick()
	rows := e.audit(a.ID)
	if got := decisions(rows); got != "fire" || rows[0].Catchup || rows[0].Fingerprint != fmt.Sprintf("cron:%d:2026-10-27T13:00:00Z", a.ID) {
		t.Fatalf("decisions %q row %+v", got, rows[0])
	}
}

func TestEvaluate_KindSwitchRowForManualFireWhenDisabled(t *testing.T) {
	e := newEnv(t)
	a := e.primed(e.def(KindWorkflowRun))
	e.cfg.Enabled = false
	row, err := e.eng.Fire(context.Background(), a.ID)
	if err != nil || row.Decision != "disabled_kill_switch" || e.fs.count() != 0 {
		t.Fatalf("row %+v err %v", row, err)
	}
}
