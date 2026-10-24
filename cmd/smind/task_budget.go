package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// cmdTaskBudgetUsage is printed on any argument error in cmdTaskBudget.
const cmdTaskBudgetUsage = "usage: smind task budget <taskId> [<usd> | --clear]"

// taskBudgetView is the part of the task payload `task budget` reads
// (task.get / task.setBudget; ADR-0041 A5).
type taskBudgetView struct {
	ID                int64    `json:"ID"`
	ParentTaskID      *int64   `json:"ParentTaskID"`
	BudgetUSD         *float64 `json:"budgetUsd"`
	BudgetSpentUSD    *float64 `json:"budgetSpentUsd"`
	BudgetPartial     bool     `json:"budgetPartial"`
	BudgetUnknownRuns int      `json:"budgetUnknownRuns"`
	OverBudget        bool     `json:"overBudget"`
}

// formatBudgetLine renders a task's tree budget:
//
//	budget $5.00 · spent $4.20+ · partial: 3 runs reported no cost · remaining $0.80
//
// "+" and the partial note appear only when some finished runs reported no
// cost (their cost is never estimated, ADR-0020, so the spend is a floor);
// "over budget" replaces "remaining" once the reported spend reached the cap.
func formatBudgetLine(v taskBudgetView) string {
	if v.BudgetUSD == nil {
		if v.ParentTaskID != nil {
			return "no budget (a budget lives on the tree's root task, not on a sub-task)"
		}
		return "no budget"
	}
	spent := 0.0
	if v.BudgetSpentUSD != nil {
		spent = *v.BudgetSpentUSD
	}
	plus := ""
	if v.BudgetPartial {
		plus = "+"
	}
	parts := []string{
		fmt.Sprintf("budget $%.2f", *v.BudgetUSD),
		fmt.Sprintf("spent $%.2f%s", spent, plus),
	}
	if v.BudgetPartial {
		runs := "runs"
		if v.BudgetUnknownRuns == 1 {
			runs = "run"
		}
		parts = append(parts, fmt.Sprintf("partial: %d %s reported no cost", v.BudgetUnknownRuns, runs))
	}
	if v.OverBudget {
		parts = append(parts, "over budget")
	} else {
		parts = append(parts, fmt.Sprintf("remaining $%.2f", *v.BudgetUSD-spent))
	}
	return strings.Join(parts, " · ")
}

// cmdTaskBudget shows, sets or clears the tree budget of a root task. Setting
// is humans only: the daemon refuses an agent's connection, and the MCP
// surface has no tool for it.
func cmdTaskBudget(args []string) int {
	var idArg, usdArg string
	clear := false
	for _, a := range args {
		switch {
		case a == "--clear":
			clear = true
		case len(a) > 1 && a[0] == '-' && (a[1] < '0' || a[1] > '9') && a[1] != '.':
			fmt.Fprintf(os.Stderr, "task budget: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskBudgetUsage)
			return 2
		case idArg == "":
			idArg = a
		case usdArg == "":
			usdArg = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskBudgetUsage)
			return 2
		}
	}
	if idArg == "" || (clear && usdArg != "") {
		fmt.Fprintln(os.Stderr, cmdTaskBudgetUsage)
		return 2
	}
	id, err := parseInt64(idArg)
	if err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "task budget: invalid taskId %q\n", idArg)
		fmt.Fprintln(os.Stderr, cmdTaskBudgetUsage)
		return 2
	}
	var usd float64
	if usdArg != "" {
		usd, err = strconv.ParseFloat(usdArg, 64)
		if err != nil || math.IsNaN(usd) || math.IsInf(usd, 0) || usd <= 0 {
			fmt.Fprintf(os.Stderr, "task budget: invalid amount %q: must be a positive number of USD\n", usdArg)
			return 2
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var view taskBudgetView
	switch {
	case clear:
		err = client.Call(context.Background(), "task.setBudget", map[string]any{"taskId": id, "budgetUsd": nil}, &view)
	case usdArg != "":
		err = client.Call(context.Background(), "task.setBudget", map[string]any{"taskId": id, "budgetUsd": usd}, &view)
	default:
		err = client.Call(context.Background(), "task.get", map[string]any{"id": id}, &view)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "task budget: %v\n", err)
		return 1
	}
	fmt.Println(formatBudgetLine(view))
	return 0
}
