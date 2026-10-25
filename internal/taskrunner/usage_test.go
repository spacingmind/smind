package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/workspace"
)

func withoutUsage(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Type != EventTypeUsage {
			out = append(out, e)
		}
	}
	return out
}

// runForUsage runs one turn and returns the turn's single usage event,
// failing unless it is immediately followed by the Done event.
func runForUsage(t *testing.T, r *Runner, task int64, provider Provider, prompt string) Usage {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task, task, provider, prompt, nil, PermissionSettings{}, "", testModelFor(provider), events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	var usage *Usage
	for i, e := range got {
		if e.Type != EventTypeUsage {
			continue
		}
		if usage != nil {
			t.Fatalf("more than one usage event: %+v", got)
		}
		if i+1 >= len(got) || got[i+1].Type != EventTypeDone {
			t.Fatalf("usage event at %d is not immediately before Done: %+v", i, got)
		}
		usage = e.Usage
	}
	if usage == nil {
		t.Fatalf("no usage event in %+v", got)
	}
	return *usage
}

func writeScenario(t *testing.T, wm *workspace.Manager, taskID int64, scenario string) {
	t.Helper()
	task, err := wm.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
}

func i64(v int64) *int64 { return &v }

// sameInstanceACP overrides the agent instance a real acp.Client reports,
// standing in for one long-lived agent process whose session-cumulative
// counters persist across runs. (A plain glmRunner spawns a fresh process,
// so a fresh instance, per run.)
type sameInstanceACP struct {
	*acp.Client
	id string
}

func (c sameInstanceACP) InstanceID() string { return c.id }

func glmRunnerSameAgent(wm *workspace.Manager) *Runner {
	r := New(wm)
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		c, err := acp.New([]string{fakeACPAgentPath}, opts...)
		if err != nil {
			return nil, err
		}
		return sameInstanceACP{Client: c, id: "long-lived-agent"}, nil
	}
	return r
}

func eqI(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func eqF(a, b *float64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func fmtI(v *int64) string {
	if v == nil {
		return "nil"
	}
	b, _ := json.Marshal(*v)
	return string(b)
}

// wantInts compares the nullable token/context fields of u to want, keyed by
// field name; a field absent from want must be nil.
func wantInts(t *testing.T, u Usage, want map[string]int64) {
	t.Helper()
	got := map[string]*int64{
		"input": u.Input, "cachedInput": u.CachedInput, "cacheWrite": u.CacheWrite,
		"output": u.Output, "reasoning": u.Reasoning,
		"contextUsed": u.ContextUsed, "contextSize": u.ContextSize,
	}
	for name, g := range got {
		var w *int64
		if v, ok := want[name]; ok {
			w = i64(v)
		}
		if !eqI(g, w) {
			t.Errorf("%s = %s, want %s", name, fmtI(g), fmtI(w))
		}
	}
}

func TestUsage_ClaudeResultParsed(t *testing.T) {
	t.Parallel()

	t.Run("pure", func(t *testing.T) {
		t.Parallel()
		u := claudeUsage(claudecode.ResultMessage{
			TotalCostUSD: 0.5,
			ModelUsage: map[string]claudecode.ModelUsage{
				"a-small": {InputTokens: 10, OutputTokens: 5, CostUSD: 0.1, ContextWindow: 200000},
				"b-big": {
					InputTokens: 100, OutputTokens: 40, CacheReadInputTokens: 300,
					CacheCreationInputTokens: 20, CostUSD: 0.4, ContextWindow: 1000000,
				},
			},
		})
		wantInts(t, u, map[string]int64{"input": 110, "cachedInput": 300, "cacheWrite": 20, "output": 45, "contextSize": 1000000})
		if u.Source != UsageSourceClaudeResult || u.Model == nil || *u.Model != "b-big" || u.CostUSD == nil || *u.CostUSD != 0.5 {
			t.Fatalf("usage = %+v", u)
		}
	})

	t.Run("cost falls back to per-model sum, tie goes to the first name, no window", func(t *testing.T) {
		t.Parallel()
		u := claudeUsage(claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{
			"m2": {InputTokens: 5, CostUSD: 0.25},
			"m1": {InputTokens: 5, CostUSD: 0.5},
		}})
		if u.CostUSD == nil || *u.CostUSD != 0.75 || u.Model == nil || *u.Model != "m1" || u.ContextSize != nil {
			t.Fatalf("usage = %+v", u)
		}
	})

	t.Run("no model usage stays nil", func(t *testing.T) {
		t.Parallel()
		u := claudeUsage(claudecode.ResultMessage{})
		wantInts(t, u, nil)
		if u.Source != UsageSourceNotReported || u.CostUSD != nil || u.Model != nil {
			t.Fatalf("usage = %+v, want not_reported and all nil", u)
		}
		u = claudeUsage(claudecode.ResultMessage{TotalCostUSD: 1.5})
		wantInts(t, u, nil)
		if u.Source != UsageSourceClaudeResult || u.CostUSD == nil || *u.CostUSD != 1.5 || u.Model != nil {
			t.Fatalf("usage = %+v, want only the cost", u)
		}
	})

	t.Run("event before done", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "usage")
		u := runForUsage(t, claudeNativeRunner(t, wm), task.ID, ProviderClaudeNative, "hi")
		wantInts(t, u, map[string]int64{"input": 110, "cachedInput": 300, "cacheWrite": 20, "output": 45, "contextSize": 1000000})
		if u.Source != UsageSourceClaudeResult || u.Model == nil || *u.Model != "claude-big" || u.CostUSD == nil || *u.CostUSD != 0.5 {
			t.Fatalf("usage = %+v", u)
		}
	})

	t.Run("event without model usage", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "usage-no-model")
		u := runForUsage(t, claudeNativeRunner(t, wm), task.ID, ProviderClaudeNative, "hi")
		wantInts(t, u, nil)
		if u.Source != UsageSourceNotReported || u.CostUSD != nil || u.Model != nil {
			t.Fatalf("usage = %+v, want not_reported and all nil", u)
		}
	})
}

