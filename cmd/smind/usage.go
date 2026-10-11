package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
)

const cmdUsageUsage = "usage: smind usage [--since <RFC3339>] [--until <RFC3339>] [--scope proxy|runs|all] " +
	"[--by account|model|day|workspace|task|chat|run|provider]"

// usageGroupBys are the --by values: account|model|day group proxy traffic,
// workspace|task|chat|run|provider group runs, and model groups both.
var usageGroupBys = map[string]bool{
	"account": true, "model": true, "day": true,
	"workspace": true, "task": true, "chat": true, "run": true, "provider": true,
}

// usageSummaryRow is the wire shape of one usage.summary result row (see
// internal/wsapi's usageSummaryEntry).
type usageSummaryRow struct {
	Scope            string   `json:"scope"`
	CostUSD          *float64 `json:"costUsd"`
	Key              string   `json:"key"`
	Count            int64    `json:"count"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	CacheReadTokens  int64    `json:"cacheReadTokens"`
	CacheWriteTokens int64    `json:"cacheWriteTokens"`
	ReasoningTokens  int64    `json:"reasoningTokens"`
}

// cmdUsage implements `smind usage`: one aggregated row per group from
// usage.summary, printed as a table. Proxy rows carry tokens only; run rows
// also carry the cost the agent itself reported (ADR-0020), never computed.
func cmdUsage(args []string) int {
	params := map[string]any{"groupBy": "account"}
	byGiven := false
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			fmt.Fprintf(os.Stderr, "usage: flag %q needs a value\n%s\n", args[i], cmdUsageUsage)
			return 2
		}
		flag, value := args[i], args[i+1]
		i++
		switch flag {
		case "--since":
			params["since"] = value
		case "--until":
			params["until"] = value
		case "--by":
			if !usageGroupBys[value] {
				fmt.Fprintf(os.Stderr, "smind usage: --by must be account, model, day, workspace, task, chat, run, or provider\n%s\n", cmdUsageUsage)
				return 2
			}
			params["groupBy"] = value
			byGiven = true
		case "--scope":
			if value != "proxy" && value != "runs" && value != "all" {
				fmt.Fprintf(os.Stderr, "smind usage: --scope must be proxy, runs, or all\n%s\n", cmdUsageUsage)
				return 2
			}
			params["scope"] = value
		default:
			fmt.Fprintf(os.Stderr, "smind usage: unknown flag %q\n%s\n", flag, cmdUsageUsage)
			return 2
		}
	}

	// --scope runs has no account/day grouping, so its default is per task.
	if !byGiven && params["scope"] == "runs" {
		params["groupBy"] = "task"
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var rows []usageSummaryRow
	if err := client.Call(context.Background(), "usage.summary", params, &rows); err != nil {
		fmt.Fprintf(os.Stderr, "smind usage: %v\n", err)
		return 1
	}

	if len(rows) == 0 {
		fmt.Println("no usage recorded")
		return 0
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SCOPE\tKEY\tCOUNT\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tREASONING\tCOST USD")
	for _, r := range rows {
		cost := "-"
		if r.CostUSD != nil {
			cost = strconv.FormatFloat(*r.CostUSD, 'f', -1, 64)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			r.Scope, r.Key, r.Count, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens, cost)
	}
	tw.Flush()
	return 0
}
