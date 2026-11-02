package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// `smind permission` is the human side of the heredoc reveal
// (docs/plans/active/permission-heredoc-reveal.md): permission cards and
// tool calls show a heredoc body elided, and `show --reveal` prints one body,
// passed through the daemon's redactor. Humans only; no MCP tool mirrors it.

const cmdPermissionUsage = `usage: smind permission show --reveal [--index N] [--json] <requestId>
       smind permission show --reveal [--index N] [--json] --run <runId> --call <toolCallId>
  prints the redacted heredoc body N (default 0) elided from a still-pending
  permission request, or from a recorded tool call (live or finished run).
  "smind task permissions <runId>" lists pending requests and their heredocs.`

func cmdPermission(args []string) int {
	return runPermission(context.Background(), args, osStdout, osStderr)
}

// runPermission is cmdPermission with its environment injected. Exit codes:
// 0 ok, 1 a daemon or call error, 2 usage.
func runPermission(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "show" {
		fmt.Fprintln(stderr, cmdPermissionUsage)
		return 2
	}
	a, err := parseLearnArgs(args[1:], []string{"--index", "--run", "--call"}, []string{"--reveal", "--json"})
	if err != nil {
		fmt.Fprintf(stderr, "smind permission show: %v\n%s\n", err, cmdPermissionUsage)
		return 2
	}
	index := 0
	if v, ok := a.vals["--index"]; ok {
		if index, err = strconv.Atoi(v); err != nil || index < 0 {
			fmt.Fprintf(stderr, "smind permission show: --index must be a non-negative number\n")
			return 2
		}
	}
	method, params := "permission.revealHeredoc", map[string]any{"index": index}
	switch {
	case !a.bools["--reveal"]:
		fmt.Fprintf(stderr, "smind permission show: only --reveal is supported\n%s\n", cmdPermissionUsage)
		return 2
	case a.vals["--run"] != "" || a.vals["--call"] != "":
		if a.vals["--run"] == "" || a.vals["--call"] == "" || len(a.pos) != 0 {
			fmt.Fprintf(stderr, "smind permission show: --run and --call go together and replace <requestId>\n%s\n", cmdPermissionUsage)
			return 2
		}
		method = "run.revealToolHeredoc"
		params["runId"], params["toolCallId"] = a.vals["--run"], a.vals["--call"]
	case len(a.pos) == 1:
		params["permissionId"] = a.pos[0]
	default:
		fmt.Fprintf(stderr, "smind permission show: give a request id, or --run and --call\n%s\n", cmdPermissionUsage)
		return 2
	}

	call, closeFn, err := policyDial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer closeFn()
	var raw json.RawMessage
	if err := call(ctx, method, params, &raw); err != nil {
		fmt.Fprintf(stderr, "smind permission show: %v\n", err)
		return 1
	}
	if a.bools["--json"] {
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") != nil {
			buf.Reset()
			buf.Write(raw)
		}
		fmt.Fprintln(stdout, buf.String())
		return 0
	}
	var res struct {
		Bytes     int    `json:"bytes"`
		Text      string `json:"text"`
		Redacted  bool   `json:"redacted"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind permission show: %v\n", errors.New("decode response: "+err.Error()))
		return 1
	}
	fmt.Fprint(stdout, sanitizeForTerminal(res.Text))
	if res.Redacted {
		fmt.Fprintln(stderr, "\n(secrets in this body were redacted)")
	}
	if res.Truncated {
		fmt.Fprintf(stderr, "\n(truncated; the body is %d bytes)\n", res.Bytes)
	}
	return 0
}