func TestUsage_CodexTokenUsageDifferencedTotal(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "token-usage")
	r := codexRunner(wm)

	// Turn 1 is a tool loop: three model calls (the fake's `last`s are
	// 10/2, 30/7 and 50/8) whose thread total ends at in 90, cached 60,
	// cache write 5, out 17, reasoning 3. The turn used all of that: not the
	// final call's `last` (50/8, what keeping tokenUsage.last gave), and not
	// the sum of the notifications' totals.
	u := runForUsage(t, r, task.ID, ProviderCodexNative, "hi")
	wantInts(t, u, map[string]int64{"input": 90, "cachedInput": 60, "cacheWrite": 5, "output": 17, "reasoning": 3, "contextSize": 200000})
	if u.Source != UsageSourceCodexTokenUsage || u.CostUSD != nil || u.Model != nil {
		t.Fatalf("usage = %+v", u)
	}
	if s := u.SessionSnapshot; s == nil || s.SessionID != "fake-thread-1" || !eqI(s.Input, i64(90)) || !eqI(s.Output, i64(17)) {
		t.Fatalf("snapshot = %+v, want thread total 90/17 for the thread", s)
	}
	h, _ := r.sessionStore.Get(task.ID)
	if s := usageSnapshotFromHandle(h); s == nil || !eqI(s.Input, i64(90)) || !eqI(s.CacheWrite, i64(5)) {
		t.Fatalf("stored snapshot = %+v, want the thread total in the handle's Metadata", s)
	}

	// Turn 2 on the same thread (a new process, whose total continues from
	// the first turn's): one call of 60/4. The turn's own usage is the
	// growth of the total.
	writeScenario(t, wm, task.ID, "token-usage-turn2")
	u = runForUsage(t, r, task.ID, ProviderCodexNative, "again")
	wantInts(t, u, map[string]int64{"input": 60, "cachedInput": 50, "cacheWrite": 0, "output": 4, "reasoning": 0, "contextSize": 200000})
	if s := u.SessionSnapshot; s == nil || !eqI(s.Input, i64(150)) || !eqI(s.Output, i64(21)) {
		t.Fatalf("turn 2 snapshot = %+v, want thread total 150/21", s)
	}

	// A stored snapshot for another thread id is no baseline: the thread's
	// whole total (150/110/5/21/3) is charged to this turn.
	h, _ = r.sessionStore.Get(task.ID)
	h.SessionID = "some-older-thread"
	r.sessionStore.Set(task.ID, h)
	u = runForUsage(t, r, task.ID, ProviderCodexNative, "again")
	wantInts(t, u, map[string]int64{"input": 150, "cachedInput": 110, "cacheWrite": 5, "output": 21, "reasoning": 3, "contextSize": 200000})

	t.Run("missing fields stay nil", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "token-usage-partial")
		u := runForUsage(t, codexRunner(wm), task.ID, ProviderCodexNative, "hi")
		wantInts(t, u, map[string]int64{"input": 4, "output": 1})
	})

	t.Run("none reported", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "")
		u := runForUsage(t, codexRunner(wm), task.ID, ProviderCodexNative, "hi")
		wantInts(t, u, nil)
		if u.Source != UsageSourceNotReported {
			t.Fatalf("source = %q, want not_reported", u.Source)
		}
	})
}

