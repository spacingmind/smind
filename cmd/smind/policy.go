package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/permpolicy"
)

// `smind policy` is the human side of the permission policy engine
// (ADR-0047 §2): init writes the commented template, show/test/audit read
// the engine over the policy.* RPCs. Nothing here edits a policy or the
// audit, and no MCP tool mirrors it.

const cmdPolicyUsage = `usage: smind policy <subcommand> [--json]
  init                                       write the commented policy template (never overwrites)
  show                                       engine switch, policy file state and rules
  test [--tool T] [--seat S] [--task ID] "<command>"
                                             explain what the engine would decide, per segment (no side effects)
  audit [--task ID] [--decision D] [--limit N]
                                             recent engine decisions, newest first`

// policyDial is a variable so tests can stub the daemon connection.
var policyDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

func cmdPolicy(args []string) int {
	return runPolicy(context.Background(), args, osStdout, osStderr)
}

// runPolicy is cmdPolicy with its environment injected. Exit codes: 0 ok,
// 1 a daemon, call or file error or an unsupported OS, 2 usage.
func runPolicy(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, cmdPolicyUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init", "show", "test", "audit":
		if !permpolicy.Supported() {
			fmt.Fprintf(stderr, "smind policy %s: %s\n", sub, permpolicy.UnsupportedMessage)
			return 1
		}
	}
	switch sub {
	case "init":
		return policyInit(rest, stdout, stderr)
	case "show":
		return policyShow(ctx, rest, stdout, stderr)
	case "test":
		return policyTest(ctx, rest, stdout, stderr)
	case "audit":
		return policyAudit(ctx, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "smind policy: unknown subcommand %q\n%s\n", sub, cmdPolicyUsage)
		return 2
	}
}

func policyUsageErr(stderr io.Writer, sub string, err error) int {
	fmt.Fprintf(stderr, "smind policy %s: %v\n%s\n", sub, err, cmdPolicyUsage)
	return 2
}

// policyInit writes the template with O_EXCL, so an existing file (which may
// hold the human's rules) is never touched, and never contacts the daemon.
func policyInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return policyUsageErr(stderr, "init", errors.New("takes no arguments"))
	}
	dir := filepath.Join(config.Dir(), "steward")
	path := filepath.Join(dir, "policy.yaml")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(stderr, "smind policy init: %v\n", err)
		return 1
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		fmt.Fprintf(stderr, "policy file already exists: %s\n", path)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "smind policy init: %v\n", err)
		return 1
	}
	_, werr := f.WriteString(permpolicy.Template)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		fmt.Fprintf(stderr, "smind policy init: write %s: %v\n", path, werr)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

// policyCall dials the daemon and makes one call, printing the raw result
// when asJSON is set; ok is false when the caller should return code.
func policyCall(ctx context.Context, sub, method string, params, out any, asJSON bool, stdout, stderr io.Writer) (code int, ok bool) {
	call, closeFn, err := policyDial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1, false
	}
	defer closeFn()
	var raw json.RawMessage
	if err := call(ctx, method, params, &raw); err != nil {
		fmt.Fprintf(stderr, "smind policy %s: %v\n", sub, err)
		return 1, false
	}
	if asJSON {
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") != nil {
			buf.Reset()
			buf.Write(raw)
		}
		fmt.Fprintln(stdout, buf.String())
		return 0, false
	}
	if err := json.Unmarshal(raw, out); err != nil {
		fmt.Fprintf(stderr, "smind policy %s: decode response: %v\n", sub, err)
		return 1, false
	}
	return 0, true
}

func policyShow(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a, err := parseLearnArgs(args, nil, []string{"--json"})
	if err != nil || len(a.pos) != 0 {
		if err == nil {
			err = errors.New("takes no arguments")
		}
		return policyUsageErr(stderr, "show", err)
	}
	var res struct {
		Enabled bool   `json:"enabled"`
		Valid   bool   `json:"valid"`
		Exists  bool   `json:"exists"`
		Error   string `json:"error"`
		Path    string `json:"path"`
		Sha     string `json:"sha"`
		Rules   []struct {
			ID, Action, Why string
		} `json:"rules"`
	}
	if code, ok := policyCall(ctx, "show", "policy.get", map[string]any{}, &res, a.bools["--json"], stdout, stderr); !ok {
		return code
	}
	state := "valid"
	switch {
	case !res.Exists:
		state = "no file (run: smind policy init)"
	case !res.Valid:
		state = "INVALID: " + res.Error + " (floor + ask only)"
	}
	on := "off"
	if res.Enabled {
		on = "on"
	}
	fmt.Fprintf(stdout, "engine:  %s\npolicy:  %s\nsha:     %s\npath:    %s\nrules:   %d\n", on, state, res.Sha, res.Path, len(res.Rules))
	if len(res.Rules) > 0 {
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tACTION\tWHY")
		for _, r := range res.Rules {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", r.ID, r.Action, r.Why)
		}
		_ = tw.Flush()
	}
	return 0
}

