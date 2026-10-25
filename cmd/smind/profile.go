package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

const cmdProfileSeedUsage = "usage: smind profile seed roles"

const cmdProfileAddUsage = "usage: smind profile add <name> <provider> [--mode <permissionMode>] [--auto-accept] [--thinking-level=<level>] [--model=<model>] [--on-rate-limit=<none|wait|fallback>] [--fallback-profile=<id>] [--notes=<text>] [--orchestration-tools=true|false] [--instructions=<text>] [--instructions-file=<path>] [--skills=a,b] [--child-policy=none|leaf|profiles] [--child-profiles=1,2]"

const cmdProfileEditUsage = "usage: smind profile edit <id> [--name=..] [--provider=..] [every `profile add` flag]"

func cmdProfile(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind profile <add|edit|ls|rm|seed> ...")
		return 2
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:])
	case "edit":
		return cmdProfileEdit(args[1:])
	case "ls":
		return cmdProfileList(args[1:])
	case "rm":
		return cmdProfileRemove(args[1:])
	case "seed":
		return cmdProfileSeed(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind profile: unknown subcommand %q\n", args[0])
		return 2
	}
}

// profileFlags holds one parsed `profile add`/`profile edit` flag set.
// Value-taking flags accept both `--flag value` (matching `task new`'s
// `--space <id>` convention) and `--flag=value`; the boolean --auto-accept
// accepts `--auto-accept`, `--auto-accept=true`, and `--auto-accept=false`.
type profileFlags struct {
	mode, thinkingLevel, model, notes, onRateLimit string
	instructions, instructionsFile, skills         string
	childPolicy, childProfiles, name, provider     string
	fallbackProfile                                *int64
	fallbackRaw                                    int64
	fallbackSet                                    bool
	orchTools                                      *bool
	orchToolsUnmanaged                             bool
	autoAccept                                     bool

	// set records every flag that appeared (flag name -> was given), so
	// edit can tell `--model=` (clear the model) from an untouched field.
	set map[string]bool
}

// parseProfileFlags parses args as the `profile add`/`profile edit` flag
// loop; cmd ("profile add"/"profile edit") prefixes every error message.
// The returned code is the exit code to return (0 = ok) once the message
// has already been printed. allowRename marks the edit form: it
// additionally accepts --name and --provider (add takes them as
// positionals) and lets --orchestration-tools=unmanaged mean "no opinion"
// where add demands a boolean. --fallback-profile stores the raw id in
// fallbackRaw/fallbackSet even for id 0 (which add's fallbackProfile
// pointer drops, but edit treats as "clear").
func parseProfileFlags(cmd string, args []string, allowRename bool) (profileFlags, int) {
	var f profileFlags
	f.set = make(map[string]bool)
	for i := 0; i < len(args); i++ {
		flag := args[i]
		value := ""
		hasValue := false
		if n, v, ok := strings.Cut(flag, "="); ok && strings.HasPrefix(n, "--") {
			flag, value, hasValue = n, v, true
		}
		switch flag {
		case "--auto-accept":
			f.autoAccept = true
			if hasValue {
				b, err := strconv.ParseBool(value)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: --auto-accept: %q is not a boolean\n", cmd, value)
					return f, 2
				}
				f.autoAccept = b
			}
			f.set[flag] = true
			continue
		case "--approval-policy":
			fmt.Fprintf(os.Stderr, "%s: %s\n", cmd, approvalPolicyRemovedMsg)
			return f, 2
		}
		if !hasValue {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s: flag %q needs a value\n", cmd, flag)
				return f, 2
			}
			i++
			value = args[i]
		}
		switch flag {
		case "--mode":
			f.mode = value
		case "--thinking-level":
			f.thinkingLevel = value
		case "--model":
			f.model = value
		case "--on-rate-limit":
			f.onRateLimit = value
		case "--fallback-profile":
			id, err := parseInt64(value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: --fallback-profile: invalid id %q: %v\n", cmd, value, err)
				return f, 2
			}
			f.fallbackRaw, f.fallbackSet = id, true
			if id != 0 {
				f.fallbackProfile = &id
			}
		case "--notes":
			f.notes = value
		case "--orchestration-tools":
			if allowRename && value == "unmanaged" {
				f.orchToolsUnmanaged = true
			} else {
				b, err := strconv.ParseBool(value)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: --orchestration-tools: %q is not a boolean\n", cmd, value)
					return f, 2
				}
				f.orchTools = &b
			}
		case "--instructions":
			f.instructions = value
		case "--instructions-file":
			f.instructionsFile = value
		case "--skills":
			f.skills = value
		case "--child-policy":
			f.childPolicy = value
		case "--child-profiles":
			f.childProfiles = value
		case "--name":
			if !allowRename {
				fmt.Fprintf(os.Stderr, "%s: unknown flag %q\n", cmd, flag)
				return f, 2
			}
			f.name = value
		case "--provider":
			if !allowRename {
				fmt.Fprintf(os.Stderr, "%s: unknown flag %q\n", cmd, flag)
				return f, 2
			}
			f.provider = value
		default:
			fmt.Fprintf(os.Stderr, "%s: unknown flag %q\n", cmd, flag)
			return f, 2
		}
		f.set[flag] = true
	}

	if f.set["--instructions"] && f.set["--instructions-file"] {
		fmt.Fprintf(os.Stderr, "%s: --instructions and --instructions-file are mutually exclusive\n", cmd)
		return f, 2
	}
	if f.set["--instructions-file"] {
		b, err := os.ReadFile(f.instructionsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: --instructions-file: %v\n", cmd, err)
			return f, 1
		}
		f.instructions = string(b)
	}
	return f, 0
}

