package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/wsapi"
	"github.com/spacingmind/smind/internal/wsclient"
)

// `smind learn` is the CLI side of the learning RPCs (ADR-0026 §8): humans
// manage learned skills and proposals here; no MCP tool does.

const cmdLearnUsage = `usage: smind learn <subcommand> [--json]
  status                                              kill switch and per-workspace counts
  ls [--workspace ID] [--proposals|--active|--archived]  proposals and skills
  show [--proposal|--skill] <id>                      one proposal or skill (default: skill)
  approve <proposal-id> [--body-file F] [--desc TEXT] [--merge-into NAME]
  reject <proposal-id> [--note TEXT]
  pin|unpin|disable|enable|archive|restore|delete <skill-id>
  undo <skill-id> --version N
  reflect <chat-id>                                   reflect on a chat now
  curate [--workspace ID]                             curate a workspace's skills now
  mode <workspace-id> off|propose                     per-workspace learning mode
  forget <workspace-id> --yes                         hard-delete all learning data of a workspace
  probe <proposal-id> [--quick] [--dry-run]           run an A/A2/B probe of a proposal (--dry-run: estimate only)
  probe show <proposal-id> [--json]                    one probe: status, arms, contrast
  export <skill-id> --to repo|claude|agents|codex [--force]
                                                      copy an approved skill to a repo or user skills dir
                                                      (exit 1 if an edited file was left alone; --force overwrites)`

// learnArgs is a parsed subcommand command line: flags may appear before or
// after the positionals, as `--flag value` or `--flag=value`.
type learnArgs struct {
	pos   []string
	vals  map[string]string
	bools map[string]bool
}

func parseLearnArgs(args []string, valueFlags, boolFlags []string) (learnArgs, error) {
	a := learnArgs{vals: map[string]string{}, bools: map[string]bool{}}
	isIn := func(set []string, s string) bool {
		for _, v := range set {
			if v == s {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			a.pos = append(a.pos, arg)
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch {
		case isIn(boolFlags, name):
			a.bools[name] = true
			if hasValue {
				b, err := strconv.ParseBool(value)
				if err != nil {
					return a, fmt.Errorf("%s: %q is not a boolean", name, value)
				}
				a.bools[name] = b
			}
		case isIn(valueFlags, name):
			if !hasValue {
				if i+1 >= len(args) {
					return a, fmt.Errorf("flag %q needs a value", name)
				}
				i++
				value = args[i]
			}
			a.vals[name] = value
		default:
			return a, fmt.Errorf("unknown flag %q", name)
		}
	}
	return a, nil
}

func (a learnArgs) id(n int, what string) (int64, error) {
	if len(a.pos) != n+1 {
		return 0, fmt.Errorf("expected exactly %d argument(s): %s", n+1, what)
	}
	return parseLearnID(a.pos[n], what)
}

func parseLearnID(s, what string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s: %q is not a positive integer", what, s)
	}
	return id, nil
}

func learnUsageErr(sub string, err error) int {
	fmt.Fprintf(os.Stderr, "smind learn %s: %v\n%s\n", sub, err, cmdLearnUsage)
	return 2
}

func cmdLearn(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cmdLearnUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status":
		return learnStatus(rest)
	case "ls":
		return learnLs(rest)
	case "show":
		return learnShow(rest)
	case "approve":
		return learnApprove(rest)
	case "reject":
		return learnReject(rest)
	case "pin", "unpin", "disable", "enable", "archive", "restore", "delete":
		return learnSkillAction(sub, rest)
	case "undo":
		return learnUndo(rest)
	case "reflect":
		return learnReflect(rest)
	case "curate":
		return learnCurate(rest)
	case "mode":
		return learnMode(rest)
	case "forget":
		return learnForget(rest)
	case "probe":
		return learnProbe(rest)
	case "export":
		return learnExport(rest)
	default:
		fmt.Fprintf(os.Stderr, "smind learn: unknown subcommand %q\n%s\n", sub, cmdLearnUsage)
		return 2
	}
}

