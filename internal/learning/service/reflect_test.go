package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/learning/reflector"
	"github.com/spacingmind/smind/internal/store"
)

const (
	userPrompt  = "why does this test keep failing on CI only, the shared `t.TempDir` is reused across parallel tests"
	userQuote   = "why does this test keep failing on CI only"
	tokenSecret = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
)

// grounded returns the reflector's reply with one create intent grounded in
// the human prompt of runID (cited at seq -1).
func grounded(runID string, mut func(map[string]any)) string {
	in := map[string]any{
		"action":      "create",
		"name":        "learned-flaky-tempdir",
		"category":    "go",
		"description": "Use when a Go test fails on a shared TempDir.",
		"body":        "Run the failing test alone with -race.\nCheck for a shared `t.TempDir` across parallel tests.\nAdd t.Parallel guards or separate dirs.",
		"reason":      "the user hit this on CI",
		"evidence":    []map[string]any{{"kind": "user_message", "run_id": runID, "seq": -1, "quote": userQuote}},
	}
	if mut != nil {
		mut(in)
	}
	return reply(in)
}

func reply(intents ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"intents": intents})
	return string(b)
}

// reflectFx is a propose-mode fixture with one finished run on the chat that
// has a human prompt, a tool call and an assistant text carrying a secret.
type reflectFx struct {
	*fx
	runID string
	ids   []int64
}

func newReflectFx(t *testing.T) *reflectFx { return newReflectFxFor(t, "claude-native") }

func newReflectFxFor(t *testing.T, provider string) *reflectFx {
	t.Helper()
	f := newFx(t, "propose")
	f.chat = f.newChat(f.task, provider)
	runID, ids := f.addRun(f.chat, userPrompt,
		toolEv("c1", "Bash", "failed", map[string]any{"command": "go test ./..."}),
		textEv("the token is "+tokenSecret+" and the fix is separate dirs"),
		toolEv("c2", "Bash", "completed", map[string]any{"command": "go test -race ./..."}),
	)
	return &reflectFx{fx: f, runID: runID, ids: ids}
}

func (f *reflectFx) say(text string) {
	f.exec.mu.Lock()
	f.exec.fn = func(executor.Request) (executor.Result, error) {
		return executor.Result{RunID: "refl-1", FinalText: text}, nil
	}
	f.exec.mu.Unlock()
}

// reflect runs one explicit job to completion and returns it.
func (f *reflectFx) reflect() store.LearningJob {
	f.t.Helper()
	if err := f.svc.Reflect(context.Background(), f.chat.ID); err != nil {
		f.t.Fatalf("Reflect: %v", err)
	}
	f.svc.wg.Wait()
	return f.jobs()[0]
}