func TestUsage_ACPPromptUsageDifferenced(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "usage in=100 out=20")
	r := glmRunnerSameAgent(wm)

	u := runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 100, "output": 20})
	if u.Source != UsageSourceACPPromptUsage || u.SessionSnapshot == nil || !eqI(u.SessionSnapshot.Input, i64(100)) || !eqI(u.SessionSnapshot.Output, i64(20)) {
		t.Fatalf("run 1 usage = %+v snapshot = %+v", u, u.SessionSnapshot)
	}
	h, _ := r.sessionStore.Get(task.ID)
	if s := usageSnapshotFromHandle(h); s == nil || s.SessionID != "fake-session-1" || !eqI(s.Input, i64(100)) || !eqI(s.Output, i64(20)) {
		t.Fatalf("stored snapshot = %+v, want 100/20 for fake-session-1", s)
	}

	// Same session, cumulative 160/50 -> this run used 60/30.
	writeScenario(t, wm, task.ID, "usage in=160 out=50 thought=7")
	u = runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 60, "output": 30, "reasoning": 7})
	if u.SessionSnapshot == nil || !eqI(u.SessionSnapshot.Input, i64(160)) || !eqI(u.SessionSnapshot.Output, i64(50)) {
		t.Fatalf("run 2 snapshot = %+v, want cumulative 160/50", u.SessionSnapshot)
	}

	// A different stored session id (the agent hands back a new one) starts
	// from zero and drops the old snapshot.
	h, _ = r.sessionStore.Get(task.ID)
	h.SessionID = "some-older-session"
	r.sessionStore.Set(task.ID, h)
	writeScenario(t, wm, task.ID, "usage in=9 out=3")
	u = runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 9, "output": 3})
	h, _ = r.sessionStore.Get(task.ID)
	if s := usageSnapshotFromHandle(h); s == nil || s.SessionID != "fake-session-1" || !eqI(s.Input, i64(9)) {
		t.Fatalf("stored snapshot = %+v, want 9/3 for fake-session-1", s)
	}
}

func TestUsage_ACPUsageUpdateContextAndCost(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "usage used=4000 size=128000 cost=0.25")
	r := glmRunnerSameAgent(wm)

	u := runForUsage(t, r, task.ID, ProviderGLM, "hi")
	// Context from the LAST usage_update (the fake sends a used=1 decoy first).
	wantInts(t, u, map[string]int64{"contextUsed": 4000, "contextSize": 128000})
	if u.Source != UsageSourceACPUsageUpdate || u.CostUSD == nil || *u.CostUSD != 0.25 {
		t.Fatalf("run 1 usage = %+v", u)
	}

	// Cumulative cost 0.75 -> this run cost 0.5.
	writeScenario(t, wm, task.ID, "usage used=9000 size=128000 cost=0.75")
	u = runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"contextUsed": 9000, "contextSize": 128000})
	if u.CostUSD == nil || *u.CostUSD != 0.5 {
		t.Fatalf("run 2 cost = %v, want 0.5", u.CostUSD)
	}

	// Both reports: tokens from PromptResponse.usage, context and cost from
	// usage_update, source acp_prompt_usage.
	writeScenario(t, wm, task.ID, "usage in=10 out=2 used=100 size=1000 cost=1")
	u = runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 10, "output": 2, "contextUsed": 100, "contextSize": 1000})
	if u.Source != UsageSourceACPPromptUsage || u.CostUSD == nil || *u.CostUSD != 0.25 {
		t.Fatalf("run 3 usage = %+v, want cost 0.25 (1 - 0.75)", u)
	}

	// Only a USD cost counts.
	wm2, task2 := newTestTask(t, "usage used=1 size=2 cost=3 currency=EUR")
	u = runForUsage(t, glmRunner(wm2), task2.ID, ProviderGLM, "hi")
	if u.CostUSD != nil {
		t.Fatalf("EUR cost = %v, want nil", *u.CostUSD)
	}
}

