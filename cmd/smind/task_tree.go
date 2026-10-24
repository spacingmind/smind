package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
		return "error: " + rateLimitedText(run.Window, resetAt, run.ResetInferred, now)
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

// cmdTaskTreeUsage is printed on any argument error in cmdTaskTree.
const cmdTaskTreeUsage = "usage: smind task tree <workspaceId> [--json]\n       smind task tree --task <taskId> [--json]"

// cmdTaskTree prints a workspace's whole task hierarchy, or one task's
// subtree via --task -- the CLI mirror of the task.tree wsapi method.
func cmdTaskTree(args []string) int {
	var idArg, taskArg string
	var asJSON bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			asJSON = true
		case a == "--task":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskTreeUsage)
				return 2
			}
			i++
			taskArg = args[i]
		case strings.HasPrefix(a, "--task="):
			taskArg = strings.TrimPrefix(a, "--task=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "task tree: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskTreeUsage)
			return 2
		case idArg == "":
			idArg = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskTreeUsage)
			return 2
		}
	}
	if (idArg == "") == (taskArg == "") {
		fmt.Fprintln(os.Stderr, cmdTaskTreeUsage)
		return 2
	}
	key, raw := "workspaceId", idArg
	if taskArg != "" {
		key, raw = "taskId", taskArg
	}
	id, err := parseInt64(raw)
	if err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "task tree: invalid %s %q\n", key, raw)
		fmt.Fprintln(os.Stderr, cmdTaskTreeUsage)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{key: id}
	if asJSON {
		var raw json.RawMessage
		if err := client.Call(context.Background(), "task.tree", params, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "task tree: %v\n", err)
			return 1
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			fmt.Fprintf(os.Stderr, "task tree: %v\n", err)
			return 1
		}
		fmt.Println(buf.String())
		return 0
	}
	var flat taskTreeFlat
	if err := client.Call(context.Background(), "task.tree", params, &flat); err != nil {
		fmt.Fprintf(os.Stderr, "task tree: %v\n", err)
		return 1
	}
	renderTaskTree(os.Stdout, flat, time.Now())
	return 0
}
