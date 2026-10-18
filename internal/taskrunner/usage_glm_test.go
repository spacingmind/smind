package taskrunner

import (
	"encoding/json"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
)

// Raw PromptResponse payloads captured from the real glm-acp-agent binary
// (fake model); see docs/plans/completed/dogfood-gap-fixes.md.
const (
	// 1.14.0: one prompt, 6 model calls: the summed total.
	glmRawSixCalls = `{"stopReason":"end_turn","usage":{"inputTokens":21000,"outputTokens":210,"totalTokens":21210,"cachedReadTokens":18900,"thoughtTokens":0}}`
	// 1.14.0: a second prompt, same session and process, 6 more calls. It
	// is that prompt's own total, NOT session-cumulative (not 78000).
	glmRawSecondPrompt = `{"stopReason":"end_turn","usage":{"inputTokens":57000,"outputTokens":570,"totalTokens":57570,"cachedReadTokens":51300,"thoughtTokens":0}}`
	// 1.14.0 with --max-turns 3: the sum of its 3 calls.
	glmRawMaxTurns = `{"stopReason":"max_turn_requests","usage":{"inputTokens":6000,"outputTokens":60,"totalTokens":6060,"cachedReadTokens":5400,"thoughtTokens":0}}`
	// 1.3.0 for comparison: 6 calls but only the last call's usage.
	glmRawOldLastCallOnly = `{"stopReason":"end_turn","usage":{"inputTokens":6000,"outputTokens":60,"totalTokens":6060,"cachedReadTokens":5400,"thoughtTokens":0}}`
)

// decodeGLMRaw decodes a raw PromptResponse the way acp.Client.Prompt does
// (its promptResult: stopReason + usage).
func decodeGLMRaw(t *testing.T, raw string) (string, acp.PromptUsage) {
	t.Helper()
	var res struct {
		StopReason string           `json:"stopReason"`
		Usage      *acp.PromptUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if res.Usage == nil {
		t.Fatalf("no usage in %s", raw)
	}
	return res.StopReason, *res.Usage
}

func glmCum(t *testing.T, raw string) cumulativeUsage {
	t.Helper()
	_, pu := decodeGLMRaw(t, raw)
	return newCumulativeUsage(pu, true, acp.UsageUpdate{}, false)
}

func TestACPPromptUsage_DecodesGLMRawResponse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, raw, stop string
		in, out, cached int64
	}{
		{"six calls", glmRawSixCalls, "end_turn", 21000, 210, 18900},
		{"second prompt", glmRawSecondPrompt, "end_turn", 57000, 570, 51300},
		{"max turns", glmRawMaxTurns, "max_turn_requests", 6000, 60, 5400},
		{"old last call only", glmRawOldLastCallOnly, "end_turn", 6000, 60, 5400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			stop, pu := decodeGLMRaw(t, c.raw)
			if stop != c.stop {
				t.Errorf("stopReason = %q, want %q", stop, c.stop)
			}
			if !eqI(pu.InputTokens, i64(c.in)) || !eqI(pu.OutputTokens, i64(c.out)) || !eqI(pu.CachedReadTokens, i64(c.cached)) {
				t.Errorf("usage = in %s out %s cached %s", fmtI(pu.InputTokens), fmtI(pu.OutputTokens), fmtI(pu.CachedReadTokens))
			}
			if !eqI(pu.ThoughtTokens, i64(0)) || pu.CachedWriteTokens != nil {
				t.Errorf("thought = %s, cachedWrite = %s", fmtI(pu.ThoughtTokens), fmtI(pu.CachedWriteTokens))
			}
		})
	}
}

// glm 1.14.0 reports each prompt's own total, and smind spawns one process
// per run, so the instance always differs and each run is charged exactly
// what its process reported.
func TestDiffACPUsage_GLMPerPromptTotalsNewInstancePerRun(t *testing.T) {
	t.Parallel()
	u1, snap1 := diffACPUsage(nil, "s", "proc-1", glmCum(t, glmRawSixCalls))
	wantInts(t, u1, map[string]int64{"input": 21000, "output": 210, "cachedInput": 18900, "reasoning": 0})
	if u1.Source != UsageSourceACPPromptUsage {
		t.Errorf("run 1 source = %q", u1.Source)
	}
	u2, _ := diffACPUsage(snap1, "s", "proc-2", glmCum(t, glmRawSecondPrompt))
	wantInts(t, u2, map[string]int64{"input": 57000, "output": 570, "cachedInput": 51300, "reasoning": 0})
	if u2.Source != UsageSourceACPPromptUsage {
		t.Errorf("run 2 source = %q", u2.Source)
	}
}

func TestDiffACPUsage_GLMMaxTurnRunCountsAllTurns(t *testing.T) {
	t.Parallel()
	u, _ := diffACPUsage(nil, "s", "proc-1", glmCum(t, glmRawMaxTurns))
	wantInts(t, u, map[string]int64{"input": 6000, "output": 60, "cachedInput": 5400, "reasoning": 0})
	if u.Source != UsageSourceACPPromptUsage {
		t.Errorf("source = %q", u.Source)
	}
}

// Documents the hazard: if ONE process served both prompts, differencing
// GLM's per-prompt totals charges 57000-21000 = 36000 for run 2, not 57000.
// That is why process-per-run matters; a future shared-process design must
// not difference GLM's per-prompt totals.
func TestDiffACPUsage_SameInstancePerPromptTotalsWouldUndercount(t *testing.T) {
	t.Parallel()
	_, snap1 := diffACPUsage(nil, "s", "proc-1", glmCum(t, glmRawSixCalls))
	u2, _ := diffACPUsage(snap1, "s", "proc-1", glmCum(t, glmRawSecondPrompt))
	wantInts(t, u2, map[string]int64{"input": 36000, "output": 360, "cachedInput": 32400, "reasoning": 0})
}
