package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

const cmdAutomationUsage = `usage: smind automation <command>

  smind automation list [--json]                  automations, their state and the global switch
  smind automation show <id> [--json]             one automation: definition, state, next fire times
  smind automation create --file <automation.json> | flags   create DISABLED (a human act, local only)
      --name N --workspace <id> --profile <id> --model M --mode default
      --cron "17 19 * * *" | --workflow "Windows full suite" [--branch develop]
      --title T --prompt P [--budget 3] [--max-runs-per-hour 2] [--memory]
  smind automation enable <id>                    arm it (nothing fires until automations.enabled is true in config.yaml)
  smind automation disable <id>                   turn it off
  smind automation resume <id>                    clear an auto-pause (failure counter and budget block too)
  smind automation fire <id> [--json]             fire now (same rails; skips only the dedupe window)
  smind automation audit <id> [--limit N] [--json]  every signal decision, newest first
  smind automation memory <id> [--clear]          show or clear the automation's memory
  smind automation delete <id>                    delete it and its history

Automations start tasks unattended. They are off until automations.enabled is true in config.yaml
(a human edit; no command or agent can set it). Creating, enabling and firing are local only.`

func cmdAutomation(args []string) int {
	return runAutomation(args, pluginIO{in: os.Stdin, out: os.Stdout, errOut: os.Stderr, dial: dialDaemonCaller})
}

func runAutomation(args []string, env pluginIO) int {
	if len(args) == 0 {
		fmt.Fprintln(env.errOut, cmdAutomationUsage)
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list", "ls":
		return autoList(rest, env)
	case "show":
		return autoShow(rest, env)
	case "create":
		return autoCreate(rest, env)
	case "enable":
		return autoSimple(rest, env, "enable", "automation.enable", "enabled")
	case "disable":
		return autoSimple(rest, env, "disable", "automation.disable", "disabled")
	case "resume":
		return autoSimple(rest, env, "resume", "automation.resume", "resumed")
	case "delete", "rm":
		return autoSimple(rest, env, "delete", "automation.delete", "deleted")
	case "fire":
		return autoFire(rest, env)
	case "audit":
		return autoAudit(rest, env)
	case "memory":
		return autoMemory(rest, env)
	case "-h", "--help", "help":
		fmt.Fprintln(env.out, cmdAutomationUsage)
		return 0
	}
	fmt.Fprintf(env.errOut, "smind automation: unknown subcommand %q\n%s\n", verb, cmdAutomationUsage)
	return 2
}

func autoFail(env pluginIO, verb string, err error) int {
	fmt.Fprintf(env.errOut, "automation %s: %v\n", verb, err)
	return 1
}

// autoParse parses flags that may come before or after the positional
// arguments (smind automation audit 3 --limit 5) and returns the positionals.
func autoParse(name string, args []string, env pluginIO, setup func(*flag.FlagSet)) (*flag.FlagSet, []string, int) {
	fs := flag.NewFlagSet("automation "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if setup != nil {
		setup(fs)
	}
	var flagArgs, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		if !strings.Contains(a, "=") && !isBoolFlag(fs, a) && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintf(env.errOut, "smind automation %s: %v\n", name, err)
		return nil, nil, 2
	}
	return fs, pos, -1
}