func TestUsage_ACPNothingReportedIsNull(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := glmRunner(wm)
	u := runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, nil)
	if u.Source != UsageSourceNotReported || u.CostUSD != nil || u.Model != nil || u.SessionSnapshot != nil {
		t.Fatalf("usage = %+v, want not_reported and all nil with no snapshot", u)
	}
	if h, _ := r.sessionStore.Get(task.ID); len(h.Metadata) != 0 {
		t.Fatalf("Metadata = %s, want none", h.Metadata)
	}
}

func TestUsage_ACPSnapshotSurvivesNotReportedRun(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "usage in=100 out=20")
	r := glmRunnerSameAgent(wm)
	runForUsage(t, r, task.ID, ProviderGLM, "hi")

	writeScenario(t, wm, task.ID, "")
	runForUsage(t, r, task.ID, ProviderGLM, "hi")

	writeScenario(t, wm, task.ID, "usage in=130 out=25")
	u := runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 30, "output": 5})
}

func TestUsage_ACPNewAgentProcessResetsBaseline(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "usage in=100 out=20")
	r := glmRunner(wm) // a new agent process, so a new instance, every run

	u := runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 100, "output": 20})

	// The resumed session's counters restarted in the new process and
	// happen to be above the stored snapshot (130/40 vs 100/20): nothing
	// went down, but 30/20 would under-count -- the run used 130/40.
	writeScenario(t, wm, task.ID, "usage in=130 out=40")
	u = runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 130, "output": 40})
	h, _ := r.sessionStore.Get(task.ID)
	if s := usageSnapshotFromHandle(h); s == nil || s.Instance == "" || !eqI(s.Input, i64(130)) {
		t.Fatalf("stored snapshot = %+v, want 130/40 tagged with the agent instance", s)
	}
}

func TestDiffCodexUsage(t *testing.T) {
	t.Parallel()
	total := func(in, out int64) codex.TokenUsage {
		return codex.TokenUsage{InputTokens: i64(in), OutputTokens: i64(out)}
	}
	prev := &UsageSnapshot{SessionID: "th", Input: i64(100), Output: i64(20)}

	u, snap := diffCodexUsage(prev, "th", total(160, 50), true)
	wantInts(t, u, map[string]int64{"input": 60, "output": 30})
	if u.Source != UsageSourceCodexTokenUsage || !eqI(snap.Input, i64(160)) {
		t.Fatalf("usage = %+v snapshot = %+v", u, snap)
	}

	// The thread's total went down (rollout compacted or replaced): restart
	// from zero.
	u, _ = diffCodexUsage(prev, "th", total(30, 5), true)
	wantInts(t, u, map[string]int64{"input": 30, "output": 5})

	u, snap = diffCodexUsage(prev, "th", codex.TokenUsage{}, false)
	if u.Source != UsageSourceNotReported || snap != nil {
		t.Fatalf("nothing reported: usage = %+v snap = %+v", u, snap)
	}
}

