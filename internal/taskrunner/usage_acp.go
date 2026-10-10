package taskrunner

import (
	"encoding/json"

	"github.com/spacingmind/smind/internal/acp"
)

// cumulativeUsage is what an ACP agent reported this turn, all of it
// session-cumulative except the context figures: PromptResponse.usage's
// counters, and a usage_update's cost, used and size. nil = not reported.
type cumulativeUsage struct {
	Input, CachedInput, CacheWrite, Output, Reasoning *int64
	CostUSD                                           *float64
	ContextUsed, ContextSize                          *int64

	hasPrompt bool // PromptResponse.usage arrived
	hasUpdate bool // a usage_update arrived
}

// newCumulativeUsage merges PromptResponse.usage (prompt, if ok) and the
// turn's last usage_update (update, if gotUpdate). Only a USD cost counts.
func newCumulativeUsage(prompt acp.PromptUsage, ok bool, update acp.UsageUpdate, gotUpdate bool) cumulativeUsage {
	var c cumulativeUsage
	if ok {
		c.hasPrompt = true
		c.Input, c.CachedInput, c.CacheWrite = prompt.InputTokens, prompt.CachedReadTokens, prompt.CachedWriteTokens
		c.Output, c.Reasoning = prompt.OutputTokens, prompt.ThoughtTokens
	}
	if gotUpdate {
		c.hasUpdate = true
		c.ContextUsed, c.ContextSize = update.Used, update.Size
		if update.Cost != nil && update.Cost.Currency == "USD" {
			c.CostUSD = update.Cost.Amount
		}
	}
	return c
}

// diffACPUsage turns an ACP agent's session-cumulative report into the
// run's own Usage (ADR-0020 §2): cum minus prev, the snapshot stored after
// the previous run. A nil prev or one for another session starts from zero,
// and so does a call where any counter went down (the agent's counters
// reset). A field cum doesn't report stays nil rather than becoming zero.
// The returned snapshot is the cumulative state to store for the next run;
// it is nil when nothing was reported.
func diffACPUsage(prev *UsageSnapshot, sessionID string, cum cumulativeUsage) (Usage, *UsageSnapshot) {
	if !cum.hasPrompt && !cum.hasUpdate {
		return Usage{Source: UsageSourceNotReported}, nil
	}

	var base UsageSnapshot
	if prev != nil && prev.SessionID == sessionID {
		base = *prev
	}
	if wentDown(base.Input, cum.Input) || wentDown(base.CachedInput, cum.CachedInput) ||
		wentDown(base.CacheWrite, cum.CacheWrite) || wentDown(base.Output, cum.Output) ||
		wentDown(base.Reasoning, cum.Reasoning) || wentDownF(base.CostUSD, cum.CostUSD) {
		base = UsageSnapshot{SessionID: sessionID}
	}

	u := Usage{
		Input:       delta(base.Input, cum.Input),
		CachedInput: delta(base.CachedInput, cum.CachedInput),
		CacheWrite:  delta(base.CacheWrite, cum.CacheWrite),
		Output:      delta(base.Output, cum.Output),
		Reasoning:   delta(base.Reasoning, cum.Reasoning),
		ContextUsed: cum.ContextUsed,
		ContextSize: cum.ContextSize,
		Source:      UsageSourceACPUsageUpdate,
	}
	if cum.CostUSD != nil {
		u.CostUSD = ptr(*cum.CostUSD - zeroF(base.CostUSD))
	}
	if cum.hasPrompt {
		u.Source = UsageSourceACPPromptUsage
	}

	// A counter missing this turn keeps its last known cumulative value, so
	// the next turn that reports it differences against that, not zero.
	snap := &UsageSnapshot{
		SessionID:   sessionID,
		Input:       latest(base.Input, cum.Input),
		CachedInput: latest(base.CachedInput, cum.CachedInput),
		CacheWrite:  latest(base.CacheWrite, cum.CacheWrite),
		Output:      latest(base.Output, cum.Output),
		Reasoning:   latest(base.Reasoning, cum.Reasoning),
		CostUSD:     base.CostUSD,
	}
	if cum.CostUSD != nil {
		snap.CostUSD = cum.CostUSD
	}
	u.SessionSnapshot = snap
	return u, snap
}

func wentDown(prev, cur *int64) bool { return prev != nil && cur != nil && *cur < *prev }

func wentDownF(prev, cur *float64) bool { return prev != nil && cur != nil && *cur < *prev }

func zeroF(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func delta(prev, cur *int64) *int64 {
	if cur == nil {
		return nil
	}
	var p int64
	if prev != nil {
		p = *prev
	}
	return ptr(*cur - p)
}

func latest(prev, cur *int64) *int64 {
	if cur != nil {
		return cur
	}
	return prev
}

// usageSnapshotFromHandle decodes the ACP usage snapshot kept in a session
// handle's Metadata, or nil if there is none.
func usageSnapshotFromHandle(h SessionHandle) *UsageSnapshot {
	if len(h.Metadata) == 0 {
		return nil
	}
	var s UsageSnapshot
	if json.Unmarshal(h.Metadata, &s) != nil {
		return nil
	}
	return &s
}
