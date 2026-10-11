package store

import (
	"database/sql"
	"fmt"
)

// TaskUsageSums is one task's summed run_usage over all its runs that
// reported usage, for the task.tree read model. Every numeric field
// except Runs is nil when no run reported it -- nil never means 0
// (ADR-0020); a reported 0 sums to a real 0.
type TaskUsageSums struct {
	Runs             int64
	InputTokens      *int64
	OutputTokens     *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	ReasoningTokens  *int64
	CostUSD          *float64
}

// SumRunUsageByTask sums run_usage per task for every task of
// workspaceID in ONE query (run_usage joined to runs to tasks), so
// task.tree pays a single store pass instead of one query per node.
// Per-field COUNT(col) tracking keeps "nothing reported" (nil)
// distinguishable from a reported 0: a field is nil exactly when no row
// for the task had a non-NULL value.
func (s *Store) SumRunUsageByTask(workspaceID int64) (map[int64]TaskUsageSums, error) {
	rows, err := s.db.Query(`
		SELECT u.task_id, COUNT(*),
			SUM(u.input_tokens), COUNT(u.input_tokens),
			SUM(u.output_tokens), COUNT(u.output_tokens),
			SUM(u.cached_input_tokens), COUNT(u.cached_input_tokens),
			SUM(u.cache_write_tokens), COUNT(u.cache_write_tokens),
			SUM(u.reasoning_tokens), COUNT(u.reasoning_tokens),
			SUM(u.cost_usd)
		FROM run_usage u
		JOIN runs r ON r.id = u.run_id
		JOIN tasks t ON t.id = u.task_id
		WHERE t.workspace_id = ?
		GROUP BY u.task_id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("sum run usage by task for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	out := make(map[int64]TaskUsageSums)
	for rows.Next() {
		var (
			taskID                             int64
			runs                               int64
			in, outTok, cacheR, cacheW, reas   sql.NullInt64
			inN, outN, cacheRN, cacheWN, reasN int64
			cost                               sql.NullFloat64
		)
		if err := rows.Scan(&taskID, &runs,
			&in, &inN, &outTok, &outN, &cacheR, &cacheRN, &cacheW, &cacheWN, &reas, &reasN, &cost); err != nil {
			return nil, fmt.Errorf("scan run usage sum for task: %w", err)
		}
		sum := TaskUsageSums{Runs: runs}
		if inN > 0 {
			sum.InputTokens = &in.Int64
		}
		if outN > 0 {
			sum.OutputTokens = &outTok.Int64
		}
		if cacheRN > 0 {
			sum.CacheReadTokens = &cacheR.Int64
		}
		if cacheWN > 0 {
			sum.CacheWriteTokens = &cacheW.Int64
		}
		if reasN > 0 {
			sum.ReasoningTokens = &reas.Int64
		}
		if cost.Valid {
			sum.CostUSD = &cost.Float64
		}
		out[taskID] = sum
	}
	return out, rows.Err()
}