func policyTest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a, err := parseLearnArgs(args, []string{"--tool", "--seat", "--task"}, []string{"--json"})
	if err != nil {
		return policyUsageErr(stderr, "test", err)
	}
	if len(a.pos) != 1 || strings.TrimSpace(a.pos[0]) == "" {
		return policyUsageErr(stderr, "test", errors.New(`expected exactly one "<command>" argument`))
	}
	params := map[string]any{"tool": "Bash", "command": a.pos[0]}
	if v := a.vals["--tool"]; v != "" {
		params["tool"] = v
	}
	if v := a.vals["--seat"]; v != "" {
		params["seat"] = v
	}
	if v := a.vals["--task"]; v != "" {
		id, err := parseLearnID(v, "--task")
		if err != nil {
			return policyUsageErr(stderr, "test", err)
		}
		params["taskId"] = id
	}
	var res struct {
		Decision struct {
			Action   string   `json:"action"`
			RuleID   string   `json:"ruleId"`
			FloorID  string   `json:"floorId"`
			Why      string   `json:"why"`
			Reasons  []string `json:"reasons"`
			Segments []struct {
				Index   int    `json:"index"`
				Text    string `json:"text"`
				Action  string `json:"action"`
				RuleID  string `json:"ruleId"`
				FloorID string `json:"floorId"`
			} `json:"segments"`
		} `json:"decision"`
		Status struct {
			Enabled bool `json:"enabled"`
		} `json:"status"`
	}
	if code, ok := policyCall(ctx, "test", "policy.test", params, &res, a.bools["--json"], stdout, stderr); !ok {
		return code
	}
	d := res.Decision
	by := "no rule"
	switch {
	case d.FloorID != "":
		by = "floor " + d.FloorID
	case d.RuleID != "":
		by = "rule " + d.RuleID
	}
	fmt.Fprintf(stdout, "%s (%s)\n", d.Action, by)
	if d.Why != "" {
		fmt.Fprintf(stdout, "why: %s\n", d.Why)
	}
	for _, r := range d.Reasons {
		fmt.Fprintf(stdout, "reason: %s\n", r)
	}
	for _, s := range d.Segments {
		id := s.RuleID
		if s.FloorID != "" {
			id = "floor " + s.FloorID
		}
		fmt.Fprintf(stdout, "  [%d] %-5s %-24s %s\n", s.Index, s.Action, id, s.Text)
	}
	if !res.Status.Enabled {
		fmt.Fprintln(stdout, "note: the engine is switched off; this is a dry run of what it would decide")
	}
	return 0
}

func policyAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a, err := parseLearnArgs(args, []string{"--task", "--decision", "--limit"}, []string{"--json"})
	if err != nil || len(a.pos) != 0 {
		if err == nil {
			err = errors.New("takes no arguments")
		}
		return policyUsageErr(stderr, "audit", err)
	}
	params := map[string]any{}
	if v := a.vals["--task"]; v != "" {
		id, err := parseLearnID(v, "--task")
		if err != nil {
			return policyUsageErr(stderr, "audit", err)
		}
		params["taskId"] = id
	}
	if v := a.vals["--decision"]; v != "" {
		params["decision"] = v
	}
	if v := a.vals["--limit"]; v != "" {
		n, err := parseLearnID(v, "--limit")
		if err != nil {
			return policyUsageErr(stderr, "audit", err)
		}
		params["limit"] = n
	}
	var res struct {
		Rows []struct {
			CreatedAt string `json:"createdAt"`
			Decision  string `json:"decision"`
			RuleID    string `json:"ruleId"`
			Seat      string `json:"seat"`
			Tool      string `json:"tool"`
			Target    string `json:"target"`
		} `json:"rows"`
	}
	if code, ok := policyCall(ctx, "audit", "policy.audit", params, &res, a.bools["--json"], stdout, stderr); !ok {
		return code
	}
	if len(res.Rows) == 0 {
		fmt.Fprintln(stdout, "no audit rows")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tDECISION\tRULE\tSEAT\tTOOL\tTARGET")
	for _, r := range res.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.CreatedAt, r.Decision, r.RuleID, r.Seat, r.Tool, r.Target)
	}
	_ = tw.Flush()
	return 0
}
