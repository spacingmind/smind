package main

// task_mode.go -- `smind task mode`, the human-only way to change a live run's
// permission mode or flip its auto-accept (orchestration-parity D12). It is the
// fix for the "43 approvals" permission wall: a human who has watched a run and
// trusts it can stop the prompts without restarting the run.
//
// Who may do this is ADR-0019 decision 6: only a human. The daemon enforces it
// per request, from the `source` the caller declares on run.setPermissionMode
// ("human" | "orchestrator" | "agent"); any other source is refused an
// auto-approving mode or autoAccept=true. This command is the human surface, so
// it declares source "human" explicitly (not by default). The MCP server is the
// orchestrator surface: its task_set_mode hardcodes source "orchestrator" and
// has no autoAccept or source input, and no other MCP tool reaches this RPC.
//
// What that does and does not prove: the source is a declaration by the
// surface, not a credential. Every client holds the same daemon token, so a
// process that can run commands as the user can run this CLI, exactly as it can
// run `smind task approve` today; against such a process the boundary is the
// provider's own permission mode or sandbox (ADR-0019), not this command. What
// the command adds is a speed bump on the escalations: turning auto-accept ON,
// or picking a mode that approves tool calls itself, must be confirmed by
// typing "yes" at a terminal, and the command refuses to run without one
// (there is deliberately no --yes flag). A shell tool that gives its commands
// no terminal cannot pass it; one that wraps them in a pseudo-terminal could,
// which is why this is a speed bump and not a boundary.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// daemonCaller is the part of *wsclient.Client `task mode` uses, so a test can
// see exactly what is sent.
type daemonCaller interface {
	Call(ctx context.Context, method string, params, result any) error
	Close() error
}

func dialDaemonCaller(ctx context.Context) (daemonCaller, error) {
	c, err := dialDaemon(ctx)
	if err != nil {
		return nil, err
	}
	return c, nil
}

const cmdTaskModeUsage = `usage: smind task mode <runId> [--auto-accept on|off] [<mode>]

Changes a live run's permission settings. For humans: it can turn on
auto-accept (the run then approves every tool call itself) or pick an
auto-approving mode, which the MCP tools and agents are refused.

  --auto-accept on|off   approve every permission prompt automatically (ACP agents)
  <mode>                 a permission mode the provider advertises (see "smind task options"
                         or the provider's modes), e.g. acceptEdits, plan

Turning auto-accept on, or picking a mode that approves tool calls itself,
asks you to type "yes" and needs a terminal.`

// taskModeRequest is what `task mode` was asked to do.
type taskModeRequest struct {
	RunID      string
	ModeID     string
	AutoAccept *bool
}

// parseTaskMode reads `<runId> [--auto-accept on|off] [<mode>]`, flags in any
// position. At least one of mode and --auto-accept is required.
func parseTaskMode(args []string) (taskModeRequest, error) {
	var req taskModeRequest
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, isFlag := "", false
		switch {
		case a == "--auto-accept":
			isFlag = true
			if i+1 >= len(args) {
				return req, fmt.Errorf("--auto-accept needs on or off")
			}
			i++
			val = args[i]
		case strings.HasPrefix(a, "--auto-accept="):
			isFlag, val = true, strings.TrimPrefix(a, "--auto-accept=")
		case strings.HasPrefix(a, "-"):
			return req, fmt.Errorf("unknown flag %q", a)
		}
		if !isFlag {
			positional = append(positional, a)
			continue
		}
		if req.AutoAccept != nil {
			return req, fmt.Errorf("--auto-accept given twice")
		}
		var on bool
		switch strings.ToLower(val) {
		case "on":
			on = true
		case "off":
		default:
			return req, fmt.Errorf("--auto-accept must be on or off, not %q", val)
		}
		req.AutoAccept = &on
	}
	if len(positional) == 0 || len(positional) > 2 {
		return req, fmt.Errorf("expected a run id and at most one mode")
	}
	req.RunID = positional[0]
	if len(positional) == 2 {
		req.ModeID = positional[1]
	}
	if req.ModeID == "" && req.AutoAccept == nil {
		return req, fmt.Errorf("give a mode, --auto-accept on|off, or both")
	}
	return req, nil
}

