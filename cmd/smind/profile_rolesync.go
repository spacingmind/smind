package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The role pack's sync commands (ADR-0058): `smind profile seed roles` with
// --check, --diff or --apply compares the seats the pack seeded with the
// current pack and writes the updates the Human did not make impossible.
// The engine is internal/profiles; this file is the CLI surface only.

const (
	seedModeCheck = "check"
	seedModeDiff  = "diff"
	seedModeApply = "apply"
)

// seedRolesArgs is the parsed flag set of `smind profile seed roles`.
type seedRolesArgs struct {
	mode       string // "", check, diff or apply
	diffFilter string // the optional word after --diff: a seat or seat.field
	backupDir  string
	takes      []profiles.Take
	dryRun     bool
}

// seedRolesUsage prints the usage line and reports the usage exit code.
func seedRolesUsage() int {
	fmt.Fprintln(os.Stderr, cmdProfileSeedUsage)
	return 2
}

// flagValue reads a value-taking flag's argument: `--flag <value>` (advancing
// i past it) or `--flag=<value>`.
func flagValue(args []string, i *int, flag string) (string, bool) {
	if v, ok := strings.CutPrefix(args[*i], flag+"="); ok {
		return v, true
	}
	if *i+1 >= len(args) {
		return "", false
	}
	*i++
	return args[*i], true
}

// parseSeedRolesFlags parses everything after `profile seed roles`: at most
// one of --check/--diff/--apply (a second is a usage error), and --backup-dir,
// --take and --dry-run only with --apply. The word right after --diff, when it
// does not start with "-", is the diff filter. No mode flag means the plain
// seeding path.
func parseSeedRolesFlags(args []string) (seedRolesArgs, int) {
	var a seedRolesArgs
	setMode := func(m string) int {
		if a.mode != "" {
			return seedRolesUsage()
		}
		a.mode = m
		return 0
	}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--check":
			if code := setMode(seedModeCheck); code != 0 {
				return a, code
			}
		case arg == "--diff":
			if code := setMode(seedModeDiff); code != 0 {
				return a, code
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				a.diffFilter = args[i]
			}
		case arg == "--apply":
			if code := setMode(seedModeApply); code != 0 {
				return a, code
			}
		case arg == "--dry-run":
			a.dryRun = true
		case arg == "--backup-dir" || strings.HasPrefix(arg, "--backup-dir="):
			dir, ok := flagValue(args, &i, "--backup-dir")
			if !ok {
				return a, seedRolesUsage()
			}
			a.backupDir = dir
		case arg == "--take" || strings.HasPrefix(arg, "--take="):
			v, ok := flagValue(args, &i, "--take")
			if !ok {
				return a, seedRolesUsage()
			}
			take, err := profiles.ParseTake(v)
			if err != nil {
				return a, seedRolesUsage()
			}
			a.takes = append(a.takes, take)
		default:
			return a, seedRolesUsage()
		}
	}
	if a.mode != seedModeApply && (a.backupDir != "" || a.takes != nil || a.dryRun) {
		return a, seedRolesUsage()
	}
	return a, 0
}

// seedRolesList reads the stored profiles the sync compares the pack with.
func seedRolesList() ([]store.AgentProfile, *wsclient.Client, error) {
	client, err := dialDaemon(context.Background())
	if err != nil {
		return nil, nil, err
	}
	var existing []store.AgentProfile
	if err := client.Call(context.Background(), "profile.list", nil, &existing); err != nil {
		client.Close()
		return nil, nil, err
	}
	return existing, client, nil
}

// cmdProfileSeedRolesCheck reports, per seat and per field, whether the stored
// value is current, stale or customised. Read-only, so it is allowed inside an
// agent run too. Exit 10 when any seat has an update available.
func cmdProfileSeedRolesCheck() int {
	existing, client, err := seedRolesList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile seed: %v\n", err)
		return 1
	}
	defer client.Close()

	checks := profiles.CheckRoles(existing)
	fmt.Printf("role pack v%d\n", profiles.PackVersion)
	updates := 0
	for _, c := range checks {
		switch c.Status {
		case profiles.SeatMissing:
			fmt.Printf("%s: missing (smind profile seed roles creates it)\n", c.Name)
			continue
		case profiles.SeatOwned:
			fmt.Printf("%s: owned by a plugin (read-only)\n", c.Name)
			continue
		}
		fmt.Printf("%s (id %d)\n", c.Name, c.ID)
		for _, f := range c.Fields {
			state := f.State
			if f.Kind == profiles.KindCapability && f.State != profiles.StateCurrent {
				state += " (capability)"
			}
			fmt.Printf("  %-20s  %s\n", f.Name, state)
		}
		if c.HasUpdates() {
			updates++
		}
	}
	if updates > 0 {
		fmt.Printf("updates available for %d seat(s)\n", updates)
		return 10
	}
	fmt.Println("up to date")
	return 0
}

