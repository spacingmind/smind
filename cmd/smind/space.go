package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/store"
)

func cmdSpace(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind space <create|ls> ...")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdSpaceCreate(args[1:])
	case "ls":
		return cmdSpaceList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind space: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdSpaceCreate(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: smind space create <workspaceId> <title>")
		return 2
	}
	workspaceID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "space create: invalid workspaceId %q: %v\n", args[0], err)
		return 2
	}
	title := strings.Join(args[1:], " ")

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var sp store.Space
	err = client.Call(context.Background(), "space.create", map[string]any{
		"workspaceId": workspaceID, "title": title,
	}, &sp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "space create: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%d\n", sp.ID, sp.Title, sp.WorkspaceID)
	return 0
}

func cmdSpaceList(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind space ls <workspaceId>")
		return 2
	}
	workspaceID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "space ls: invalid workspaceId %q: %v\n", args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var spaces []store.Space
	err = client.Call(context.Background(), "space.list", map[string]any{"workspaceId": workspaceID}, &spaces)
	if err != nil {
		fmt.Fprintf(os.Stderr, "space ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tWORKSPACEID")
	for _, sp := range spaces {
		fmt.Fprintf(tw, "%d\t%s\t%d\n", sp.ID, sp.Title, sp.WorkspaceID)
	}
	tw.Flush()
	return 0
}
