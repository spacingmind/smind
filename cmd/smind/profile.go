package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/store"
)

const cmdProfileAddUsage = "usage: smind profile add <name> <provider> [--mode <permissionMode>] [--auto-accept] [--thinking-level=<level>] [--notes=<text>]"

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

// cmdProfileAdd parses `smind profile add <name> <provider> [flags]`.
// Value-taking flags accept both `--flag value` (matching `task new`'s
// `--space <id>` convention) and `--flag=value`; the boolean
// --auto-accept accepts `--auto-accept`, `--auto-accept=true`, and
// `--auto-accept=false` -- no model flag (ADR-0014's model deferral,
// dropped from v1 entirely).
func cmdProfileAdd(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdProfileAddUsage)
		return 2
	}
	name, provider := args[0], args[1]

	var mode, thinkingLevel, notes string
	var autoAccept bool
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		flag := rest[i]
		value := ""
		hasValue := false
		if n, v, ok := strings.Cut(flag, "="); ok && strings.HasPrefix(n, "--") {
			flag, value, hasValue = n, v, true
		}
		switch flag {
		case "--auto-accept":
			autoAccept = true
			if hasValue {
				b, err := strconv.ParseBool(value)
				if err != nil {
					fmt.Fprintf(os.Stderr, "profile add: --auto-accept: %q is not a boolean\n", value)
					return 2
				}
				autoAccept = b
			}
			continue
		case "--approval-policy":
			fmt.Fprintf(os.Stderr, "profile add: %s\n", approvalPolicyRemovedMsg)
			return 2
		}
		if !hasValue {
			if i+1 >= len(rest) {
				fmt.Fprintf(os.Stderr, "profile add: flag %q needs a value\n", flag)
				return 2
			}
			i++
			value = rest[i]
		}
		switch flag {
		case "--mode":
			mode = value
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
		"permissionMode": mode, "autoAccept": autoAccept, "thinkingLevel": thinkingLevel, "notes": notes,
	}, &p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile add: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, profileModeColumn(p), p.ThinkingLevel)
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
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tMODE\tTHINKING")
	for _, p := range profiles {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, profileModeColumn(p), p.ThinkingLevel)
	}
	tw.Flush()
	return 0
}

// profileModeColumn renders a profile's permission settings for the MODE
// column: the mode id (or "default" when unset), plus "+auto-accept".
func profileModeColumn(p store.AgentProfile) string {
	mode := p.PermissionMode
	if mode == "" {
		mode = "default"
	}
	if p.AutoAccept {
		mode += "+auto-accept"
	}
	return mode
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