// parseProfileChildIDs parses the --child-profiles flag value into profile
// ids, exiting 2 (via the returned code) on any malformed id.
func parseProfileChildIDs(cmd, value string) ([]int64, int) {
	var ids []int64
	for _, part := range splitCSV(value) {
		id, err := parseInt64(part)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: --child-profiles: invalid id %q: %v\n", cmd, part, err)
			return nil, 2
		}
		ids = append(ids, id)
	}
	return ids, 0
}

// cmdProfileAdd parses `smind profile add <name> <provider> [flags]`.
// --model (ADR-0025) names the profile's model; the daemon validates it.
func cmdProfileAdd(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdProfileAddUsage)
		return 2
	}
	name, provider := args[0], args[1]

	flags, code := parseProfileFlags("profile add", args[2:], false)
	if code != 0 {
		return code
	}
	childIDs, code := parseProfileChildIDs("profile add", flags.childProfiles)
	if code != 0 {
		return code
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{
		"name": name, "provider": provider,
		"permissionMode": flags.mode, "autoAccept": flags.autoAccept, "thinkingLevel": flags.thinkingLevel, "model": flags.model, "notes": flags.notes,
	}
	if flags.onRateLimit != "" {
		params["onRateLimit"] = flags.onRateLimit
	}
	if flags.fallbackProfile != nil {
		params["fallbackProfileId"] = *flags.fallbackProfile
	}
	if flags.orchTools != nil {
		params["orchestrationTools"] = *flags.orchTools
	}
	if flags.instructions != "" {
		params["instructions"] = flags.instructions
	}
	if skills := splitCSV(flags.skills); len(skills) > 0 {
		params["skills"] = skills
	}
	if flags.childPolicy != "" {
		params["childPolicy"] = flags.childPolicy
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

// cmdProfileEdit parses `smind profile edit <id> [flags]`. profile.update
// on the wire is a full-record replace (handleProfileUpdate), so edit
// reads the stored profile, overlays only the flags actually given (an
// empty value like `--model=` means clear), and sends the merged record;
// the daemon's registry validation rejects combinations run start would.
func cmdProfileEdit(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cmdProfileEditUsage)
		return 2
	}
	id, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile edit: invalid id %q: %v\n", args[0], err)
		return 2
	}
	flags, code := parseProfileFlags("profile edit", args[1:], true)
	if code != 0 {
		return code
	}
	if len(flags.set) == 0 {
		fmt.Fprintln(os.Stderr, cmdProfileEditUsage)
		return 2
	}
	childIDs, code := parseProfileChildIDs("profile edit", flags.childProfiles)
	if code != 0 {
		return code
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var list []store.AgentProfile
	if err := client.Call(context.Background(), "profile.list", nil, &list); err != nil {
		fmt.Fprintf(os.Stderr, "profile edit: %v\n", err)
		return 1
	}
	var current *store.AgentProfile
	for i := range list {
		if list[i].ID == id {
			current = &list[i]
			break
		}
	}
	if current == nil {
		fmt.Fprintf(os.Stderr, "profile edit: profile %d not found\n", id)
		return 1
	}

	skills := current.Skills
	if skills == nil {
		skills = []string{}
	}
	currentChildIDs := current.ChildProfileIDs
	if currentChildIDs == nil {
		currentChildIDs = []int64{}
	}
	params := map[string]any{
		"id":   id,
		"name": current.Name, "provider": current.Provider, "permissionMode": current.PermissionMode,
		"autoAccept": current.AutoAccept, "thinkingLevel": current.ThinkingLevel, "model": current.Model,
		"notes": current.Notes, "onRateLimit": current.OnRateLimit,
		"orchestrationTools": current.OrchestrationTools, "instructions": current.Instructions,
		"skills": skills, "childPolicy": current.ChildPolicy, "childProfileIds": currentChildIDs,
	}
	if current.FallbackProfileID != nil {
		params["fallbackProfileId"] = *current.FallbackProfileID
	}
	if flags.set["--name"] {
		params["name"] = flags.name
	}
	if flags.set["--provider"] {
		params["provider"] = flags.provider
	}
	if flags.set["--mode"] {
		params["permissionMode"] = flags.mode
	}
	if flags.set["--auto-accept"] {
		params["autoAccept"] = flags.autoAccept
	}
	if flags.set["--thinking-level"] {
		params["thinkingLevel"] = flags.thinkingLevel
	}
	if flags.set["--model"] {
		params["model"] = flags.model
	}
	if flags.set["--notes"] {
		params["notes"] = flags.notes
	}
	if flags.set["--on-rate-limit"] {
		params["onRateLimit"] = flags.onRateLimit
	}
	if flags.fallbackSet {
		if flags.fallbackRaw == 0 {
			delete(params, "fallbackProfileId")
		} else {
			params["fallbackProfileId"] = flags.fallbackRaw
		}
	}
	if flags.set["--orchestration-tools"] {
		if flags.orchToolsUnmanaged {
			params["orchestrationTools"] = nil
		} else {
			params["orchestrationTools"] = *flags.orchTools
		}
	}
	if flags.set["--instructions"] || flags.set["--instructions-file"] {
		params["instructions"] = flags.instructions
	}
	if flags.set["--skills"] {
		params["skills"] = splitCSV(flags.skills)
	}
	if flags.set["--child-policy"] {
		params["childPolicy"] = flags.childPolicy
	}
	if flags.set["--child-profiles"] {
		params["childProfileIds"] = childIDs
	}

	var p store.AgentProfile
	if err := client.Call(context.Background(), "profile.update", params, &p); err != nil {
		fmt.Fprintf(os.Stderr, "profile edit: %v\n", err)
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
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tMODE\tTHINKING\tMODEL\tON-RATE-LIMIT\tROLE\tOWNER")
	for _, p := range profiles {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Provider, profileModeColumn(p), p.ThinkingLevel, p.Model, profileRateLimitColumn(p), profileRole(p), ownerColumn(p.Owner))
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

// cmdProfileSeed parses `smind profile seed roles`: it creates the standard
// seat pack (ADR-0041 S4a) over the profile.list and profile.create RPCs,
// leaving every existing profile of the same name untouched. It is a human
// command: inside an agent run (SMIND_RUN_ID set) it refuses.
func cmdProfileSeed(args []string) int {
	if len(args) != 1 || args[0] != "roles" {
		fmt.Fprintln(os.Stderr, cmdProfileSeedUsage)
		return 2
	}
	if os.Getenv("SMIND_RUN_ID") != "" {
		fmt.Fprintln(os.Stderr, "profile seed is for humans, not agents")
		return 1
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var existing []store.AgentProfile
	if err := client.Call(context.Background(), "profile.list", nil, &existing); err != nil {
		fmt.Fprintf(os.Stderr, "profile seed: %v\n", err)
		return 1
	}
	create := func(p store.AgentProfile) (store.AgentProfile, error) {
		params := map[string]any{
			"name": p.Name, "provider": p.Provider, "permissionMode": p.PermissionMode, "autoAccept": p.AutoAccept,
			"thinkingLevel": p.ThinkingLevel, "model": p.Model, "notes": p.Notes, "onRateLimit": p.OnRateLimit,
			"orchestrationTools": p.OrchestrationTools, "instructions": p.Instructions, "skills": p.Skills,
			"childPolicy": p.ChildPolicy, "childProfileIds": p.ChildProfileIDs,
		}
		if p.FallbackProfileID != nil {
			params["fallbackProfileId"] = *p.FallbackProfileID
		}
		var created store.AgentProfile
		err := client.Call(context.Background(), "profile.create", params, &created)
		return created, err
	}
	results, err := profiles.SeedRolesWith(create, existing)
	for _, r := range results {
		fmt.Printf("%s %s (id %d)\n", r.Action, r.Name, r.ID)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile seed: %v\n", err)
		return 1
	}
	return 0
}