// lineDiff is a unified-style line diff of a against b (a classic LCS over
// lines): " " keeps a line, "-" removes one of a, "+" adds one of b.
func lineDiff(a, b string) []string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			switch {
			case x[i] == y[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			out = append(out, " "+x[i])
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "-"+x[i])
			i++
		default:
			out = append(out, "+"+y[j])
			j++
		}
	}
	for ; i < len(x); i++ {
		out = append(out, "-"+x[i])
	}
	for ; j < len(y); j++ {
		out = append(out, "+"+y[j])
	}
	return out
}

// cmdProfileSeedRolesDiff shows, for every ok seat field that is not current
// (filtered to a seat or seat.field when filter is set), the stored text
// against the pack's. Read-only.
func cmdProfileSeedRolesDiff(filter string) int {
	existing, client, err := seedRolesList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile seed: %v\n", err)
		return 1
	}
	defer client.Close()

	any := false
	for _, c := range profiles.CheckRoles(existing) {
		if c.Status != profiles.SeatOK {
			continue
		}
		for _, f := range c.Fields {
			if f.State == profiles.StateCurrent {
				continue
			}
			if filter != "" && filter != c.Name && filter != c.Name+"."+f.Name {
				continue
			}
			any = true
			kind := ""
			if f.Kind == profiles.KindCapability {
				kind = ", capability"
			}
			fmt.Printf("--- %s.%s stored (%s%s)\n", c.Name, f.Name, f.State, kind)
			fmt.Printf("+++ %s.%s pack v%d\n", c.Name, f.Name, profiles.PackVersion)
			for _, l := range lineDiff(f.Stored, f.Want) {
				fmt.Printf("%s%s\n", l[:1], stewardText(l[1:]))
			}
		}
	}
	if !any {
		fmt.Println("no differences")
	}
	return 0
}

// cmdProfileSeedRolesApply writes the planned updates: the stale prompt fields
// of untouched seats, plus whatever a --take names, each profile written once
// through the daemon's profile.update after a full backup.
func cmdProfileSeedRolesApply(a seedRolesArgs) int {
	if os.Getenv("SMIND_RUN_ID") != "" {
		fmt.Fprintln(os.Stderr, "profile seed is for humans, not agents")
		return 1
	}
	existing, client, err := seedRolesList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile seed: %v\n", err)
		return 1
	}
	defer client.Close()

	items := profiles.PlanApply(profiles.CheckRoles(existing), existing, a.takes)
	if len(items) == 0 {
		fmt.Println("nothing to update")
		return 0
	}
	backupDir := a.backupDir
	if backupDir == "" {
		backupDir = filepath.Join(config.Dir(), "backups", "role-pack")
	}
	update := func(p store.AgentProfile, expected time.Time) (store.AgentProfile, error) {
		params := map[string]any{
			"id":   p.ID,
			"name": p.Name, "provider": p.Provider, "permissionMode": p.PermissionMode, "autoAccept": p.AutoAccept,
			"thinkingLevel": p.ThinkingLevel, "model": p.Model, "notes": p.Notes, "onRateLimit": p.OnRateLimit,
			"questions": p.Questions, "orchestrationTools": p.OrchestrationTools, "instructions": p.Instructions,
			"skills": p.Skills, "childPolicy": p.ChildPolicy, "childProfileIds": p.ChildProfileIDs,
			"expectedUpdatedAt": expected.Format(time.RFC3339Nano),
		}
		if p.FallbackProfileID != nil {
			params["fallbackProfileId"] = *p.FallbackProfileID
		}
		var out store.AgentProfile
		if err := client.Call(context.Background(), "profile.update", params, &out); err != nil {
			var rpcErr *wsclient.RPCError
			if errors.As(err, &rpcErr) && rpcErr.Code == "conflict" {
				return store.AgentProfile{}, store.ErrAgentProfileChanged
			}
			return store.AgentProfile{}, err
		}
		return out, nil
	}
	dir, results, err := profiles.ApplyRoles(items, profiles.ApplyOptions{BackupDir: backupDir, Update: update, DryRun: a.dryRun})
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile seed: %v\n", err)
		return 1
	}
	exit := 0
	for _, r := range results {
		switch r.Outcome {
		case profiles.OutcomeUpdated:
			fmt.Printf("updated %s (id %d): %s\n", r.Seat, r.ID, strings.Join(r.Fields, ", "))
		case profiles.OutcomeChangedMeanwhile:
			fmt.Printf("skipped %s (id %d): changed since the check, run --check again\n", r.Seat, r.ID)
			exit = 1
		case profiles.OutcomeDryRun:
			fmt.Printf("would update %s (id %d): %s\n", r.Seat, r.ID, strings.Join(r.Fields, ", "))
		default:
			fmt.Printf("failed %s (id %d): %s\n", r.Seat, r.ID, stewardText(fmt.Sprint(r.Err)))
			exit = 1
		}
	}
	if dir != "" {
		fmt.Printf("backup: %s\n", dir)
	}
	return exit
}
