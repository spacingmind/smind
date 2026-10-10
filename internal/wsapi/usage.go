package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// usageLogEntry is the wire shape of one usage.list row -- store.RequestLog
// field-for-field, but camelCase and with StartedAt rendered as RFC3339
// (matching accountResult's own timestamp convention). Already
// credential- and content-free at the store layer (see RequestLog's doc
// comment); nothing further to redact here.
type usageLogEntry struct {
	ID               int64   `json:"id"`
	StartedAt        string  `json:"startedAt"`
	Provider         string  `json:"provider"`
	AccountID        *int64  `json:"accountId,omitempty"`
	SessionKey       string  `json:"sessionKey"`
	Model            *string `json:"model,omitempty"`
	Stream           *bool   `json:"stream,omitempty"`
	Status           int     `json:"status"`
	UpstreamStatus   *int    `json:"upstreamStatus,omitempty"`
	Outcome          string  `json:"outcome"`
	Error            *string `json:"error,omitempty"`
	TTFBMs           *int64  `json:"ttfbMs,omitempty"`
	DurationMs       int64   `json:"durationMs"`
	InputTokens      *int64  `json:"inputTokens,omitempty"`
	OutputTokens     *int64  `json:"outputTokens,omitempty"`
	CacheReadTokens  *int64  `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int64  `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  *int64  `json:"reasoningTokens,omitempty"`
}

func usageLogEntryFrom(r store.RequestLog) usageLogEntry {
	return usageLogEntry{
		ID: r.ID, StartedAt: r.StartedAt.Format(time.RFC3339), Provider: r.Provider,
		AccountID: r.AccountID, SessionKey: r.SessionKey, Model: r.Model, Stream: r.Stream,
		Status: r.Status, UpstreamStatus: r.UpstreamStatus, Outcome: r.Outcome, Error: r.Error,
		TTFBMs: r.TTFBMs, DurationMs: r.DurationMs,
		InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		CacheReadTokens: r.CacheReadTokens, CacheWriteTokens: r.CacheWriteTokens,
		ReasoningTokens: r.ReasoningTokens,
	}
}

// handleUsageList returns request_log rows -- most recent first --
// optionally narrowed by since/until (since inclusive, until exclusive)
// and accountId, and capped to limit rows when limit > 0.
func handleUsageList(db *store.Store) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		if db == nil {
			return nil, fmt.Errorf("usage.list: store is unavailable")
		}
		var p struct {
			Since     string `json:"since"`
			Until     string `json:"until"`
			AccountID int64  `json:"accountId"`
			Limit     int    `json:"limit"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("usage.list: invalid params: %w", err)
			}
		}

		filter := store.RequestLogFilter{Limit: p.Limit}
		since, err := parseUsageTime("usage.list", "since", p.Since)
		if err != nil {
			return nil, err
		}
		filter.Since = since
		until, err := parseUsageTime("usage.list", "until", p.Until)
		if err != nil {
			return nil, err
		}
		filter.Until = until
		if p.AccountID != 0 {
			filter.AccountID = &p.AccountID
		}

		rows, err := db.ListRequestLogs(filter)
		if err != nil {
			return nil, fmt.Errorf("usage.list: %w", err)
		}
		result := make([]usageLogEntry, len(rows))
		for i, r := range rows {
			result[i] = usageLogEntryFrom(r)
		}
		return result, nil
	}
}

// usageSummaryEntry is the wire shape of one usage.summary row --
// store.UsageSummary (scope "proxy") or store.RunUsageSummary (scope
// "runs") field-for-field, camelCase. CostUSD is only carried by runs rows
// (the proxy meter never computes cost).
type usageSummaryEntry struct {
	Scope            string   `json:"scope"`
	Key              string   `json:"key"`
	Count            int64    `json:"count"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	CacheReadTokens  int64    `json:"cacheReadTokens"`
	CacheWriteTokens int64    `json:"cacheWriteTokens"`
	ReasoningTokens  int64    `json:"reasoningTokens"`
	CostUSD          *float64 `json:"costUsd,omitempty"`
}

