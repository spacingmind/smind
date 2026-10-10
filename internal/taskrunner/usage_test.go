package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
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
		errCh <- r.RunPrompt(context.Background(), task, task, provider, prompt, nil, PermissionSettings{}, "", events)
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

func TestUsage_CodexTokenUsageLastOfTurn(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "token-usage")
	u := runForUsage(t, codexRunner(wm), task.ID, ProviderCodexNative, "hi")
	// The second notification's tokenUsage.last: not the sum of both, and
	// not total.
	wantInts(t, u, map[string]int64{"input": 30, "cachedInput": 20, "cacheWrite": 5, "output": 7, "reasoning": 3, "contextSize": 200000})
	if u.Source != UsageSourceCodexTokenUsage || u.CostUSD != nil || u.Model != nil || u.SessionSnapshot != nil {
		t.Fatalf("usage = %+v", u)
	}

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
	r := glmRunner(wm)

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
	r := glmRunner(wm)

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
	r := glmRunner(wm)
	runForUsage(t, r, task.ID, ProviderGLM, "hi")

	writeScenario(t, wm, task.ID, "")
	runForUsage(t, r, task.ID, ProviderGLM, "hi")

	writeScenario(t, wm, task.ID, "usage in=130 out=25")
	u := runForUsage(t, r, task.ID, ProviderGLM, "hi")
	wantInts(t, u, map[string]int64{"input": 30, "output": 5})
}

func TestDiffACPUsage(t *testing.T) {
	t.Parallel()
	cum := func(in, out int64) cumulativeUsage {
		return cumulativeUsage{Input: i64(in), Output: i64(out), hasPrompt: true}
	}
	prev := &UsageSnapshot{SessionID: "s", Input: i64(100), Output: i64(20), CostUSD: new(float64)}

	t.Run("counter reset uses a zero baseline", func(t *testing.T) {
		t.Parallel()
		u, snap := diffACPUsage(prev, "s", cum(30, 25))
		wantInts(t, u, map[string]int64{"input": 30, "output": 25})
		if !eqI(snap.Input, i64(30)) {
			t.Fatalf("snapshot = %+v", snap)
		}
	})

	t.Run("a field the agent omits stays nil", func(t *testing.T) {
		t.Parallel()
		u, snap := diffACPUsage(prev, "s", cumulativeUsage{Input: i64(150), hasPrompt: true})
		wantInts(t, u, map[string]int64{"input": 50})
		if !eqI(snap.Output, i64(20)) {
			t.Fatalf("snapshot output = %v, want prior 20 kept", fmtI(snap.Output))
		}
	})

	t.Run("cost counter reset", func(t *testing.T) {
		t.Parallel()
		p := &UsageSnapshot{SessionID: "s", CostUSD: func() *float64 { v := 5.0; return &v }()}
		cost := 2.0
		u, _ := diffACPUsage(p, "s", cumulativeUsage{CostUSD: &cost, hasUpdate: true})
		if !eqF(u.CostUSD, &cost) {
			t.Fatalf("cost = %v, want 2", u.CostUSD)
		}
	})

	t.Run("nothing reported", func(t *testing.T) {
		t.Parallel()
		u, snap := diffACPUsage(prev, "s", cumulativeUsage{})
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