func isBoolFlag(fs *flag.FlagSet, arg string) bool {
	f := fs.Lookup(strings.TrimLeft(arg, "-"))
	if f == nil {
		return false
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// autoID parses the single positional id argument.
func autoID(pos []string) (int64, bool) {
	if len(pos) != 1 {
		return 0, false
	}
	id, err := strconv.ParseInt(pos[0], 10, 64)
	return id, err == nil && id > 0
}

type autoRow struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	Enabled             bool           `json:"enabled"`
	WorkspaceID         int64          `json:"workspaceId"`
	Signal              map[string]any `json:"signal"`
	Template            map[string]any `json:"template"`
	Rails               map[string]any `json:"rails"`
	Status              string         `json:"status"`
	PausedReason        string         `json:"pausedReason"`
	ConsecutiveFailures int            `json:"consecutiveFailures"`
	BudgetBlocked       bool           `json:"budgetBlocked"`
	LastFiredAt         *string        `json:"lastFiredAt"`
	NextRunAt           *string        `json:"nextRunAt"`
	NextRuns            []string       `json:"nextRuns"`
}

type autoListing struct {
	Enabled     bool      `json:"enabled"`
	Paused      bool      `json:"paused"`
	Automations []autoRow `json:"automations"`
}

func autoState(a autoRow) string {
	switch {
	case a.Status == "paused":
		return "paused"
	case a.Enabled:
		return "active"
	}
	return "disabled"
}

func autoSignal(a autoRow) string {
	if c, ok := a.Signal["cron"].(string); ok && a.Signal["kind"] == "cron" {
		return "cron " + c + " (UTC)"
	}
	return fmt.Sprintf("%v: %v on %v", a.Signal["kind"], a.Signal["workflow"], a.Signal["branch"])
}

func autoList(args []string, env pluginIO) int {
	fs, _, code := autoParse("list", args, env, func(fs *flag.FlagSet) { fs.Bool("json", false, "") })
	if fs == nil {
		return code
	}
	var l autoListing
	if err := env.call("list", "automation.list", nil, &l); err != nil {
		return autoFail(env, "list", err)
	}
	if fs.Lookup("json").Value.String() == "true" {
		b, _ := json.MarshalIndent(l, "", "  ")
		fmt.Fprintln(env.out, string(b))
		return 0
	}
	if !l.Enabled {
		fmt.Fprintln(env.out, "Automations are OFF: set automations.enabled: true in config.yaml (a human edit) to let them fire.")
	} else if l.Paused {
		fmt.Fprintln(env.out, "Automations are globally PAUSED (automations.paused: true): signals are recorded, nothing fires.")
	}
	if len(l.Automations) == 0 {
		fmt.Fprintln(env.out, "No automations.")
		return 0
	}
	tw := tabwriter.NewWriter(env.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATE\tSIGNAL\tLAST FIRED")
	for _, a := range l.Automations {
		last := "-"
		if a.LastFiredAt != nil {
			last = *a.LastFiredAt
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", a.ID, a.Name, autoState(a), autoSignal(a), last)
	}
	tw.Flush()
	return 0
}

func autoShow(args []string, env pluginIO) int {
	fs, pos, code := autoParse("show", args, env, func(fs *flag.FlagSet) { fs.Bool("json", false, "") })
	if fs == nil {
		return code
	}
	id, ok := autoID(pos)
	if !ok {
		fmt.Fprintln(env.errOut, "usage: smind automation show <id> [--json]")
		return 2
	}
	var a autoRow
	if err := env.call("show", "automation.get", map[string]any{"id": id}, &a); err != nil {
		return autoFail(env, "show", err)
	}
	if fs.Lookup("json").Value.String() == "true" {
		b, _ := json.MarshalIndent(a, "", "  ")
		fmt.Fprintln(env.out, string(b))
		return 0
	}
	fmt.Fprintf(env.out, "#%d %s  [%s]\nsignal: %s\n", a.ID, a.Name, autoState(a), autoSignal(a))
	if a.PausedReason != "" {
		fmt.Fprintf(env.out, "PAUSED: %s\n  resume with: smind automation resume %d\n", a.PausedReason, a.ID)
	}
	if a.BudgetBlocked {
		fmt.Fprintln(env.out, "capped: the last run reached its budget (resume or edit to clear)")
	}
	if len(a.NextRuns) > 0 {
		fmt.Fprintf(env.out, "next fire times (UTC): %s\n", strings.Join(a.NextRuns, ", "))
	}
	fmt.Fprintf(env.out, "consecutive failures: %d\n", a.ConsecutiveFailures)
	tpl, _ := json.MarshalIndent(a.Template, "", "  ")
	rails, _ := json.MarshalIndent(a.Rails, "", "  ")
	fmt.Fprintf(env.out, "template: %s\nrails: %s\n", tpl, rails)
	return 0
}

func autoCreate(args []string, env pluginIO) int {
	var file, name, cron, workflow, branch, model, mode, title, prompt string
	var workspace, profile int64
	var budget float64
	var maxRuns int
	var memory bool
	fs, _, code := autoParse("create", args, env, func(fs *flag.FlagSet) {
		fs.StringVar(&file, "file", "", "")
		fs.StringVar(&name, "name", "", "")
		fs.StringVar(&cron, "cron", "", "")
		fs.StringVar(&workflow, "workflow", "", "")
		fs.StringVar(&branch, "branch", "", "")
		fs.StringVar(&model, "model", "", "")
		fs.StringVar(&mode, "mode", "default", "")
		fs.StringVar(&title, "title", "", "")
		fs.StringVar(&prompt, "prompt", "", "")
		fs.Int64Var(&workspace, "workspace", 0, "")
		fs.Int64Var(&profile, "profile", 0, "")
		fs.Float64Var(&budget, "budget", 0, "")
		fs.IntVar(&maxRuns, "max-runs-per-hour", 0, "")
		fs.BoolVar(&memory, "memory", false, "")
	})
	if fs == nil {
		return code
	}
	var def map[string]any
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return autoFail(env, "create", err)
		}
		if err := json.Unmarshal(b, &def); err != nil {
			return autoFail(env, "create", fmt.Errorf("%s: %w", file, err))
		}
	} else {
		sig := map[string]any{}
		switch {
		case cron != "" && workflow == "":
			sig = map[string]any{"kind": "cron", "cron": cron}
		case workflow != "" && cron == "":
			sig = map[string]any{"kind": "github.workflow_run", "workflow": workflow}
			if branch != "" {
				sig["branch"] = branch
			}
		default:
			fmt.Fprintln(env.errOut, "automation create: pass exactly one of --cron or --workflow (or --file)")
			return 2
		}
		tpl := map[string]any{"profileId": profile, "model": model, "mode": mode, "titleTemplate": title, "promptTemplate": prompt}
		if budget > 0 {
			tpl["budgetUsd"] = budget
		}
		rails := map[string]any{"memoryEnabled": memory}
		if maxRuns > 0 {
			rails["maxRunsPerHour"] = maxRuns
		}
		def = map[string]any{"name": name, "workspaceId": workspace, "signal": sig, "template": tpl, "rails": rails}
	}
	var a autoRow
	if err := env.call("create", "automation.create", def, &a); err != nil {
		return autoFail(env, "create", err)
	}
	fmt.Fprintf(env.out, "Created automation #%d %q (DISABLED). To arm it: smind automation enable %d\n", a.ID, a.Name, a.ID)
	return 0
}

func autoSimple(args []string, env pluginIO, verb, method, done string) int {
	id, ok := autoID(args)
	if !ok {
		fmt.Fprintf(env.errOut, "usage: smind automation %s <id>\n", verb)
		return 2
	}
	var out json.RawMessage
	if err := env.call(verb, method, map[string]any{"id": id}, &out); err != nil {
		return autoFail(env, verb, err)
	}
	fmt.Fprintf(env.out, "automation #%d %s.\n", id, done)
	return 0
}

type autoAuditRow struct {
	ID          int64   `json:"id"`
	SignalKind  string  `json:"signalKind"`
	Fingerprint string  `json:"fingerprint"`
	Decision    string  `json:"decision"`
	Reason      string  `json:"reason"`
	Manual      bool    `json:"manual"`
	Catchup     bool    `json:"catchup"`
	TaskID      *int64  `json:"taskId"`
	RunID       string  `json:"runId"`
	Outcome     string  `json:"outcome"`
	Note        string  `json:"note"`
	CreatedAt   string  `json:"createdAt"`
	FinishedAt  *string `json:"finishedAt"`
}

func autoFire(args []string, env pluginIO) int {
	fs, pos, code := autoParse("fire", args, env, func(fs *flag.FlagSet) { fs.Bool("json", false, "") })
	if fs == nil {
		return code
	}
	id, ok := autoID(pos)
	if !ok {
		fmt.Fprintln(env.errOut, "usage: smind automation fire <id> [--json]")
		return 2
	}
	var r autoAuditRow
	if err := env.call("fire", "automation.fire", map[string]any{"id": id}, &r); err != nil {
		return autoFail(env, "fire", err)
	}
	if fs.Lookup("json").Value.String() == "true" {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Fprintln(env.out, string(b))
		return 0
	}
	switch {
	case r.Decision == "fire" && r.TaskID != nil:
		fmt.Fprintf(env.out, "fired: task #%d (audit #%d)\n", *r.TaskID, r.ID)
	case r.Decision == "fire":
		fmt.Fprintf(env.out, "fire recorded but no task started: %s (audit #%d)\n", r.Note, r.ID)
		return 1
	default:
		fmt.Fprintf(env.out, "%s: %s (audit #%d)\n", r.Decision, r.Reason, r.ID)
		return 1
	}
	return 0
}

func autoAudit(args []string, env pluginIO) int {
	var limit int
	fs, pos, code := autoParse("audit", args, env, func(fs *flag.FlagSet) {
		fs.Bool("json", false, "")
		fs.IntVar(&limit, "limit", 20, "")
	})
	if fs == nil {
		return code
	}
	id, ok := autoID(pos)
	if !ok {
		fmt.Fprintln(env.errOut, "usage: smind automation audit <id> [--limit N] [--json]")
		return 2
	}
	var out struct {
		Rows []autoAuditRow `json:"rows"`
	}
	if err := env.call("audit", "automation.audit", map[string]any{"id": id, "limit": limit}, &out); err != nil {
		return autoFail(env, "audit", err)
	}
	if fs.Lookup("json").Value.String() == "true" {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(env.out, string(b))
		return 0
	}
	if len(out.Rows) == 0 {
		fmt.Fprintln(env.out, "No signals recorded.")
		return 0
	}
	tw := tabwriter.NewWriter(env.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "AUDIT\tWHEN\tDECISION\tTASK\tOUTCOME\tREASON")
	for _, r := range out.Rows {
		task := "-"
		if r.TaskID != nil {
			task = "#" + strconv.FormatInt(*r.TaskID, 10)
		}
		reason := r.Reason
		if r.Note != "" {
			reason = strings.TrimSpace(reason + " " + r.Note)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.CreatedAt, r.Decision, task, r.Outcome, reason)
	}
	tw.Flush()
	return 0
}

func autoMemory(args []string, env pluginIO) int {
	fs, pos, code := autoParse("memory", args, env, func(fs *flag.FlagSet) { fs.Bool("clear", false, "") })
	if fs == nil {
		return code
	}
	id, ok := autoID(pos)
	if !ok {
		fmt.Fprintln(env.errOut, "usage: smind automation memory <id> [--clear]")
		return 2
	}
	if fs.Lookup("clear").Value.String() == "true" {
		var out json.RawMessage
		if err := env.call("memory", "automation.memory.clear", map[string]any{"id": id}, &out); err != nil {
			return autoFail(env, "memory", err)
		}
		fmt.Fprintf(env.out, "memory of automation #%d cleared.\n", id)
		return 0
	}
	var out struct {
		Memory string `json:"memory"`
	}
	if err := env.call("memory", "automation.memory.get", map[string]any{"id": id}, &out); err != nil {
		return autoFail(env, "memory", err)
	}
	if out.Memory == "" {
		fmt.Fprintln(env.out, "(no memory)")
		return 0
	}
	fmt.Fprint(env.out, out.Memory)
	return 0
}
