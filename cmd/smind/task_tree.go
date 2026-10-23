package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// treeTitleMax is the longest title (in runes) renderTaskTree prints.
const treeTitleMax = 48

// treeUsage is the slice of a task.tree node's usage rollup the CLI shows:
// the subtree's summed cost (nil = nothing reported).
type treeUsage struct {
	Subtree struct {
		CostUSD *float64 `json:"costUsd"`
	} `json:"subtree"`
}

// renderTaskTree prints the task.tree flat result as an indented tree: a
// count header, one line per node, and a note when the daemon truncated it.
func renderTaskTree(w io.Writer, flat taskTreeFlat, now time.Time) {
	fmt.Fprintln(w, treeHeader(flat.Nodes))
	minDepth := 0
	for i, n := range flat.Nodes {
		if i == 0 || n.Depth < minDepth {
			minDepth = n.Depth
		}
	}
	for _, n := range flat.Nodes {
		fmt.Fprintln(w, strings.Repeat("  ", n.Depth-minDepth)+treeLine(n, now))
	}
	if flat.Truncated {
		fmt.Fprintln(w, "(tree truncated by the daemon; narrow it with --task)")
	}
}

// treeHeader renders "<N> tasks: <R> running, <W> waiting, <F> failed, <D> done".
func treeHeader(nodes []taskTreeNode) string {
	if len(nodes) == 0 {
		return "0 tasks"
	}
	var running, waiting, failed, done int
	for _, n := range nodes {
		switch n.Run.State {
		case "running":
			running++
		case "waiting_permission":
			waiting++
		case "error":
			failed++
		case "done":
			done++
		}
	}
	noun := "tasks"
	if len(nodes) == 1 {
		noun = "task"
	}
	return fmt.Sprintf("%d %s: %d running, %d waiting, %d failed, %d done", len(nodes), noun, running, waiting, failed, done)
}

// treeLine renders one node without its indent.
func treeLine(n taskTreeNode, now time.Time) string {
	tier := "-"
	if n.Tier != nil && *n.Tier != "" {
		tier = *n.Tier
	}
	segs := []string{fmt.Sprintf("#%d %s", n.ID, treeTitle(n.Title)), tier, treeStateText(n.Run, now)}
	switch p := n.Run.PendingPermissions; {
	case p == 1:
		segs = append(segs, "1 permission")
	case p > 1:
		segs = append(segs, fmt.Sprintf("%d permissions", p))
	}
	return strings.Join(append(segs, treeCost(n)), "  ")
}

// treeTitle flattens newlines and truncates to treeTitleMax runes.
func treeTitle(title string) string {
	title = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(title)
	r := []rune(title)
	if len(r) <= treeTitleMax {
		return title
	}
	return string(r[:treeTitleMax-1]) + "…"
}

func treeStateText(run taskTreeNodeRun, now time.Time) string {
	switch {
	case run.State == "error" && run.FailureKind == "rate_limited":
		var resetAt *time.Time
		if t, err := time.Parse(time.RFC3339, run.ResetAt); err == nil {
			resetAt = &t
		}
		return "error: " + rateLimitedText(run.Window, resetAt, now)
	case run.State == "error" && run.FailureKind != "":
		return "error: " + run.FailureKind
	case run.State == "done" && run.Disposition == "needs-continue":
		return "done (needs-continue)"
	}
	return run.State
}

func treeCost(n taskTreeNode) string {
	var u treeUsage
	if len(n.Usage) > 0 {
		_ = json.Unmarshal(n.Usage, &u)
	}
	if u.Subtree.CostUSD == nil {
		return "cost n/a"
	}
	return fmt.Sprintf("$%.2f", *u.Subtree.CostUSD)
}