func usageSummaryEntryFrom(u store.UsageSummary) usageSummaryEntry {
	return usageSummaryEntry{
		Scope: usageScopeProxy,
		Key:   u.Key, Count: u.Count, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
		ReasoningTokens: u.ReasoningTokens,
	}
}

func runUsageSummaryEntryFrom(u store.RunUsageSummary) usageSummaryEntry {
	return usageSummaryEntry{
		Scope: usageScopeRuns,
		Key:   u.Key, Count: u.Count, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
		ReasoningTokens: u.ReasoningTokens, CostUSD: u.CostUSD,
	}
}

// usage.summary scopes (ADR-0020): the proxy meter (request_log), per-run
// usage reported by agents (run_usage), or both.
const (
	usageScopeProxy = "proxy"
	usageScopeRuns  = "runs"
	usageScopeAll   = "all"
)

// proxyGroupBys are the groupBy values the proxy meter accepts.
var proxyGroupBys = map[string]bool{"account": true, "model": true, "day": true}

// handleUsageSummary aggregates usage matching since/until (since
// inclusive, until exclusive) into one row per distinct value of groupBy.
// scope picks the family: "proxy" (groupBy account|model|day), "runs"
// (groupBy workspace|task|chat|run|provider|model), or "all" (the default),
// which concatenates proxy rows then run rows for whichever families accept
// groupBy. The result is always an array, each row tagged with its scope.
func handleUsageSummary(db *store.Store) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		if db == nil {
			return nil, fmt.Errorf("usage.summary: store is unavailable")
		}
		var p struct {
			Since   string `json:"since"`
			Until   string `json:"until"`
			GroupBy string `json:"groupBy"`
			Scope   string `json:"scope"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("usage.summary: invalid params: %w", err)
		}
		if p.GroupBy == "" {
			return nil, fmt.Errorf("usage.summary: groupBy is required")
		}
		if p.Scope == "" {
			p.Scope = usageScopeAll
		}
		switch p.Scope {
		case usageScopeProxy, usageScopeRuns, usageScopeAll:
		default:
			return nil, fmt.Errorf("usage.summary: invalid scope %q (want proxy, runs, or all)", p.Scope)
		}

		wantProxy := p.Scope != usageScopeRuns && proxyGroupBys[p.GroupBy]
		wantRuns := p.Scope != usageScopeProxy && store.IsRunUsageGroupBy(p.GroupBy)
		if !wantProxy && !wantRuns {
			return nil, fmt.Errorf("usage.summary: invalid groupBy %q for scope %q", p.GroupBy, p.Scope)
		}

		since, err := parseUsageTime("usage.summary", "since", p.Since)
		if err != nil {
			return nil, err
		}
		until, err := parseUsageTime("usage.summary", "until", p.Until)
		if err != nil {
			return nil, err
		}

		result := []usageSummaryEntry{}
		if wantProxy {
			rows, err := db.SummarizeRequestLogs(since, until, p.GroupBy)
			if err != nil {
				return nil, fmt.Errorf("usage.summary: %w", err)
			}
			for _, r := range rows {
				result = append(result, usageSummaryEntryFrom(r))
			}
		}
		if wantRuns {
			rows, err := db.SummarizeRunUsage(since, until, p.GroupBy)
			if err != nil {
				return nil, fmt.Errorf("usage.summary: %w", err)
			}
			for _, r := range rows {
				result = append(result, runUsageSummaryEntryFrom(r))
			}
		}
		return result, nil
	}
}

// parseUsageTime parses an optional RFC3339 timestamp param (empty ->
// nil, an open bound), naming method/field in any error so usage.list and
// usage.summary's since/until both report which one failed.
func parseUsageTime(method, field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid %s: %w", method, field, err)
	}
	return &t, nil
}