func cmdTaskMode(args []string) int {
	interactive := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	return runTaskMode(args, os.Stdin, os.Stdout, os.Stderr, interactive, dialDaemonCaller)
}

// runTaskMode is cmdTaskMode with its environment injected. interactive says
// whether stdin is a terminal; dial opens the daemon connection.
//
// Two things need a person to type "yes": turning auto-accept on, and picking
// a mode that approves tool calls itself (bypassPermissions, full-access, ...).
// The command does not keep its own list of such modes. It asks the daemon,
// which owns that knowledge: the mode is first requested as the *orchestrator*
// source, which the daemon refuses for any auto-approving mode. If that
// succeeds the mode is safe and already applied; if it fails for any reason
// the mode is treated as auto-approving (fail closed) and confirmed before the
// human-source call.
func runTaskMode(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool, dial func(context.Context) (daemonCaller, error)) int {
	req, err := parseTaskMode(args)
	if err != nil {
		fmt.Fprintf(stderr, "task mode: %v\n%s\n", err, cmdTaskModeUsage)
		return 2
	}

	// Cheap refusal before touching the daemon: auto-accept needs a terminal.
	autoAcceptOn := req.AutoAccept != nil && *req.AutoAccept
	if autoAcceptOn && !interactive {
		fmt.Fprintln(stderr, refuseNoTerminal("turning auto-accept on"))
		return 1
	}

	client, err := dial(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer client.Close()

	var what string
	if autoAcceptOn {
		what = fmt.Sprintf("Auto-accept ON makes run %s approve every tool call without asking you.", req.RunID)
	}
	if req.ModeID != "" {
		var probe permissionState
		probeParams := map[string]any{"runId": req.RunID, "modeId": req.ModeID, "source": "orchestrator"}
		if err := client.Call(context.Background(), "run.setPermissionMode", probeParams, &probe); err != nil && what == "" {
			// Refused as the orchestrator (an auto-approving mode), or could not
			// be checked: confirm before asking again as the human.
			if !interactive {
				fmt.Fprintln(stderr, refuseNoTerminal(fmt.Sprintf("picking mode %q, which may approve tool calls itself (or could not be checked: %v),", req.ModeID, err)))
				return 1
			}
			what = fmt.Sprintf("Mode %q may let run %s approve tool calls without asking you (the daemon would not allow it for an agent: %v).", req.ModeID, req.RunID, err)
		}
	}
	if what != "" {
		fmt.Fprintf(stdout, "%s\nType \"yes\" to continue: ", what)
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			fmt.Fprintln(stderr, "\ntask mode: not confirmed; nothing more was changed.")
			return 1
		}
	}

	// Source is declared explicitly: this is the human surface (see the file
	// comment), and the daemon's default must not be what decides it.
	params := map[string]any{"runId": req.RunID, "source": "human"}
	if req.ModeID != "" {
		params["modeId"] = req.ModeID
	}
	if req.AutoAccept != nil {
		params["autoAccept"] = *req.AutoAccept
	}
	var result permissionState
	if err := client.Call(context.Background(), "run.setPermissionMode", params, &result); err != nil {
		fmt.Fprintf(stderr, "task mode: %v\n", err)
		return 1
	}
	auto := "off"
	if result.AutoAccept {
		auto = "on"
	}
	fmt.Fprintf(stdout, "run %s: permission mode %q, auto-accept %s\n", req.RunID, result.PermissionMode, auto)
	return 0
}

// permissionState is run.setPermissionMode's result.
type permissionState struct {
	PermissionMode string `json:"permissionMode"`
	AutoAccept     bool   `json:"autoAccept"`
}

func refuseNoTerminal(action string) string {
	return "task mode: " + action + " has to be confirmed by a person at a terminal, and stdin is not one.\n" +
		"This command is for humans; agents cannot do this (the MCP server has no such tool and the daemon refuses it for any non-human caller)."
}
