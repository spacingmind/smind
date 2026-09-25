package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/store"
)

const cmdProfileAddUsage = "usage: smind profile add <name> <provider> [--approval-policy=<policy>] [--thinking-level=<level>] [--notes=<text>]"

func cmdProfile(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind profile <add|ls|rm> ...")
		return 2
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:])
	case "ls":
		return cmdProfileList(args[1:])
	case "rm":
		return cmdProfileRemove(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind profile: unknown subcommand %q\n", args[0])
		return 2
	}
}

// cmdProfileAdd parses `smind profile add <name> <provider> [flags]`. Flags
// are each a single `--flag value` pair (matching `task new`'s `--space
// <id>` convention), scanned in any order after the two required
// positional args -- no model flag (ADR-0014's model deferral, dropped
// from v1 entirely).
func cmdProfileAdd(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdProfileAddUsage)
		return 2
	}
	name, provider := args[0], args[1]

	var approvalPolicy, thinkingLevel, notes string
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		if i+1 >= len(rest) {
			fmt.Fprintf(os.Stderr, "profile add: flag %q needs a value\n", rest[i])
			return 2
		}
		flag, value := rest[i], rest[i+1]
		i++
		switch flag {
		case "--approval-policy":
			approvalPolicy = value
		case "--thinking-level":
			thinkingLevel = value
		case "--notes":
			notes = value
		default:
			fmt.Fprintf(os.Stderr, "profile add: unknown flag %q\n", flag)
			return 2
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var p store.AgentProfile
	err = client.Call(context.Background(), "profile.create", map[string]any{
		"name": name, "provider": provider,
		"approvalPolicy": approvalPolicy, "thinkingLevel": thinkingLevel, "notes": notes,
	}, &p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile add: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, p.ApprovalPolicy, p.ThinkingLevel)
	return 0
}

func cmdProfileList(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind profile ls")
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var profiles []store.AgentProfile
	if err := client.Call(context.Background(), "profile.list", nil, &profiles); err != nil {
		fmt.Fprintf(os.Stderr, "profile ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tAPPROVAL\tTHINKING")
	for _, p := range profiles {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, p.ApprovalPolicy, p.ThinkingLevel)
	}
	tw.Flush()
	return 0
}

func cmdProfileRemove(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind profile rm <id>")
		return 2
	}
	id, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile rm: invalid id %q: %v\n", args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	if err := client.Call(context.Background(), "profile.delete", map[string]any{"id": id}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "profile rm: %v\n", err)
		return 1
	}
	return 0
}
