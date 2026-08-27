package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/store"
)

func cmdWorkspace(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind workspace <create|ls> ...")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdWorkspaceCreate(args[1:])
	case "ls":
		return cmdWorkspaceList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind workspace: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdWorkspaceCreate(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: smind workspace create <repoPath> <name> <policy>")
		return 2
	}
	repoPath, name, policy := args[0], args[1], args[2]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var ws store.Workspace
	err = client.Call(context.Background(), "workspace.create", map[string]any{
		"path": repoPath, "title": name, "routingPolicy": policy, "accountIds": []int64{},
	}, &ws)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace create: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\n", ws.ID, ws.Title, ws.Path, ws.RoutingPolicy)
	return 0
}

func cmdWorkspaceList(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind workspace ls")
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var workspaces []store.Workspace
	if err := client.Call(context.Background(), "workspace.list", nil, &workspaces); err != nil {
		fmt.Fprintf(os.Stderr, "workspace ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tPATH\tPOLICY")
	for _, ws := range workspaces {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", ws.ID, ws.Title, ws.Path, ws.RoutingPolicy)
	}
	tw.Flush()
	return 0
}