func TestDiffACPUsage(t *testing.T) {
	t.Parallel()
	cum := func(in, out int64) cumulativeUsage {
		return cumulativeUsage{Input: i64(in), Output: i64(out), hasPrompt: true}
	}
	prev := &UsageSnapshot{SessionID: "s", Instance: "i1", Input: i64(100), Output: i64(20), CostUSD: new(float64)}

	t.Run("counter reset uses a zero baseline", func(t *testing.T) {
		t.Parallel()
		u, snap := diffACPUsage(prev, "s", "i1", cum(30, 25))
		wantInts(t, u, map[string]int64{"input": 30, "output": 25})
		if !eqI(snap.Input, i64(30)) {
			t.Fatalf("snapshot = %+v", snap)
		}
	})

	t.Run("a different agent instance uses a zero baseline", func(t *testing.T) {
		t.Parallel()
		// Counters restarted in the new process but already exceed the
		// snapshot (130 >= 100, 40 >= 20): no counter went down, yet the
		// delta must still be the whole 130/40, not 30/20.
		u, snap := diffACPUsage(prev, "s", "i2", cum(130, 40))
		wantInts(t, u, map[string]int64{"input": 130, "output": 40})
		if snap.Instance != "i2" || !eqI(snap.Input, i64(130)) {
			t.Fatalf("snapshot = %+v, want instance i2 at 130/40", snap)
		}
		// Same instance, same figures: the ordinary difference.
		u, _ = diffACPUsage(prev, "s", "i1", cum(130, 40))
		wantInts(t, u, map[string]int64{"input": 30, "output": 20})
	})

	t.Run("a snapshot without an instance (stored before instances were tagged) is not a baseline for a tagged agent", func(t *testing.T) {
		t.Parallel()
		legacy := &UsageSnapshot{SessionID: "s", Input: i64(100), Output: i64(20)}
		u, _ := diffACPUsage(legacy, "s", "i2", cum(130, 40))
		wantInts(t, u, map[string]int64{"input": 130, "output": 40})
	})

	t.Run("a field the agent omits stays nil", func(t *testing.T) {
		t.Parallel()
		u, snap := diffACPUsage(prev, "s", "i1", cumulativeUsage{Input: i64(150), hasPrompt: true})
		wantInts(t, u, map[string]int64{"input": 50})
		if !eqI(snap.Output, i64(20)) {
			t.Fatalf("snapshot output = %v, want prior 20 kept", fmtI(snap.Output))
		}
	})

	t.Run("cost counter reset", func(t *testing.T) {
		t.Parallel()
		p := &UsageSnapshot{SessionID: "s", CostUSD: func() *float64 { v := 5.0; return &v }()}
		cost := 2.0
		u, _ := diffACPUsage(p, "s", "", cumulativeUsage{CostUSD: &cost, hasUpdate: true})
		if !eqF(u.CostUSD, &cost) {
			t.Fatalf("cost = %v, want 2", u.CostUSD)
		}
	})

	t.Run("nothing reported", func(t *testing.T) {
		t.Parallel()
		u, snap := diffACPUsage(prev, "s", "i1", cumulativeUsage{})
		if u.Source != UsageSourceNotReported || snap != nil || u.SessionSnapshot != nil {
			t.Fatalf("usage = %+v snap = %+v", u, snap)
		}
	})
}

// TestUsageEvent_NoPromptOrSecret proves a usage event, snapshot included,
// carries neither the prompt text nor a secret from the environment.
// Not parallel: it sets an environment variable.
func TestUsageEvent_NoPromptOrSecret(t *testing.T) {
	const (
		prompt = "PROMPT-TEXT-xyzzy-do-not-leak"
		secret = "sk-test-SECRET-plugh-do-not-leak"
	)
	t.Setenv("ANTHROPIC_API_KEY", secret)
	t.Setenv("ZAI_API_KEY", secret)

	run := func(name string, provider Provider, scenario string, mk func(*workspace.Manager) *Runner) {
		wm, task := newTestTask(t, scenario)
		u := runForUsage(t, mk(wm), task.ID, provider, prompt)
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if u.Source == UsageSourceNotReported {
			t.Fatalf("%s: scenario reported no usage; the check would prove nothing", name)
		}
		if s := string(data); strings.Contains(s, prompt) || strings.Contains(s, secret) {
			t.Fatalf("%s: usage JSON leaks the prompt or a secret: %s", name, s)
		}
	}
	run("claude", ProviderClaudeNative, "usage", func(wm *workspace.Manager) *Runner { return claudeNativeRunner(t, wm) })
	run("codex", ProviderCodexNative, "token-usage", codexRunner)
	run("acp", ProviderGLM, "usage in=1 out=2 used=3 size=4 cost=0.5", glmRunner)
}

func claudeResult(cost float64, models map[string]claudecode.ModelUsage) claudecode.ResultMessage {
	return claudecode.ResultMessage{SessionID: "sess-1", StopReason: "end_turn", NumTurns: 1, TotalCostUSD: cost, ModelUsage: models}
}

func haiku(in, out, cacheRead int, cost float64) map[string]claudecode.ModelUsage {
	return map[string]claudecode.ModelUsage{"claude-haiku-5-5": {InputTokens: in, OutputTokens: out, CacheReadInputTokens: cacheRead, CostUSD: cost}}
}

