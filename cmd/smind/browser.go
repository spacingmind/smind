package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// cmdBrowserUsage is printed on any argument error in cmdBrowser.
const cmdBrowserUsage = "usage: smind browser <status|enable|disable|allow add <host>|allow rm <host>|clear> --workspace <id> [--json]\n(human-only; as human-only as the local principal: an unidentified local connection counts as the human, ADR-0056)"

// browserSettings is the shared result of browser.settings.get and .set.
type browserSettings struct {
	WorkspaceID     int64    `json:"workspaceId"`
	Enabled         bool     `json:"enabled"`
	AllowHosts      []string `json:"allowHosts"`
	ProfileMode     string   `json:"profileMode"`
	KillSwitch      bool     `json:"killSwitch"`
	Available       bool     `json:"available"`
	ChromeInstalled bool     `json:"chromeInstalled"`
	InstallStep     string   `json:"installStep"`
	UpdatedAt       string   `json:"updatedAt"`
	UpdatedBy       string   `json:"updatedBy"`
}

func cmdBrowser(args []string) int {
	return runBrowser(context.Background(), args, osStdout, osStderr)
}

// runBrowser is cmdBrowser with its environment injected. Exit codes: 0 ok,
// 1 a daemon or call error, 2 usage.
func runBrowser(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if inAgentRun() {
		fmt.Fprintln(stderr, "smind browser: this command is for the person, not for an agent run")
		return 1
	}
	var verb string
	if len(args) > 0 {
		verb, args = args[0], args[1:]
	}
	allowVerb := ""
	switch verb {
	case "":
		fmt.Fprintln(stderr, cmdBrowserUsage)
		return 2
	case "status", "enable", "disable", "clear":
	case "allow":
		if len(args) == 0 || (args[0] != "add" && args[0] != "rm") {
			fmt.Fprintf(stderr, "smind browser allow: give add or rm and a host\n%s\n", cmdBrowserUsage)
			return 2
		}
		allowVerb, args = args[0], args[1:]
	default:
		fmt.Fprintf(stderr, "smind browser: unknown verb %q\n%s\n", verb, cmdBrowserUsage)
		return 2
	}
	a, err := parseLearnArgs(args, []string{"--workspace"}, []string{"--json"})
	if err != nil {
		fmt.Fprintf(stderr, "smind browser %s: %v\n%s\n", verb, err, cmdBrowserUsage)
		return 2
	}
	if verb == "allow" && len(a.pos) != 1 {
		fmt.Fprintf(stderr, "smind browser allow %s: give one host\n%s\n", allowVerb, cmdBrowserUsage)
		return 2
	}
	if verb != "allow" && len(a.pos) != 0 {
		fmt.Fprintf(stderr, "smind browser %s: unexpected %q\n%s\n", verb, a.pos[0], cmdBrowserUsage)
		return 2
	}
	if a.vals["--workspace"] == "" {
		fmt.Fprintf(stderr, "smind browser %s: --workspace is required (the workspace id)\n%s\n", verb, cmdBrowserUsage)
		return 2
	}
	id, err := strconv.ParseInt(a.vals["--workspace"], 10, 64)
	if err != nil || id <= 0 {
		fmt.Fprintf(stderr, "smind browser %s: --workspace must be a workspace id\n%s\n", verb, cmdBrowserUsage)
		return 2
	}

	call, closeFn, err := policyDial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer closeFn()

	switch verb {
	case "status":
		var res browserSettings
		if code, ok := browserCall(ctx, call, verb, "browser.settings.get", map[string]any{"workspaceId": id}, &res, a.bools["--json"], stdout, stderr); !ok {
			return code
		}
		if !a.bools["--json"] {
			printBrowserStatus(stdout, &res)
		}
		return 0
	case "enable", "disable":
		params := map[string]any{"workspaceId": id, "enabled": verb == "enable"}
		var res browserSettings
		if code, ok := browserCall(ctx, call, verb, "browser.settings.set", params, &res, a.bools["--json"], stdout, stderr); !ok {
			return code
		}
		if !a.bools["--json"] {
			fmt.Fprintf(stdout, "browser %s for workspace %d\n", browserOnOff(verb == "enable"), id)
		}
		if verb == "enable" {
			if !res.Available {
				fmt.Fprintln(stdout, "The browser tools are not yet offered to agents in this version; the setting is stored.")
			}
			fmt.Fprintln(stdout, "The switch is as human-only as the daemon's local principal: an unidentified local connection counts as the human (ADR-0056).")
		}
		return 0
	case "allow":
		// The daemon applies the change atomically under its own lock (no client-side
		// read-modify-write: two commands at once must not drop a host) and matches
		// hosts by their canonical form (case, trailing dot).
		param := "allowAdd"
		if allowVerb == "rm" {
			param = "allowRemove"
		}
		var res browserSettings
		if code, ok := browserCall(ctx, call, "allow "+allowVerb, "browser.settings.set", map[string]any{"workspaceId": id, param: []string{a.pos[0]}}, &res, a.bools["--json"], stdout, stderr); !ok {
			return code
		}
		if !a.bools["--json"] {
			fmt.Fprintf(stdout, "allowed hosts: %s\n", browserHostsLabel(&res))
		}
		return 0
	default: // clear
		var res struct{}
		if code, ok := browserCall(ctx, call, verb, "browser.data.clear", map[string]any{"workspaceId": id}, &res, a.bools["--json"], stdout, stderr); !ok {
			return code
		}
		if !a.bools["--json"] {
			fmt.Fprintf(stdout, "browser session ended and its data removed for workspace %d\n", id)
		}
		return 0
	}
}

// browserCall makes one browser RPC, printing the raw result indented when
// asJSON is set; ok is false when the caller should return code.
func browserCall(ctx context.Context, call wsCaller, verb, method string, params map[string]any, out any, asJSON bool, stdout, stderr io.Writer) (int, bool) {
	var raw json.RawMessage
	if err := call(ctx, method, params, &raw); err != nil {
		fmt.Fprintf(stderr, "smind browser %s: %v\n", verb, err)
		return 1, false
	}
	if asJSON {
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") != nil {
			buf.Reset()
			buf.Write(raw)
		}
		fmt.Fprintln(stdout, buf.String())
		return 0, true
	}
	if err := json.Unmarshal(raw, out); err != nil {
		fmt.Fprintf(stderr, "smind browser %s: decode response: %v\n", verb, err)
		return 1, false
	}
	return 0, true
}

// printBrowserStatus is the text form of browser.settings.get.
func printBrowserStatus(w io.Writer, s *browserSettings) {
	fmt.Fprintf(w, "browser: %s\n", browserOnOff(s.Enabled))
	fmt.Fprintf(w, "allowed hosts: %s\n", browserHostsLabel(s))
	fmt.Fprintf(w, "profile: %s\n", s.ProfileMode)
	if s.KillSwitch {
		fmt.Fprintln(w, "kill switch: ON (the daemon's configuration disables the browser)")
	}
	if !s.Available {
		fmt.Fprintln(w, "not yet offered to agents in this version")
	}
	if !s.ChromeInstalled {
		fmt.Fprintf(w, "no browser installed: %s\n", s.InstallStep)
	}
}

// browserOnOff names the switch state.
func browserOnOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// browserHostsLabel names the allowlist: its hosts comma-separated, or
// loopback only when empty (the daemon always allows loopback).
func browserHostsLabel(s *browserSettings) string {
	if len(s.AllowHosts) == 0 {
		return "loopback only"
	}
	return strings.Join(s.AllowHosts, ", ")
}