// learnCall dials the daemon, makes one call and reports an RPC error the
// way every learn subcommand does. It returns the exit code (0 = ok).
func learnCall(sub, method string, params, out any) int {
	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()
	return learnCallOn(client, sub, method, params, out)
}

func learnCallOn(client *wsclient.Client, sub, method string, params, out any) int {
	if err := client.Call(context.Background(), method, params, out); err != nil {
		reportLearnError(sub, err)
		return 1
	}
	return 0
}

// gateFindings are the findings of a "gate_rejected" error (RPCError.Data).
type gateFindings struct {
	Findings []struct {
		Reason string `json:"reason"`
		Detail string `json:"detail"`
	} `json:"findings"`
}

func reportLearnError(sub string, err error) {
	var rpc *wsclient.RPCError
	if errors.As(err, &rpc) && rpc.Code == "gate_rejected" {
		var g gateFindings
		if json.Unmarshal(rpc.Data, &g) == nil && len(g.Findings) > 0 {
			fmt.Fprintf(os.Stderr, "smind learn %s: refused by the safety gate; nothing was changed:\n", sub)
			for _, f := range g.Findings {
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", f.Reason, f.Detail)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "smind learn %s: %v\n", sub, err)
}

func printLearnJSON(raw json.RawMessage) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		fmt.Println(string(raw))
		return
	}
	fmt.Println(buf.String())
}

func learnTable() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0) }

// learnStatusResult mirrors the learning.status wire result.
type learnStatusResult struct {
	Enabled    bool `json:"enabled"`
	Workspaces []struct {
		ID               int64  `json:"id"`
		Title            string `json:"title"`
		LearningMode     string `json:"learningMode"`
		PendingProposals int    `json:"pendingProposals"`
		ActiveSkills     int    `json:"activeSkills"`
		DisabledSkills   int    `json:"disabledSkills"`
	} `json:"workspaces"`
}

func learnStatus(args []string) int {
	a, err := parseLearnArgs(args, nil, []string{"--json"})
	if err != nil || len(a.pos) != 0 {
		if err == nil {
			err = errors.New("status takes no arguments")
		}
		return learnUsageErr("status", err)
	}
	var raw json.RawMessage
	if code := learnCall("status", "learning.status", nil, &raw); code != 0 {
		return code
	}
	if a.bools["--json"] {
		printLearnJSON(raw)
		return 0
	}
	var st learnStatusResult
	if err := json.Unmarshal(raw, &st); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn status: decode: %v\n", err)
		return 1
	}
	if st.Enabled {
		fmt.Println("learning: enabled")
	} else {
		fmt.Println("learning: disabled (global kill switch)")
	}
	if len(st.Workspaces) == 0 {
		fmt.Println("no workspaces")
		return 0
	}
	tw := learnTable()
	fmt.Fprintln(tw, "WORKSPACE\tTITLE\tMODE\tPENDING\tACTIVE\tDISABLED")
	for _, w := range st.Workspaces {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\t%d\n", w.ID, w.Title, w.LearningMode, w.PendingProposals, w.ActiveSkills, w.DisabledSkills)
	}
	tw.Flush()
	return 0
}

// learnWorkspaces returns the ids to list: the given one, or every workspace.
func learnWorkspaces(client *wsclient.Client, sub, flag string) ([]int64, int) {
	if flag != "" {
		id, err := parseLearnID(flag, "--workspace")
		if err != nil {
			return nil, learnUsageErr(sub, err)
		}
		return []int64{id}, 0
	}
	var st learnStatusResult
	if code := learnCallOn(client, sub, "learning.status", nil, &st); code != 0 {
		return nil, code
	}
	ids := make([]int64, 0, len(st.Workspaces))
	for _, w := range st.Workspaces {
		ids = append(ids, w.ID)
	}
	return ids, 0
}

