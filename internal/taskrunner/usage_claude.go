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
