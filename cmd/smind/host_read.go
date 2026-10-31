package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/spacingmind/smind/internal/wsclient"
)

// hostReadResult is the envelope of the daemon's remote.read answer.
type hostReadResult struct {
	Host          string          `json:"host"`
	HostID        string          `json:"hostId"`
	Op            string          `json:"op"`
	Data          json.RawMessage `json:"data"`
	StatusUnknown bool            `json:"statusUnknown"`
	Failures      int             `json:"failures"`
}

type hostReadTask struct {
	ID          int    `json:"id"`
	WorkspaceID int    `json:"workspaceId"`
	Title       string `json:"title"`
	Branch      string `json:"branch"`
	State       string `json:"state"`
	ParentID    int    `json:"parentId"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Host        string `json:"host"`
	GlobalID    string `json:"globalId"`
	Origin      *struct {
		Host  string `json:"host"`
		Trace string `json:"trace"`
	} `json:"origin"`
}

// hostReadErr prints a typed refusal as `smind host <verb>: <code>: <message>`.
func hostReadErr(stderr io.Writer, verb string, err error) {
	var rpc *wsclient.RPCError
	if errors.As(err, &rpc) {
		fmt.Fprintf(stderr, "smind host %s: %s: %s\n", verb, rpc.Code, rpc.Message)
		return
	}
	fmt.Fprintf(stderr, "smind host %s: %v\n", verb, err)
}

// hostSafeText makes text from a remote host safe to print on a terminal:
// control runes (ESC, C1, newlines, tabs) become one space and bidi controls
// are dropped, so the host cannot repaint the screen or forge extra lines.
func hostSafeText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return ' '
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, s)
}

// parseHostGlobalID splits "<alias>:<id>" at the first colon.
func parseHostGlobalID(s string) (alias string, id int, ok bool) {
	alias, num, found := strings.Cut(s, ":")
	if !found || alias == "" {
		return "", 0, false
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return alias, n, true
}

// hostReadCall sends one remote.read and returns the envelope plus its raw
// JSON. code is 0 on success, else the exit code after printing.
func hostReadCall(ctx context.Context, verb string, params map[string]any, stderr io.Writer) (res hostReadResult, raw json.RawMessage, code int) {
	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return res, nil, 1
	}
	defer closeFn()
	if err := call(ctx, "remote.read", params, &raw); err != nil {
		hostReadErr(stderr, verb, err)
		return res, nil, 1
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind host %s: bad answer: %v\n", verb, err)
		return res, nil, 1
	}
	return res, raw, 0
}

// hostReadFlags parses `<positional>... [flags]` for a read verb.
type hostReadFlags struct {
	positional []string
	json       bool
	workspace  int
	cursor     int
	limit      int
	run        string
}

func parseHostReadFlags(verb string, args []string, stderr io.Writer, withWorkspace, withPage, withRun bool) (hostReadFlags, int, bool) {
	var f hostReadFlags
	positional, rest := hostSplitArgs(args)
	f.positional = positional
	fs := flag.NewFlagSet("host "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&f.json, "json", false, "print the result as JSON")
	if withWorkspace {
		fs.IntVar(&f.workspace, "workspace", 0, "only this workspace id")
	}
	if withPage {
		fs.IntVar(&f.cursor, "cursor", 0, "continue from this cursor")
		fs.IntVar(&f.limit, "limit", 0, "maximum rows")
	}
	if withRun {
		fs.StringVar(&f.run, "run", "", "run id (default: the task's latest run)")
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, 0, false
		}
		return f, 2, false
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, hostUsage)
		return f, 2, false
	}
	return f, 0, true
}

func runHostRead(ctx context.Context, verb string, args []string, stdout, stderr io.Writer) int {
	var (
		f    hostReadFlags
		code int
		ok   bool
	)
	switch verb {
	case "info", "workspaces":
		f, code, ok = parseHostReadFlags(verb, args, stderr, false, false, false)
	case "tasks":
		f, code, ok = parseHostReadFlags(verb, args, stderr, true, true, false)
	case "task":
		f, code, ok = parseHostReadFlags(verb, args, stderr, false, false, false)
	case "status":
		f, code, ok = parseHostReadFlags(verb, args, stderr, false, false, false)
	case "activity":
		f, code, ok = parseHostReadFlags(verb, args, stderr, false, true, true)
	}
	if !ok {
		return code
	}

	params := map[string]any{"op": verb}
	usage := func() int {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	pos := f.positional
	switch verb {
	case "info", "workspaces", "tasks":
		if len(pos) != 1 || pos[0] == "" {
			return usage()
		}
		params["host"] = pos[0]
		if f.workspace != 0 {
			params["workspaceId"] = f.workspace
		}
	case "task":
		if len(pos) != 1 {
			return usage()
		}
		alias, id, valid := parseHostGlobalID(pos[0])
		if !valid {
			return usage()
		}
		params["host"], params["id"] = alias, id
	case "status":
		switch {
		case len(pos) == 1 && strings.Contains(pos[0], ":"):
			alias, id, valid := parseHostGlobalID(pos[0])
			if !valid {
				return usage()
			}
			params["host"], params["taskId"] = alias, id
		case len(pos) == 2 && pos[0] != "" && pos[1] != "":
			params["host"], params["runId"] = pos[0], pos[1]
		default:
			return usage()
		}
	case "activity":
		if len(pos) != 1 {
			return usage()
		}
		alias, id, valid := parseHostGlobalID(pos[0])
		if !valid {
			return usage()
		}
		params["host"], params["taskId"] = alias, id
		if f.run != "" {
			params["runId"] = f.run
		}
	}
	if f.cursor != 0 {
		params["cursor"] = f.cursor
	}
	if f.limit != 0 {
		params["limit"] = f.limit
	}

	res, raw, rc := hostReadCall(ctx, verb, params, stderr)
	if rc != 0 {
		return rc
	}
	if f.json {
		var obj any
		_ = json.Unmarshal(raw, &obj)
		if err := json.NewEncoder(stdout).Encode(obj); err != nil {
			fmt.Fprintf(stderr, "smind host %s: %v\n", verb, err)
			return 1
		}
	}
	if verb == "status" && res.StatusUnknown {
		fmt.Fprintf(stderr, "smind host status: status unknown after %d failed observations (the run may still be running); inspect the host before re-delegating\n", res.Failures)
		return 1
	}
	if f.json {
		return 0
	}
	if err := renderHostRead(stdout, verb, res); err != nil {
		fmt.Fprintf(stderr, "smind host %s: bad answer: %v\n", verb, err)
		return 1
	}
	return 0
}

func renderHostRead(w io.Writer, verb string, res hostReadResult) error {
	switch verb {
	case "info":
		var d struct {
			HostID string   `json:"hostId"`
			Ver    string   `json:"version"`
			FleetV int      `json:"fleetV"`
			Caps   []string `json:"caps"`
			OS     string   `json:"os"`
			Arch   string   `json:"arch"`
			Limits struct {
				Ops           []string `json:"ops"`
				MaxLiveAgents int      `json:"maxLiveAgents"`
				MaxWall       int      `json:"maxWallSeconds"`
				MaxCostUsd    float64  `json:"maxCostUsd"`
			} `json:"limits"`
		}
		if err := json.Unmarshal(res.Data, &d); err != nil {
			return err
		}
		fmt.Fprintf(w, "hostId: %s\nversion: %s\nfleetV: %d\ncaps: %s\nos: %s/%s\nops: %s\nmaxLiveAgents: %d\nmaxWallSeconds: %d\nmaxCostUsd: %g\n",
			hostSafeText(d.HostID), hostSafeText(d.Ver), d.FleetV, hostSafeText(strings.Join(d.Caps, ",")), hostSafeText(d.OS), hostSafeText(d.Arch), hostSafeText(strings.Join(d.Limits.Ops, ",")),
			d.Limits.MaxLiveAgents, d.Limits.MaxWall, d.Limits.MaxCostUsd)
	case "workspaces":
		var d struct {
			Workspaces []struct {
				ID            int    `json:"id"`
				Title         string `json:"title"`
				RepoURL       string `json:"repoUrl"`
				DefaultBranch string `json:"defaultBranch"`
			} `json:"workspaces"`
		}
		if err := json.Unmarshal(res.Data, &d); err != nil {
			return err
		}
		if len(d.Workspaces) == 0 {
			fmt.Fprintln(w, "(no workspaces)")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tTITLE\tREPO\tBRANCH")
		for _, ws := range d.Workspaces {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", ws.ID, hostSafeText(ws.Title), hostSafeText(ws.RepoURL), hostSafeText(ws.DefaultBranch))
		}
		_ = tw.Flush()
	case "tasks":
		var d struct {
			Tasks      []hostReadTask `json:"tasks"`
			NextCursor int            `json:"nextCursor"`
		}
		if err := json.Unmarshal(res.Data, &d); err != nil {
			return err
		}
		if len(d.Tasks) == 0 {
			fmt.Fprintln(w, "(no tasks)")
		} else {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "GLOBAL ID\tSTATE\tBRANCH\tTITLE")
			for _, t := range d.Tasks {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", hostSafeText(t.GlobalID), hostSafeText(t.State), hostSafeText(t.Branch), hostSafeText(t.Title))
			}
			_ = tw.Flush()
		}
		if d.NextCursor != 0 {
			fmt.Fprintf(w, "more: --cursor %d\n", d.NextCursor)
		}
	case "task":
		var t hostReadTask
		if err := json.Unmarshal(res.Data, &t); err != nil {
			return err
		}
		fmt.Fprintf(w, "globalId: %s\nhost: %s\nid: %d\nworkspaceId: %d\ntitle: %s\nbranch: %s\nstate: %s\n", hostSafeText(t.GlobalID), hostSafeText(t.Host), t.ID, t.WorkspaceID, hostSafeText(t.Title), hostSafeText(t.Branch), hostSafeText(t.State))
		if t.ParentID != 0 {
			fmt.Fprintf(w, "parentId: %d\n", t.ParentID)
		}
		if t.Origin != nil {
			fmt.Fprintf(w, "origin: %s (%s)\n", hostSafeText(t.Origin.Host), hostSafeText(t.Origin.Trace))
		}
		fmt.Fprintf(w, "createdAt: %s\nupdatedAt: %s\n", hostSafeText(t.CreatedAt), hostSafeText(t.UpdatedAt))
	case "status":
		var d struct {
			RunID      string   `json:"runId"`
			TaskID     int      `json:"taskId"`
			State      string   `json:"state"`
			Provider   string   `json:"provider"`
			Model      string   `json:"model"`
			StartedAt  string   `json:"startedAt"`
			FinishedAt string   `json:"finishedAt"`
			StopReason string   `json:"stopReason"`
			Failure    string   `json:"failureKind"`
			CostUsd    *float64 `json:"costUsd"`
		}
		if err := json.Unmarshal(res.Data, &d); err != nil {
			return err
		}
		fmt.Fprintf(w, "runId: %s\ntaskId: %d\nstate: %s\nprovider: %s\n", hostSafeText(d.RunID), d.TaskID, hostSafeText(d.State), hostSafeText(d.Provider))
		for _, kv := range [][2]string{{"model", d.Model}, {"startedAt", d.StartedAt}, {"finishedAt", d.FinishedAt}, {"stopReason", d.StopReason}, {"failureKind", d.Failure}} {
			if kv[1] != "" {
				fmt.Fprintf(w, "%s: %s\n", kv[0], hostSafeText(kv[1]))
			}
		}
		cost := "unknown"
		if d.CostUsd != nil {
			cost = strconv.FormatFloat(*d.CostUsd, 'f', -1, 64)
		}
		fmt.Fprintf(w, "costUsd: %s\n", cost)
	case "activity":
		var d struct {
			Events []struct {
				Seq  int    `json:"seq"`
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"events"`
			NextCursor int  `json:"nextCursor"`
			HasMore    bool `json:"hasMore"`
		}
		if err := json.Unmarshal(res.Data, &d); err != nil {
			return err
		}
		for _, e := range d.Events {
			fmt.Fprintf(w, "%d %s %s\n", e.Seq, hostSafeText(e.Type), hostSafeText(e.Text))
		}
		if d.HasMore {
			fmt.Fprintf(w, "more: --cursor %d\n", d.NextCursor)
		}
	}
	return nil
}