func learnLs(args []string) int {
	a, err := parseLearnArgs(args, []string{"--workspace"}, []string{"--proposals", "--active", "--archived", "--json"})
	if err != nil || len(a.pos) != 0 {
		if err == nil {
			err = errors.New("ls takes no arguments")
		}
		return learnUsageErr("ls", err)
	}
	wantProposals := a.bools["--proposals"]
	wantActive := a.bools["--active"]
	wantArchived := a.bools["--archived"]
	if !wantProposals && !wantActive && !wantArchived {
		wantProposals, wantActive = true, true
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()
	ids, code := learnWorkspaces(client, "ls", a.vals["--workspace"])
	if code != 0 {
		return code
	}

	var statuses []string
	if wantActive {
		statuses = append(statuses, "active", "disabled")
	}
	if wantArchived {
		statuses = append(statuses, "archived")
	}
	type wsRows struct {
		WorkspaceID int64                    `json:"workspaceId"`
		Proposals   []wsapi.ProposalView     `json:"proposals,omitempty"`
		Skills      []wsapi.LearnedSkillView `json:"skills,omitempty"`
	}
	var all []wsRows
	for _, id := range ids {
		row := wsRows{WorkspaceID: id}
		if wantProposals {
			if code := learnCallOn(client, "ls", "learning.proposals.list", map[string]any{"workspaceId": id, "status": "pending"}, &row.Proposals); code != 0 {
				return code
			}
		}
		if len(statuses) > 0 {
			if code := learnCallOn(client, "ls", "learning.skills.list", map[string]any{"workspaceId": id, "statuses": statuses}, &row.Skills); code != 0 {
				return code
			}
		}
		all = append(all, row)
	}
	if a.bools["--json"] {
		raw, _ := json.Marshal(all)
		printLearnJSON(raw)
		return 0
	}

	printed := false
	for _, row := range all {
		if len(row.Proposals) > 0 {
			printed = true
			fmt.Printf("workspace %d: proposals\n", row.WorkspaceID)
			tw := learnTable()
			fmt.Fprintln(tw, "ID\tKIND\tNAME\tSOURCE\tNOTE")
			for _, p := range row.Proposals {
				note := "-"
				switch {
				case p.Malformed:
					note = "malformed payload"
				case p.PossibleDuplicateOf != "":
					note = fmt.Sprintf("possible duplicate of %s (%.2f)", p.PossibleDuplicateOf, p.Similarity)
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", p.ID, p.Kind, p.Name, p.Source, note)
			}
			tw.Flush()
		}
		if len(row.Skills) > 0 {
			printed = true
			fmt.Printf("workspace %d: skills\n", row.WorkspaceID)
			tw := learnTable()
			fmt.Fprintln(tw, "ID\tNAME\tCATEGORY\tSTATUS\tLOADED/OFFERED\tRATE\tFLAGS")
			for _, k := range row.Skills {
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d/%d\t%.0f%%\t%s\n", k.ID, k.Name, k.Category, k.Status, k.LoadedRuns, k.OfferedRuns, k.LoadRate*100, skillFlags(k))
			}
			tw.Flush()
		}
	}
	if !printed {
		fmt.Println("nothing to show")
	}
	return 0
}

func skillFlags(k wsapi.LearnedSkillView) string {
	var f []string
	if k.Pinned {
		f = append(f, "pinned")
	}
	if k.Probation {
		f = append(f, "probation")
	}
	if !k.Measured {
		f = append(f, "unmeasured")
	}
	return strings.Join(f, ",")
}

func learnShow(args []string) int {
	a, err := parseLearnArgs(args, nil, []string{"--proposal", "--skill", "--json"})
	if err != nil {
		return learnUsageErr("show", err)
	}
	if a.bools["--proposal"] && a.bools["--skill"] {
		return learnUsageErr("show", errors.New("--proposal and --skill are mutually exclusive"))
	}
	id, err := a.id(0, "<id>")
	if err != nil {
		return learnUsageErr("show", err)
	}
	var raw json.RawMessage
	if a.bools["--proposal"] {
		if code := learnCall("show", "learning.proposals.get", map[string]any{"id": id}, &raw); code != 0 {
			return code
		}
		if a.bools["--json"] {
			printLearnJSON(raw)
			return 0
		}
		return printProposal(raw)
	}
	if code := learnCall("show", "learning.skills.get", map[string]any{"id": id}, &raw); code != 0 {
		return code
	}
	if a.bools["--json"] {
		printLearnJSON(raw)
		return 0
	}
	return printSkill(raw)
}

func printSkill(raw json.RawMessage) int {
	var d struct {
		Skill    wsapi.LearnedSkillView  `json:"skill"`
		Versions []wsapi.VersionView     `json:"versions"`
		Ledger   []wsapi.LedgerEntryView `json:"ledger"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn show: decode: %v\n", err)
		return 1
	}
	k := d.Skill
	fmt.Printf("skill %d: %s\n", k.ID, k.Name)
	fmt.Printf("workspace: %d\ncategory: %s\nstatus: %s\nversion: %d\nsource: %s\n", k.WorkspaceID, k.Category, k.Status, k.Version, k.Source)
	fmt.Printf("flags: %s\n", skillFlags(k))
	fmt.Printf("loaded/offered: %d/%d (%.0f%%)\n", k.LoadedRuns, k.OfferedRuns, k.LoadRate*100)
	fmt.Printf("description: %s\n", k.Description)
	if k.Body != nil {
		fmt.Printf("\n%s\n", *k.Body)
	}
	if len(d.Versions) > 0 {
		fmt.Println("\nversions:")
		tw := learnTable()
		for _, v := range d.Versions {
			fmt.Fprintf(tw, "  v%d\t%s\t%s\n", v.Version, v.Actor, v.CreatedAt)
		}
		tw.Flush()
	}
	if len(d.Ledger) > 0 {
		fmt.Println("\nledger:")
		tw := learnTable()
		for _, e := range d.Ledger {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", e.CreatedAt, e.Action, e.Actor, e.Reason)
		}
		tw.Flush()
	}
	return 0
}

func printProposal(raw json.RawMessage) int {
	var d struct {
		Proposal wsapi.ProposalView `json:"proposal"`
		Merge    *struct {
			Target     wsapi.LearnedSkillView `json:"target"`
			Similarity float64                `json:"similarity"`
		} `json:"merge"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn show: decode: %v\n", err)
		return 1
	}
	p := d.Proposal
	fmt.Printf("proposal %d: %s (%s, %s)\n", p.ID, p.Name, p.Kind, p.Status)
	fmt.Printf("workspace: %d\nsource: %s\n", p.WorkspaceID, p.Source)
	if p.Note != "" {
		fmt.Printf("note: %s\n", p.Note)
	}
	if p.Malformed {
		fmt.Println("warning: the stored payload is malformed; it can only be rejected")
	}
	if p.PossibleDuplicateOf != "" {
		fmt.Printf("possible duplicate of: %s (similarity %.2f)\n", p.PossibleDuplicateOf, p.Similarity)
	}
	if d.Merge != nil {
		fmt.Printf("merge target: skill %d %s (similarity %.2f); approve with --merge-into %s --body-file F\n",
			d.Merge.Target.ID, d.Merge.Target.Name, d.Merge.Similarity, d.Merge.Target.Name)
	}
	for _, it := range p.Items {
		fmt.Printf("\n[%s] %s\n", it.Action, it.Name)
		if it.Description != "" {
			fmt.Printf("description: %s\n", it.Description)
		}
		if it.OldString != "" || it.NewString != "" {
			fmt.Printf("replace: %q\nwith:    %q\n", it.OldString, it.NewString)
		}
		if it.Reason != "" {
			fmt.Printf("reason: %s\n", it.Reason)
		}
		if it.Body != "" {
			fmt.Printf("\n%s\n", it.Body)
		}
	}
	if len(p.Evidence) > 0 {
		fmt.Println("\nevidence:")
		for _, e := range p.Evidence {
			fmt.Printf("  %s run %s #%d: %q\n", e.Kind, e.RunID, e.Seq, e.Quote)
		}
	}
	return 0
}

func learnApprove(args []string) int {
	a, err := parseLearnArgs(args, []string{"--body-file", "--desc", "--merge-into"}, []string{"--json"})
	if err != nil {
		return learnUsageErr("approve", err)
	}
	id, err := a.id(0, "<proposal-id>")
	if err != nil {
		return learnUsageErr("approve", err)
	}
	params := map[string]any{"id": id}
	if f, ok := a.vals["--body-file"]; ok {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "smind learn approve: %v\n", err)
			return 1
		}
		params["body"] = string(b)
	}
	if d, ok := a.vals["--desc"]; ok {
		params["description"] = d
	}
	if m, ok := a.vals["--merge-into"]; ok {
		params["mergeInto"] = m
	}
	var raw json.RawMessage
	if code := learnCall("approve", "learning.proposals.approve", params, &raw); code != 0 {
		return code
	}
	if a.bools["--json"] {
		printLearnJSON(raw)
		return 0
	}
	var k wsapi.LearnedSkillView
	if err := json.Unmarshal(raw, &k); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn approve: decode: %v\n", err)
		return 1
	}
	fmt.Printf("approved proposal %d: skill %d %s (v%d, %s)\n", id, k.ID, k.Name, k.Version, k.Status)
	return 0
}

