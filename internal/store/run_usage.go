package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UpsertRunUsage writes u's row, replacing any earlier row for the same
// run (one row per run; a later turn or the run's finish overwrites it).
// WorkspaceID is always resolved from the run's task, ignoring whatever u
// carries, and UpdatedAt is stamped here.
func (s *Store) UpsertRunUsage(u RunUsage) error {
	var workspaceID sql.NullInt64
	err := s.db.QueryRow(`SELECT workspace_id FROM tasks WHERE id = ?`, u.TaskID).Scan(&workspaceID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("upsert run usage %q: lookup task: %w", u.RunID, err)
	}
	_, err = s.db.Exec(
		`INSERT INTO run_usage (
			run_id, workspace_id, task_id, chat_id, provider, model,
			input_tokens, cached_input_tokens, cache_write_tokens, output_tokens, reasoning_tokens,
			cost_usd, context_used, context_size, session_snapshot, source, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET
			workspace_id = excluded.workspace_id, task_id = excluded.task_id, chat_id = excluded.chat_id,
			provider = excluded.provider, model = excluded.model,
			input_tokens = excluded.input_tokens, cached_input_tokens = excluded.cached_input_tokens,
			cache_write_tokens = excluded.cache_write_tokens, output_tokens = excluded.output_tokens,
			reasoning_tokens = excluded.reasoning_tokens, cost_usd = excluded.cost_usd,
			context_used = excluded.context_used, context_size = excluded.context_size,
			session_snapshot = excluded.session_snapshot, source = excluded.source,
			updated_at = excluded.updated_at`,
		u.RunID, workspaceID, u.TaskID, u.ChatID, u.Provider, u.Model,
		u.InputTokens, u.CachedInputTokens, u.CacheWriteTokens, u.OutputTokens, u.ReasoningTokens,
		u.CostUSD, u.ContextUsed, u.ContextSize, u.SessionSnapshot, u.Source, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert run usage %q: %w", u.RunID, err)
	}
	return nil
}

const runUsageColumns = `run_id, workspace_id, task_id, chat_id, provider, model,
	input_tokens, cached_input_tokens, cache_write_tokens, output_tokens, reasoning_tokens,
	cost_usd, context_used, context_size, session_snapshot, source, updated_at`

func scanRunUsage(sc interface{ Scan(...any) error }) (RunUsage, error) {
	var u RunUsage
	err := sc.Scan(&u.RunID, &u.WorkspaceID, &u.TaskID, &u.ChatID, &u.Provider, &u.Model,
		&u.InputTokens, &u.CachedInputTokens, &u.CacheWriteTokens, &u.OutputTokens, &u.ReasoningTokens,
		&u.CostUSD, &u.ContextUsed, &u.ContextSize, &u.SessionSnapshot, &u.Source, &u.UpdatedAt)
	return u, err
}

// GetRunUsage returns runID's usage row, or sql.ErrNoRows (wrapped) when the
// run never reported any.
func (s *Store) GetRunUsage(runID string) (RunUsage, error) {
	u, err := scanRunUsage(s.db.QueryRow(`SELECT `+runUsageColumns+` FROM run_usage WHERE run_id = ?`, runID))
	if err != nil {
		return RunUsage{}, fmt.Errorf("get run usage %q: %w", runID, err)
	}
	return u, nil
}

// ListRunUsage returns the usage rows of runIDs keyed by run id, in one
// query per batch; runs without a row are simply absent.
func (s *Store) ListRunUsage(runIDs []string) (map[string]RunUsage, error) {
	out := make(map[string]RunUsage, len(runIDs))
	// Stay well under SQLite's bound-variable limit.
	const batch = 500
	for start := 0; start < len(runIDs); start += batch {
		end := min(start+batch, len(runIDs))
		ids := runIDs[start:end]
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		rows, err := s.db.Query(`SELECT `+runUsageColumns+` FROM run_usage WHERE run_id IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("list run usage: %w", err)
		}
		for rows.Next() {
			u, err := scanRunUsage(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan run usage: %w", err)
			}
			out[u.RunID] = u
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("list run usage: %w", err)
		}
	}
	return out, nil
}

// runUsageGroupColumns maps usage.summary's runs-scope groupBy values to
// the SQL expression producing the group key (over run_usage u).
var runUsageGroupColumns = map[string]string{
	"workspace": "COALESCE(CAST(u.workspace_id AS TEXT), '')",
	"task":      "CAST(u.task_id AS TEXT)",
	"chat":      "COALESCE(CAST(u.chat_id AS TEXT), '')",
	"run":       "u.run_id",
	"provider":  "u.provider",
	"model":     "COALESCE(u.model, '')",
}

// IsRunUsageGroupBy reports whether groupBy is a valid runs-scope grouping.
func IsRunUsageGroupBy(groupBy string) bool {
	_, ok := runUsageGroupColumns[groupBy]
	return ok
}

// SummarizeRunUsage aggregates run_usage rows into one RunUsageSummary per
// distinct value of groupBy ("workspace", "task", "chat", "run", "provider",
// or "model"). Runs are filtered on runs.started_at, Since inclusive and
// Until exclusive like SummarizeRequestLogs. Sums skip NULLs; Count is the
// number of runs with a usage row.
func (s *Store) SummarizeRunUsage(since, until *time.Time, groupBy string) ([]RunUsageSummary, error) {
	col, ok := runUsageGroupColumns[groupBy]
	if !ok {
		return nil, fmt.Errorf("summarize run usage: invalid groupBy %q", groupBy)
	}

	var conds []string
	var args []any
	if since != nil {
		conds = append(conds, "r.started_at >= ?")
		args = append(args, *since)
	}
	if until != nil {
		conds = append(conds, "r.started_at < ?")
		args = append(args, *until)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	query := fmt.Sprintf(`SELECT %s AS grp, COUNT(*),
		COALESCE(SUM(u.input_tokens), 0), COALESCE(SUM(u.output_tokens), 0),
		COALESCE(SUM(u.cached_input_tokens), 0), COALESCE(SUM(u.cache_write_tokens), 0),
		COALESCE(SUM(u.reasoning_tokens), 0), COALESCE(SUM(u.cost_usd), 0)
		FROM run_usage u JOIN runs r ON r.id = u.run_id%s GROUP BY grp ORDER BY grp`, col, where)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("summarize run usage: %w", err)
	}
	defer rows.Close()

	var out []RunUsageSummary
	for rows.Next() {
		var u RunUsageSummary
		if err := rows.Scan(&u.Key, &u.Count, &u.InputTokens, &u.OutputTokens,
			&u.CacheReadTokens, &u.CacheWriteTokens, &u.ReasoningTokens, &u.CostUSD); err != nil {
			return nil, fmt.Errorf("scan run usage summary: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
