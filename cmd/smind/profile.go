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

const cmdProfileAddUsage = "usage: smind profile add <name> <provider> [--mode <permissionMode>] [--auto-accept] [--thinking-level=<level>] [--model=<model>] [--on-rate-limit=<none|wait|fallback>] [--fallback-profile=<id>] [--notes=<text>] [--orchestration-tools=true|false] [--instructions=<text>] [--instructions-file=<path>] [--skills=a,b] [--child-policy=none|leaf|profiles] [--child-profiles=1,2]"

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
// `--auto-accept=false`. --model (ADR-0025) names the profile's model; the
// daemon validates it.
func cmdProfileAdd(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdProfileAddUsage)
		return 2
	}
	name, provider := args[0], args[1]

	var mode, thinkingLevel, model, notes, onRateLimit string
	var fallbackProfile *int64
	var orchTools *bool
	var instructions, instructionsFile, skillsList, childPolicy, childProfiles string
	var instructionsSet, instructionsFileSet bool
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
		case "--model":
			model = value
		case "--on-rate-limit":
			onRateLimit = value
		case "--fallback-profile":
			id, err := parseInt64(value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "profile add: --fallback-profile: invalid id %q: %v\n", value, err)
				return 2
			}
			if id != 0 {
				fallbackProfile = &id
			}
		case "--notes":
			notes = value
		case "--orchestration-tools":
			b, err := strconv.ParseBool(value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "profile add: --orchestration-tools: %q is not a boolean\n", value)
				return 2
			}
			orchTools = &b
		case "--instructions":
			instructions, instructionsSet = value, true
		case "--instructions-file":
			instructionsFile, instructionsFileSet = value, true
		case "--skills":
			skillsList = value
		case "--child-policy":
			childPolicy = value
		case "--child-profiles":
			childProfiles = value
		default:
			fmt.Fprintf(os.Stderr, "profile add: unknown flag %q\n", flag)
			return 2
		}
	}

	if instructionsSet && instructionsFileSet {
		fmt.Fprintln(os.Stderr, "profile add: --instructions and --instructions-file are mutually exclusive")
		return 2
	}
	if instructionsFileSet {
		b, err := os.ReadFile(instructionsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "profile add: --instructions-file: %v\n", err)
			return 1
		}
		instructions = string(b)
	}
	var childIDs []int64
	for _, part := range splitCSV(childProfiles) {
		id, err := parseInt64(part)
		if err != nil {
			fmt.Fprintf(os.Stderr, "profile add: --child-profiles: invalid id %q: %v\n", part, err)
			return 2
		}
		childIDs = append(childIDs, id)
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{
		"name": name, "provider": provider,
		"permissionMode": mode, "autoAccept": autoAccept, "thinkingLevel": thinkingLevel, "model": model, "notes": notes,
	}
	if onRateLimit != "" {
		params["onRateLimit"] = onRateLimit
	}
	if fallbackProfile != nil {
		params["fallbackProfileId"] = *fallbackProfile
	}
	if orchTools != nil {
		params["orchestrationTools"] = *orchTools
	}
	if instructions != "" {
		params["instructions"] = instructions
	}
	if skills := splitCSV(skillsList); len(skills) > 0 {
		params["skills"] = skills
	}
	if childPolicy != "" {
		params["childPolicy"] = childPolicy
	}
	if len(childIDs) > 0 {
		params["childProfileIds"] = childIDs
	}
	var p store.AgentProfile
	err = client.Call(context.Background(), "profile.create", params, &p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile add: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, profileModeColumn(p), p.ThinkingLevel, p.Model)
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
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tMODE\tTHINKING\tMODEL\tON-RATE-LIMIT\tROLE")
	for _, p := range profiles {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, profileModeColumn(p), p.ThinkingLevel, p.Model, profileRateLimitColumn(p), profileRole(p))
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

// profileRateLimitColumn renders a profile's failover policy for the
// ON-RATE-LIMIT column: "none", "wait" or "fallback→<id>".
func profileRateLimitColumn(p store.AgentProfile) string {
	switch p.OnRateLimit {
	case "", "none":
		return "none"
	case "fallback":
		if p.FallbackProfileID != nil {
			return fmt.Sprintf("fallback→%d", *p.FallbackProfileID)
		}
	}
	return p.OnRateLimit
}

// profileRole derives the display label for a profile's seat (ADR-0041): a
// label only, never stored. "-" is an unmanaged (legacy) profile.
func profileRole(p store.AgentProfile) string {
	if p.OrchestrationTools == nil {
		return "-"
	}
	if !*p.OrchestrationTools {
		return "peer"
	}
	switch p.ChildPolicy {
	case "leaf":
		return "lead"
	case "profiles":
		return "supervisor"
	default:
		return "orchestrator"
	}
}

// splitCSV splits a comma-separated flag value, trimming blanks.
func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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