func learnReject(args []string) int {
	a, err := parseLearnArgs(args, []string{"--note"}, nil)
	if err != nil {
		return learnUsageErr("reject", err)
	}
	id, err := a.id(0, "<proposal-id>")
	if err != nil {
		return learnUsageErr("reject", err)
	}
	if code := learnCall("reject", "learning.proposals.reject", map[string]any{"id": id, "note": a.vals["--note"]}, nil); code != 0 {
		return code
	}
	fmt.Printf("rejected proposal %d\n", id)
	return 0
}

var learnActionDone = map[string]string{
	"pin": "pinned", "unpin": "unpinned", "disable": "disabled", "enable": "enabled",
	"archive": "archived", "restore": "restored", "delete": "deleted",
}

func learnSkillAction(sub string, args []string) int {
	a, err := parseLearnArgs(args, nil, nil)
	if err != nil {
		return learnUsageErr(sub, err)
	}
	id, err := a.id(0, "<skill-id>")
	if err != nil {
		return learnUsageErr(sub, err)
	}
	if code := learnCall(sub, "learning.skills."+sub, map[string]any{"id": id}, nil); code != 0 {
		return code
	}
	fmt.Printf("%s skill %d\n", learnActionDone[sub], id)
	return 0
}

func learnUndo(args []string) int {
	a, err := parseLearnArgs(args, []string{"--version"}, []string{"--json"})
	if err != nil {
		return learnUsageErr("undo", err)
	}
	id, err := a.id(0, "<skill-id>")
	if err != nil {
		return learnUsageErr("undo", err)
	}
	v, err := strconv.Atoi(a.vals["--version"])
	if err != nil || v <= 0 {
		return learnUsageErr("undo", errors.New("--version N (a positive integer) is required"))
	}
	var raw json.RawMessage
	if code := learnCall("undo", "learning.skills.undo", map[string]any{"id": id, "version": v}, &raw); code != 0 {
		return code
	}
	if a.bools["--json"] {
		printLearnJSON(raw)
		return 0
	}
	var k wsapi.LearnedSkillView
	if err := json.Unmarshal(raw, &k); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn undo: decode: %v\n", err)
		return 1
	}
	fmt.Printf("skill %d restored to the text of v%d as v%d\n", id, v, k.Version)
	return 0
}

