package taskrunner

import (
	"sort"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func ptr[T any](v T) *T { return &v }

// claudeUsage normalizes a claude-native turn's result message into a Usage
// (ADR-0020). The SDK's ResultMessage has no top-level usage and a
// non-pointer TotalCostUSD, so tokens come from ModelUsage summed across
// models, and a zero cost is read as "not reported". Nothing is estimated.
func claudeUsage(res claudecode.ResultMessage) Usage {
	if len(res.ModelUsage) == 0 {
		if res.TotalCostUSD > 0 {
			return Usage{CostUSD: ptr(res.TotalCostUSD), Source: UsageSourceClaudeResult}
		}
		return Usage{Source: UsageSourceNotReported}
	}

	var in, cacheRead, cacheWrite, out int64
	var modelCost float64
	names := make([]string, 0, len(res.ModelUsage))
	for name, mu := range res.ModelUsage {
		names = append(names, name)
		in += int64(mu.InputTokens)
		cacheRead += int64(mu.CacheReadInputTokens)
		cacheWrite += int64(mu.CacheCreationInputTokens)
		out += int64(mu.OutputTokens)
		modelCost += mu.CostUSD
	}
	sort.Strings(names)

	// The dominant model is the one with the most tokens; names is sorted,
	// so a tie goes to the first name.
	total := func(mu claudecode.ModelUsage) int {
		return mu.InputTokens + mu.OutputTokens + mu.CacheReadInputTokens + mu.CacheCreationInputTokens
	}
	top := names[0]
	for _, name := range names[1:] {
		if total(res.ModelUsage[name]) > total(res.ModelUsage[top]) {
			top = name
		}
	}

	u := Usage{
		Input:       ptr(in),
		CachedInput: ptr(cacheRead),
		CacheWrite:  ptr(cacheWrite),
		Output:      ptr(out),
		Model:       ptr(top),
		Source:      UsageSourceClaudeResult,
	}
	switch {
	case res.TotalCostUSD > 0:
		u.CostUSD = ptr(res.TotalCostUSD)
	case modelCost > 0:
		u.CostUSD = ptr(modelCost)
	}
	if w := res.ModelUsage[top].ContextWindow; w > 0 {
		u.ContextSize = ptr(int64(w))
	}
	return u
}

// ClaudeUsageTotals is a claude-native session's cumulative usage as the CLI
// last reported it: per-model token counts and cost, and the total cost.
type ClaudeUsageTotals struct {
	Models  map[string]claudecode.ModelUsage `json:"models,omitempty"`
	CostUSD float64                          `json:"costUsd,omitempty"`
}

// claudeTotalsOf is the cumulative usage res reports.
func claudeTotalsOf(res claudecode.ResultMessage) *ClaudeUsageTotals {
	models := make(map[string]claudecode.ModelUsage, len(res.ModelUsage))
	for name, mu := range res.ModelUsage {
		models[name] = mu
	}
	return &ClaudeUsageTotals{Models: models, CostUSD: res.TotalCostUSD}
}

// claudeTurnUsage is claudeUsage for one turn of a resumed session. The CLI
// restores a resumed session's running totals, so a result's modelUsage and
// total_cost_usd are cumulative across every turn of the chat: reading them as
// this turn's usage made a one-word reply report the whole conversation's
// tokens and cost, and a task's total grow quadratically with its turns
// (observed: output tokens 1.1k, 1.4k, 1.6k ... 16.7k over a dozen short
// turns). The turn's own usage is the result minus the totals stored after the
// previous turn; with no baseline (a first turn, or a session stored before
// totals were kept) the result is taken as is, and a result smaller than the
// baseline (a fresh session after a failed resume) is a reset, taken as is
// too.
func claudeTurnUsage(res claudecode.ResultMessage, prev *ClaudeUsageTotals) Usage {
	return claudeUsage(subtractClaudeTotals(res, prev))
}

func subtractClaudeTotals(res claudecode.ResultMessage, prev *ClaudeUsageTotals) claudecode.ResultMessage {
	if prev == nil || res.TotalCostUSD < prev.CostUSD {
		return res
	}
	models := make(map[string]claudecode.ModelUsage, len(res.ModelUsage))
	for name, mu := range res.ModelUsage {
		old, ok := prev.Models[name]
		if !ok {
			models[name] = mu
			continue
		}
		if mu.InputTokens < old.InputTokens || mu.OutputTokens < old.OutputTokens ||
			mu.CacheReadInputTokens < old.CacheReadInputTokens || mu.CacheCreationInputTokens < old.CacheCreationInputTokens {
			// A counter went backwards: the totals were reset, not extended.
			return res
		}
		mu.InputTokens -= old.InputTokens
		mu.OutputTokens -= old.OutputTokens
		mu.CacheReadInputTokens -= old.CacheReadInputTokens
		mu.CacheCreationInputTokens -= old.CacheCreationInputTokens
		mu.WebSearchRequests -= old.WebSearchRequests
		if d := mu.CostUSD - old.CostUSD; d > 0 {
			mu.CostUSD = d
		} else {
			mu.CostUSD = 0
		}
		models[name] = mu
	}
	out := res
	out.ModelUsage = models
	out.TotalCostUSD = res.TotalCostUSD - prev.CostUSD
	return out
}