// The CLI restores a resumed session's running totals, so each result is
// cumulative; a turn's usage is the difference from the previous turn's.
func TestClaudeTurnUsage_IsTheDifferenceFromThePreviousTurnsTotals(t *testing.T) {
	t.Parallel()
	turn1 := claudeResult(0.010, haiku(1000, 400, 5000, 0.010))
	turn2 := claudeResult(0.025, haiku(1010, 2600, 9000, 0.025))

	first := claudeTurnUsage(turn1, nil)
	if first.Output == nil || *first.Output != 400 || first.CostUSD == nil || *first.CostUSD != 0.010 {
		t.Fatalf("first turn (no baseline) = output %s cost %v, want the result as is: 400 / 0.010", fmtI(first.Output), first.CostUSD)
	}

	second := claudeTurnUsage(turn2, claudeTotalsOf(turn1))
	if second.Input == nil || *second.Input != 10 || second.Output == nil || *second.Output != 2200 || second.CachedInput == nil || *second.CachedInput != 4000 {
		t.Fatalf("second turn = input %s output %s cached %s, want 10 / 2200 / 4000", fmtI(second.Input), fmtI(second.Output), fmtI(second.CachedInput))
	}
	if second.CostUSD == nil || *second.CostUSD < 0.0149 || *second.CostUSD > 0.0151 {
		t.Fatalf("second turn cost = %v, want 0.015 (0.025 - 0.010)", second.CostUSD)
	}
}

// A counter that goes backwards means a fresh session (a failed resume), not
// an extension: the result is that turn's own usage.
func TestClaudeTurnUsage_AResetIsTakenAsIs(t *testing.T) {
	t.Parallel()
	before := claudeResult(0.50, haiku(1000, 9000, 50000, 0.50))
	fresh := claudeResult(0.004, haiku(900, 300, 0, 0.004))
	u := claudeTurnUsage(fresh, claudeTotalsOf(before))
	if u.Output == nil || *u.Output != 300 || u.CostUSD == nil || *u.CostUSD != 0.004 {
		t.Fatalf("reset turn = output %s cost %v, want the result as is: 300 / 0.004", fmtI(u.Output), u.CostUSD)
	}
}

// A model the baseline never saw (the user switched model) counts in full.
func TestClaudeTurnUsage_ANewModelCountsInFull(t *testing.T) {
	t.Parallel()
	turn1 := claudeResult(0.010, haiku(1000, 400, 0, 0.010))
	both := haiku(1000, 400, 0, 0.010)
	both["claude-sonnet-5-5"] = claudecode.ModelUsage{InputTokens: 50, OutputTokens: 700, CostUSD: 0.020}
	turn2 := claudeResult(0.030, both)
	u := claudeTurnUsage(turn2, claudeTotalsOf(turn1))
	if u.Output == nil || *u.Output != 700 || u.Model == nil || *u.Model != "claude-sonnet-5-5" {
		t.Fatalf("turn after a model switch = output %s model %v, want 700 on sonnet", fmtI(u.Output), u.Model)
	}
}

// End to end through RunPrompt: successive turns of one chat whose results are
// cumulative each report their own usage.
func TestRunner_RunPrompt_ClaudeNative_UsagePerTurnNotCumulative(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := New(wm)
	results := []claudecode.ResultMessage{
		claudeResult(0.010, haiku(1000, 400, 0, 0.010)),
		claudeResult(0.025, haiku(1010, 2600, 0, 0.025)),
		claudeResult(0.026, haiku(1012, 2700, 0, 0.026)),
	}
	turn := 0
	r.newClaudeClient = func(string, ...claudecode.Option) (claudeBackend, error) {
		res := results[turn]
		turn++
		return stubClaudeResult{result: res}, nil
	}
	var outputs []int64
	for range results {
		u := runForUsage(t, r, task.ID, ProviderClaudeNative, "hi")
		if u.Output == nil {
			t.Fatalf("turn %d reported no output tokens", turn)
		}
		outputs = append(outputs, *u.Output)
	}
	if outputs[0] != 400 || outputs[1] != 2200 || outputs[2] != 100 {
		t.Fatalf("per-turn output tokens = %v, want [400 2200 100]", outputs)
	}
}

type stubClaudeResult struct{ result claudecode.ResultMessage }

func (s stubClaudeResult) Prompt(_ context.Context, _ string, updates chan<- claudecode.Message) (claudecode.ResultMessage, error) {
	close(updates)
	return s.result, nil
}

func (s stubClaudeResult) PromptBlocks(_ context.Context, _ []claudecode.PromptBlock, updates chan<- claudecode.Message) (claudecode.ResultMessage, error) {
	close(updates)
	return s.result, nil
}

func (stubClaudeResult) SetPermissionMode(context.Context, string) error { return nil }
func (stubClaudeResult) SetModel(context.Context, *string) error         { return nil }
func (stubClaudeResult) Close() error                                    { return nil }