func learnReflect(args []string) int {
	a, err := parseLearnArgs(args, nil, nil)
	if err != nil {
		return learnUsageErr("reflect", err)
	}
	id, err := a.id(0, "<chat-id>")
	if err != nil {
		return learnUsageErr("reflect", err)
	}
	if code := learnCall("reflect", "learning.reflect", map[string]any{"chatId": id}, nil); code != 0 {
		return code
	}
	fmt.Printf("reflection requested for chat %d\n", id)
	return 0
}

func learnCurate(args []string) int {
	a, err := parseLearnArgs(args, []string{"--workspace"}, nil)
	if err != nil || len(a.pos) != 0 {
		if err == nil {
			err = errors.New("curate takes no arguments")
		}
		return learnUsageErr("curate", err)
	}
	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()
	ids, code := learnWorkspaces(client, "curate", a.vals["--workspace"])
	if code != 0 {
		return code
	}
	if len(ids) != 1 {
		return learnUsageErr("curate", fmt.Errorf("--workspace ID is required (%d workspaces)", len(ids)))
	}
	if code := learnCallOn(client, "curate", "learning.curate", map[string]any{"workspaceId": ids[0]}, nil); code != 0 {
		return code
	}
	fmt.Printf("curation requested for workspace %d\n", ids[0])
	return 0
}