func (f *reflectFx) state() (watermark, pending int64) {
	c, err := f.st.GetChat(f.chat.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return c.LearnWatermark, c.LearnPending
}

func (f *reflectFx) proposals() []store.LearnedProposal {
	ps, err := f.st.ListProposals(f.ws.ID, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return ps
}

// learningRun records a finished reflect-purpose run on the workspace's
// system task, optionally with a usage row.
func (f *reflectFx) learningRun(id string, usage *store.RunUsage) {
	f.t.Helper()
	sys, ok, err := f.st.GetSystemTask(f.ws.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	var chatID int64
	if !ok {
		var c store.Chat
		sys, c, err = f.st.CreateSystemTask(f.ws.ID, "learning", "/cwd")
		if err != nil {
			f.t.Fatal(err)
		}
		chatID = c.ID
	}
	fin := f.clk.Now()
	if _, err := f.st.CreateRun(store.Run{ID: id, TaskID: sys.ID, ChatID: chatID, Provider: "claude-native", Status: "done",
		StartedAt: f.clk.Now(), FinishedAt: &fin, Purpose: "reflect"}); err != nil {
		f.t.Fatal(err)
	}
	if usage != nil {
		usage.RunID, usage.TaskID, usage.Provider, usage.Source = id, sys.ID, "claude-native", "test"
		if err := f.st.UpsertRunUsage(*usage); err != nil {
			f.t.Fatal(err)
		}
	}
}

func i64(v int64) *int64 { return &v }

func TestReflect_HappyPathCreatesProposals(t *testing.T) {
	f := newReflectFx(t)
	f.say(grounded(f.runID, nil))
	job := f.reflect()

	if job.Status != "landed" || job.Error != "" || job.RunID != "refl-1" {
		t.Fatalf("job = %+v", job)
	}
	ps := f.proposals()
	if len(ps) != 1 || ps[0].Status != "pending" || ps[0].Kind != "create" || ps[0].Name != "learned-flaky-tempdir" || ps[0].Source != "reflector" ||
		ps[0].JobID == nil || *ps[0].JobID != job.ID {
		t.Fatalf("proposals = %+v", ps)
	}
	if !strings.Contains(ps[0].Evidence, userQuote) {
		t.Fatalf("evidence not stored: %s", ps[0].Evidence)
	}
	items, err := approval.DecodeItems(ps[0].Intents)
	if err != nil || len(items) != 1 || items[0].Result.Name != "learned-flaky-tempdir" {
		t.Fatalf("items = %+v, %v", items, err)
	}
	led, _ := f.st.ListLedger(f.ws.ID, 0, 10)
	if len(led) != 1 || led[0].Action != "propose" {
		t.Fatalf("ledger = %+v", led)
	}
	wm, pending := f.state()
	if wm != f.ids[len(f.ids)-1] || pending != 0 {
		t.Fatalf("state = %d/%d, want %d/0", wm, pending, f.ids[len(f.ids)-1])
	}
	if f.notify.proposedCount() != 1 {
		t.Fatalf("NotifyProposed called %d times", f.notify.proposedCount())
	}
	if f.exec.count() != 1 {
		t.Fatalf("executor called %d times, want 1", f.exec.count())
	}
	req := f.exec.last()
	if req.SystemPrompt != reflector.ReflectorPrompt() || req.Purpose != "reflect" || req.MaxTurns != 1 || req.Timeout != 5*time.Minute ||
		req.Model != "haiku" || req.WorkspaceID != f.ws.ID {
		t.Fatalf("request = %+v", req)
	}
	if !strings.Contains(req.UserPrompt, userQuote) {
		t.Fatal("window does not contain the human prompt")
	}
	if strings.Contains(req.UserPrompt, tokenSecret) {
		t.Fatal("window leaked the secret")
	}
	f.notify.mu.Lock()
	defer f.notify.mu.Unlock()
	if got := strings.Join(f.notify.jobs, ","); got != "queued,running,landed" {
		t.Fatalf("job notifications = %s", got)
	}
	if len(f.notify.changed) == 0 {
		t.Fatal("NotifyChanged not called")
	}
}

func TestCollector_WatermarkAdvancesOnlyOnTerminalJob(t *testing.T) {
	type tc struct {
		name    string
		setup   func(f *reflectFx)
		status  string
		advance bool
	}
	cases := []tc{
		{"landed", func(f *reflectFx) { f.say(grounded(f.runID, nil)) }, "landed", true},
		{"empty", func(f *reflectFx) { f.say(`{"intents":[]}`) }, "empty", true},
		{"parse failure", func(f *reflectFx) { f.say("not json") }, "failed", true},
		{"executor error", func(f *reflectFx) {
			f.exec.fn = func(executor.Request) (executor.Result, error) {
				return executor.Result{}, errors.New("isolation violated")
			}
		}, "failed", true},
		{"provider skip", nil, "skipped", true},
		{"budget skip", func(f *reflectFx) {
			f.learningRun("spent", &store.RunUsage{CostUSD: ptrF(f.config().DailyBudgetUSD), InputTokens: i64(1), OutputTokens: i64(1)})
		}, "skipped", false},
		{"executor busy", func(f *reflectFx) {
			f.exec.fn = func(executor.Request) (executor.Result, error) { return executor.Result{}, executor.ErrBusy }
		}, "failed", false},
		{"cancelled", func(f *reflectFx) {
			f.exec.fn = func(executor.Request) (executor.Result, error) { return executor.Result{}, context.Canceled }
		}, "failed", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newReflectFx(t)
			if c.name == "provider skip" {
				f = newReflectFxFor(t, "glm")
			} else {
				c.setup(f)
			}
			if _, err := f.st.AddChatLearnPending(f.chat.ID, 33); err != nil {
				t.Fatal(err)
			}
			job := f.reflect()
			if job.Status != c.status {
				t.Fatalf("status = %s (%s %s), want %s", job.Status, job.SkipReason, job.Error, c.status)
			}
			wm, pending := f.state()
			last := f.ids[len(f.ids)-1]
			if c.advance && (wm != last || pending != 0) {
				t.Fatalf("state = %d/%d, want watermark %d and pending 0", wm, pending, last)
			}
			if !c.advance && (wm != 0 || pending != 33) {
				t.Fatalf("state = %d/%d, want untouched 0/33", wm, pending)
			}
		})
	}
}

func ptrF(v float64) *float64 { return &v }

func TestCollector_CrashRetriesSameWindow(t *testing.T) {
	// Crash scenario: a job row was left running by the old process.
	a := newReflectFx(t)
	a.say(`{"intents":[]}`)
	if _, err := a.st.AddChatLearnPending(a.chat.ID, 20); err != nil {
		t.Fatal(err)
	}
	stale, err := a.st.CreateLearningJob(store.LearningJob{WorkspaceID: a.ws.ID, ChatID: &a.chat.ID, Kind: "reflect", TriggerKind: "cadence", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.svc.Start(ctx) // the "new process"
	got, _ := a.st.GetLearningJob(stale.ID)
	if got.Status != "failed" || got.Error != "interrupted by restart" {
		t.Fatalf("stale job = %+v", got)
	}
	if wm, pending := a.state(); wm != 0 || pending != 20 {
		t.Fatalf("watermark moved by the restart: %d/%d", wm, pending)
	}
	a.reflect()
	a.svc.Close()

	// Clean scenario with identical data: the window the retry saw is the same.
	b := newReflectFx(t)
	b.say(`{"intents":[]}`)
	b.reflect()
	if a.exec.count() != 1 || b.exec.count() != 1 {
		t.Fatalf("calls = %d/%d", a.exec.count(), b.exec.count())
	}
	if a.exec.last().UserPrompt != b.exec.last().UserPrompt {
		t.Fatal("retry after a crash rebuilt a different window")
	}
}

func TestReflect_ParseFailureMarksJobFailed(t *testing.T) {
	for name, text := range map[string]string{
		"prose":          "Here are my thoughts: you should learn to use TempDir carefully.",
		"fenced json":    "```json\n" + `{"intents":[]}` + "\n```",
		"unknown action": reply(map[string]any{"action": "explode", "name": "learned-x"}),
	} {
		t.Run(name, func(t *testing.T) {
			f := newReflectFx(t)
			f.say(text)
			job := f.reflect()
			if job.Status != "failed" || job.Error == "" || len(f.proposals()) != 0 {
				t.Fatalf("job = %+v, proposals %d", job, len(f.proposals()))
			}
			if wm, _ := f.state(); wm == 0 {
				t.Fatal("a poisoned window must advance the watermark")
			}
		})
	}
}

func TestReflect_GateRejectsUngroundedIntentRecordedInVerdict(t *testing.T) {
	f := newReflectFx(t)
	f.say(grounded(f.runID, func(in map[string]any) {
		in["body"] = "Run the failing test alone with -race.\nThen run `rm -rf build` before retrying.\nAdd separate dirs."
	}))
	job := f.reflect()
	if job.Status != "empty" || len(f.proposals()) != 0 {
		t.Fatalf("job = %+v, proposals %d", job, len(f.proposals()))
	}
	var vs []verdict
	if err := json.Unmarshal([]byte(job.Verdicts), &vs); err != nil || len(vs) != 1 {
		t.Fatalf("verdicts = %s, %v", job.Verdicts, err)
	}
	v := vs[0]
	if v.Accepted || v.Name != "learned-flaky-tempdir" || v.Action != "create" || len(v.Findings) == 0 || v.Findings[0].Reason != "ungrounded_content" {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestReflect_ScopeCheckRejectsUnknownPatchTarget(t *testing.T) {
	f := newReflectFx(t)
	f.say(reply(map[string]any{
		"action": "patch", "name": "learned-nope", "old_string": "a", "new_string": "b", "reason": "r",
		"evidence": []map[string]any{{"kind": "user_message", "run_id": f.runID, "seq": -1, "quote": userQuote}},
	}))
	job := f.reflect()
	if job.Status != "failed" || !strings.Contains(job.Error, "not shown") || len(f.proposals()) != 0 {
		t.Fatalf("job = %+v", job)
	}
}

func TestReflect_EvidenceRedactedBeforeStore(t *testing.T) {
	f := newReflectFx(t)
	f.say(grounded(f.runID, func(in map[string]any) {
		in["evidence"] = []map[string]any{{"kind": "user_message", "run_id": f.runID, "seq": -1, "quote": userQuote + " " + tokenSecret}}
	}))
	job := f.reflect()
	if job.Status != "empty" || len(f.proposals()) != 0 {
		t.Fatalf("job = %+v, proposals %d", job, len(f.proposals()))
	}
	// The gate's verbatim check refuses the raw secret first (the window holds
	// the redacted text); the final re-redaction below is the second net.
	if !strings.Contains(job.Verdicts, `"accepted":false`) {
		t.Fatalf("verdicts = %s", job.Verdicts)
	}
	if strings.Contains(job.Verdicts, tokenSecret) || strings.Contains(job.Error, tokenSecret) {
		t.Fatal("secret persisted on the job row")
	}
	led, _ := f.st.ListLedger(f.ws.ID, 0, 10)
	if len(led) != 0 {
		t.Fatalf("ledger = %+v", led)
	}
	// The final re-redaction check itself, independent of what the gate caught.
	rd := redact.New(nil, nil)
	if !secretIn(rd, reflectorIntent(tokenSecret)) {
		t.Fatal("secretIn missed a token-shaped secret in an evidence quote")
	}
	if secretIn(rd, reflectorIntent("a perfectly normal quote")) {
		t.Fatal("secretIn flagged a clean quote")
	}
}

func TestReflect_TwoIntentsCannotDuplicateEachOther(t *testing.T) {
	f := newReflectFx(t)
	first := map[string]any{
		"action": "create", "name": "learned-flaky-tempdir", "category": "go",
		"description": "Use when a Go test fails on a shared TempDir.",
		"body":        "Run the failing test alone with -race.\nCheck for a shared `t.TempDir` across parallel tests.\nAdd t.Parallel guards or separate dirs.",
		"reason":      "the user hit this on CI",
		"evidence":    []map[string]any{{"kind": "user_message", "run_id": f.runID, "seq": -1, "quote": userQuote}},
	}
	second := map[string]any{}
	for k, v := range first {
		second[k] = v
	}
	second["name"] = "learned-shared-tempdir"
	f.say(reply(first, second))
	job := f.reflect()
	ps := f.proposals()
	if job.Status != "landed" || len(ps) != 1 || ps[0].Name != "learned-flaky-tempdir" {
		t.Fatalf("job = %+v, proposals = %+v", job, ps)
	}
	var vs []verdict
	if err := json.Unmarshal([]byte(job.Verdicts), &vs); err != nil || len(vs) != 2 || !vs[0].Accepted || vs[1].Accepted {
		t.Fatalf("verdicts = %s", job.Verdicts)
	}
	if got := vs[1].Findings[0].Reason; got != "duplicate" {
		t.Fatalf("second intent rejected as %q, want duplicate", got)
	}
}

func TestReflect_MeteredUnderLearningBucket(t *testing.T) {
	f := newReflectFx(t)
	f.exec.fn = func(executor.Request) (executor.Result, error) {
		f.learningRun("refl-run", &store.RunUsage{InputTokens: i64(900), OutputTokens: i64(100), CostUSD: ptrF(0.01)})
		in, out, cost := i64(900), i64(100), 0.01
		return executor.Result{RunID: "refl-run", FinalText: `{"intents":[]}`, InputTokens: in, OutputTokens: out, CostUSD: &cost}, nil
	}
	job := f.reflect()
	if job.RunID != "refl-run" || job.Tokens == nil || *job.Tokens != 1000 || job.CostUSD == nil || *job.CostUSD != 0.01 {
		t.Fatalf("job usage = %+v", job)
	}
	runs, in, out, cost, err := f.st.SummarizeLearningUsage(t0.Add(-time.Hour))
	if err != nil || runs != 1 || in != 900 || out != 100 || cost != 0.01 {
		t.Fatalf("learning bucket = %d %d %d %v, %v", runs, in, out, cost, err)
	}
	byTask, err := f.st.SumRunUsageByTask(f.ws.ID)
	if err != nil || len(byTask) != 0 {
		t.Fatalf("SumRunUsageByTask = %+v, %v; learning runs must not show up per task", byTask, err)
	}
}

func TestReflect_NoRawModelOutputInJobRow(t *testing.T) {
	f := newReflectFx(t)
	raw := "RAWMODELOUTPUT" + strings.Repeat("x", 600)
	f.say(raw)
	job := f.reflect()
	if job.Status != "failed" || len(job.Error) > 200 {
		t.Fatalf("job = %+v (error %d bytes)", job, len(job.Error))
	}
	row, _ := json.Marshal(job)
	if strings.Contains(string(row), strings.Repeat("x", 200)) || strings.Contains(string(row), raw) {
		t.Fatal("job row carries raw model output")
	}

	// A rejected intent keeps only clipped findings, never the raw reply.
	g := newReflectFx(t)
	g.say(grounded(g.runID, func(in map[string]any) {
		in["body"] = "do the thing `" + strings.Repeat("z", 700) + "` now"
	}))
	job = g.reflect()
	if strings.Contains(job.Verdicts, strings.Repeat("z", 201)) {
		t.Fatal("verdicts carry more than the clipped model text")
	}
}

func TestReflect_ProviderNoToolIsolationSkipped(t *testing.T) {
	f := newReflectFxFor(t, "glm")
	job := f.reflect()
	if job.Status != "skipped" || job.SkipReason != "provider_no_tool_isolation" || f.exec.count() != 0 || len(f.proposals()) != 0 {
		t.Fatalf("job = %+v, exec calls %d", job, f.exec.count())
	}
	if wm, pending := f.state(); wm != f.ids[len(f.ids)-1] || pending != 0 {
		t.Fatalf("state = %d/%d", wm, pending)
	}
}

func TestReflect_BudgetSkipsWithoutStartingRun(t *testing.T) {
	budget := config.DefaultLearning().DailyBudgetUSD
	for _, tc := range []struct {
		name     string
		cost     float64
		wantCall int
	}{
		{"exactly at the limit", budget, 0},
		{"above the limit", budget + 0.5, 0},
		{"just below the limit", budget - 0.001, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReflectFx(t)
			f.say(`{"intents":[]}`)
			f.learningRun("spent", &store.RunUsage{CostUSD: ptrF(tc.cost), InputTokens: i64(10), OutputTokens: i64(10)})
			job := f.reflect()
			if f.exec.count() != tc.wantCall {
				t.Fatalf("executor calls = %d, want %d (job %+v)", f.exec.count(), tc.wantCall, job)
			}
			if tc.wantCall == 0 && (job.Status != "skipped" || job.SkipReason != "budget") {
				t.Fatalf("job = %+v, want skipped: budget", job)
			}
		})
	}
	t.Run("token budget", func(t *testing.T) {
		f := newReflectFx(t)
		f.setConfig(func(c *config.LearningConfig) { c.DailyBudgetTokens = 1000 })
		f.learningRun("spent", &store.RunUsage{CostUSD: ptrF(0.001), InputTokens: i64(900), OutputTokens: i64(50)})
		job := f.reflect()
		if f.exec.count() != 0 || job.SkipReason != "budget" {
			t.Fatalf("job = %+v, exec calls %d", job, f.exec.count())
		}
	})
	t.Run("yesterday's spend does not count", func(t *testing.T) {
		f := newReflectFx(t)
		f.say(`{"intents":[]}`)
		f.clk.advance(-48 * time.Hour)
		f.learningRun("old", &store.RunUsage{CostUSD: ptrF(5), InputTokens: i64(1), OutputTokens: i64(1)})
		f.clk.advance(48 * time.Hour)
		if f.reflect(); f.exec.count() != 1 {
			t.Fatalf("executor calls = %d, want 1", f.exec.count())
		}
	})
}

func TestReflect_UnreportedUsageCountsAsWindow(t *testing.T) {
	// One unreported run is priced at windowKB*1024/4 = 30720 tokens.
	setup := func(t *testing.T, unreported int) *reflectFx {
		f := newReflectFx(t)
		f.setConfig(func(c *config.LearningConfig) { c.DailyBudgetTokens = 61440 })
		f.say(`{"intents":[]}`)
		for i := 0; i < unreported; i++ {
			f.learningRun("u"+string(rune('a'+i)), nil)
		}
		return f
	}
	if f := setup(t, 1); func() bool { f.reflect(); return f.exec.count() != 1 }() {
		t.Fatal("one unreported run plus a small window should fit 61440 tokens")
	}
	f := setup(t, 2)
	job := f.reflect()
	if f.exec.count() != 0 || job.SkipReason != "budget" {
		t.Fatalf("two unreported runs must exhaust the budget: job %+v, calls %d", job, f.exec.count())
	}
}

func reflectorIntent(quote string) gate.Intent {
	return gate.Intent{Reason: "r", Evidence: []gate.Evidence{{Kind: gate.EvUserMessage, Quote: quote}}}
}

func TestReflect_UnboundChatFallsBackToRunProvider(t *testing.T) {
	f := newReflectFxFor(t, "") // chat.provider unset; its run is claude-native
	f.say(`{"intents":[]}`)
	if job := f.reflect(); job.Status != "empty" || f.exec.count() != 1 {
		t.Fatalf("job = %+v, exec calls %d", job, f.exec.count())
	}
}
