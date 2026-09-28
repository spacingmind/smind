package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
)

const cmdUsageUsage = "usage: smind usage [--since <RFC3339>] [--until <RFC3339>] [--by account|model|day]"

// usageSummaryRow is the wire shape of one usage.summary result row (see
// internal/wsapi's usageSummaryEntry).
type usageSummaryRow struct {
	Key              string `json:"key"`
	Count            int64  `json:"count"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	CacheReadTokens  int64  `json:"cacheReadTokens"`
	CacheWriteTokens int64  `json:"cacheWriteTokens"`
	ReasoningTokens  int64  `json:"reasoningTokens"`
}

// cmdUsage implements `smind usage`: one aggregated row per group from
// usage.summary, printed as a table. Tokens only -- no cost/price
// anywhere (plan decision "No cost/price computation").
func cmdUsage(args []string) int {
	params := map[string]any{"groupBy": "account"}
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
			if value != "account" && value != "model" && value != "day" {
				fmt.Fprintf(os.Stderr, "smind usage: --by must be account, model, or day\n%s\n", cmdUsageUsage)
				return 2
			}
			params["groupBy"] = value
		default:
			fmt.Fprintf(os.Stderr, "smind usage: unknown flag %q\n%s\n", flag, cmdUsageUsage)
			return 2
		}
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
	fmt.Fprintln(tw, "KEY\tREQUESTS\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tREASONING")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			r.Key, r.Count, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens)
	}
	tw.Flush()
	return 0
}