func learnMode(args []string) int {
	a, err := parseLearnArgs(args, nil, nil)
	if err != nil || len(a.pos) != 2 {
		if err == nil {
			err = errors.New("expected <workspace-id> off|propose")
		}
		return learnUsageErr("mode", err)
	}
	id, err := parseLearnID(a.pos[0], "<workspace-id>")
	if err != nil {
		return learnUsageErr("mode", err)
	}
	mode := a.pos[1]
	if mode != "off" && mode != "propose" {
		return learnUsageErr("mode", fmt.Errorf("mode must be off or propose, got %q", mode))
	}
	if code := learnCall("mode", "learning.setMode", map[string]any{"workspaceId": id, "mode": mode}, nil); code != 0 {
		return code
	}
	fmt.Printf("workspace %d learning mode: %s\n", id, mode)
	return 0
}

func learnForget(args []string) int {
	a, err := parseLearnArgs(args, nil, []string{"--yes"})
	if err != nil {
		return learnUsageErr("forget", err)
	}
	id, err := a.id(0, "<workspace-id>")
	if err != nil {
		return learnUsageErr("forget", err)
	}
	if !a.bools["--yes"] {
		return learnUsageErr("forget", errors.New("this hard-deletes every learned skill, proposal and job of the workspace; pass --yes to confirm"))
	}
	if code := learnCall("forget", "learning.forget", map[string]any{"workspaceId": id}, nil); code != 0 {
		return code
	}
	fmt.Printf("forgot all learning data of workspace %d\n", id)
	return 0
}

func learnExport(args []string) int {
	a, err := parseLearnArgs(args, []string{"--to"}, []string{"--force", "--json"})
	if err != nil {
		return learnUsageErr("export", err)
	}
	id, err := a.id(0, "<skill-id>")
	if err != nil {
		return learnUsageErr("export", err)
	}
	to := a.vals["--to"]
	switch to {
	case "repo", "claude", "agents", "codex":
	case "":
		return learnUsageErr("export", errors.New("--to repo|claude|agents|codex is required"))
	default:
		return learnUsageErr("export", fmt.Errorf("--to must be repo, claude, agents or codex, got %q", to))
	}
	var raw json.RawMessage
	if code := learnCall("export", "learning.skills.export", map[string]any{"id": id, "target": to, "force": a.bools["--force"]}, &raw); code != 0 {
		return code
	}
	var res wsapi.ExportResultView
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn export: decode: %v\n", err)
		return 1
	}
	if a.bools["--json"] {
		printLearnJSON(raw)
	} else {
		fmt.Printf("exported skill %d to %s\n", id, res.Dir)
		for _, f := range res.Files {
			fmt.Printf("  wrote %s\n", f)
		}
	}
	if res.Status != "installed" || len(res.Skipped) > 0 {
		fmt.Fprintf(os.Stderr, "smind learn export: left %d file(s) alone because they differ from what smind would write (edited, or not written by smind): %s\nrerun with --force to overwrite them\n",
			len(res.Skipped), strings.Join(res.Skipped, ", "))
		return 1
	}
	return 0
}
