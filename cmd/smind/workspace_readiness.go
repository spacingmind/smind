package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/readiness"
	"github.com/spacingmind/smind/internal/store"
)

const workspaceReadinessUsage = "usage: smind workspace readiness [workspaceId] [--json]"

type readinessReport struct {
	WorkspaceID int64               `json:"workspaceId"`
	Title       string              `json:"title"`
	Findings    []readiness.Finding `json:"findings"`
}

// cmdWorkspaceReadiness prints the static agent-readiness findings of one
// workspace, or of every workspace when no id is given. Findings never change
// the exit code; only an unknown workspace or a daemon error does.
func cmdWorkspaceReadiness(args []string) int {
	fs := flag.NewFlagSet("workspace readiness", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	// Accept the id before or after --json.
	var ids []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			fmt.Fprintln(os.Stderr, workspaceReadinessUsage)
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		ids = append(ids, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(ids) > 1 {
		fmt.Fprintln(os.Stderr, workspaceReadinessUsage)
		return 2
	}
	var id int64
	if len(ids) == 1 {
		var err error
		if id, err = strconv.ParseInt(ids[0], 10, 64); err != nil || id <= 0 {
			fmt.Fprintf(os.Stderr, "smind workspace readiness: invalid workspaceId %q\n%s\n", ids[0], workspaceReadinessUsage)
			return 2
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()
	return runWorkspaceReadiness(context.Background(), client, id, *asJSON, os.Stdout, os.Stderr)
}

type rpcCaller interface {
	Call(ctx context.Context, method string, params, out any) error
}

func runWorkspaceReadiness(ctx context.Context, c rpcCaller, id int64, asJSON bool, out, errw io.Writer) int {
	var targets []store.Workspace
	if id != 0 {
		var ws store.Workspace
		if err := c.Call(ctx, "workspace.get", map[string]any{"id": id}, &ws); err != nil {
			fmt.Fprintf(errw, "workspace readiness: %v\n", err)
			return 1
		}
		targets = []store.Workspace{ws}
	} else if err := c.Call(ctx, "workspace.list", nil, &targets); err != nil {
		fmt.Fprintf(errw, "workspace readiness: %v\n", err)
		return 1
	}
	reports := make([]readinessReport, 0, len(targets))
	for _, ws := range targets {
		var res struct {
			Findings []readiness.Finding `json:"findings"`
		}
		if err := c.Call(ctx, "workspace.readiness", map[string]any{"workspaceId": ws.ID}, &res); err != nil {
			fmt.Fprintf(errw, "workspace readiness: workspace %d: %v\n", ws.ID, err)
			return 1
		}
		reports = append(reports, readinessReport{WorkspaceID: ws.ID, Title: ws.Title, Findings: res.Findings})
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintf(errw, "workspace readiness: %v\n", err)
			return 1
		}
		return 0
	}
	for i, r := range reports {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "workspace %d: %s\n", r.WorkspaceID, r.Title)
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		for _, f := range r.Findings {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", f.Status, f.Title, f.Evidence)
			if f.Fix != "" {
				fmt.Fprintf(tw, "\t  fix:\t%s\n", f.Fix)
			}
		}
		tw.Flush()
	}
	return 0
}
