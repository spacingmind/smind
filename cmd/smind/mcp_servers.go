package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

const cmdMcpAddUsage = "usage: smind mcp add <name> <stdio|http|sse> [--command <cmd>] [--arg <value>]... [--url <url>] [--env <KEY=VALUE>]... [--header <KEY=VALUE>]..."

// mcpServerCLI is the CLI's decode target for every mcp.* RPC result: the
// wire shape internal/wsapi's mcpServerResult marshals to (lowercase field
// names, env/headers already redacted server-side -- this struct never
// carries a real secret value, only whatever placeholder the daemon sent).
type mcpServerCLI struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Transport string          `json:"transport"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
	URL       string          `json:"url"`
	Env       json.RawMessage `json:"env"`
	Headers   json.RawMessage `json:"headers"`
	Enabled   bool            `json:"enabled"`
}

// cmdMcpAdd parses `smind mcp add <name> <transport> [flags]`. Flags are
// each a single `--flag value` pair (matching `profile add`'s convention);
// --arg/--env/--header are each repeatable, accumulating into args/env/
// headers rather than the last one winning.
func cmdMcpAdd(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdMcpAddUsage)
		return 2
	}
	name, transport := args[0], args[1]

	var command, url string
	var argList []string
	env := map[string]string{}
	headers := map[string]string{}
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		if i+1 >= len(rest) {
			fmt.Fprintf(os.Stderr, "mcp add: flag %q needs a value\n", rest[i])
			return 2
		}
		flag, value := rest[i], rest[i+1]
		i++
		switch flag {
		case "--command":
			command = value
		case "--arg":
			argList = append(argList, value)
		case "--url":
			url = value
		case "--env":
			k, v, ok := splitKV(value)
			if !ok {
				fmt.Fprintf(os.Stderr, "mcp add: --env value %q must be KEY=VALUE\n", value)
				return 2
			}
			env[k] = v
		case "--header":
			k, v, ok := splitKV(value)
			if !ok {
				fmt.Fprintf(os.Stderr, "mcp add: --header value %q must be KEY=VALUE\n", value)
				return 2
			}
			headers[k] = v
		default:
			fmt.Fprintf(os.Stderr, "mcp add: unknown flag %q\n", flag)
			return 2
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{
		"name": name, "transport": transport, "command": command, "url": url,
	}
	if len(argList) > 0 {
		params["args"] = argList
	}
	if len(env) > 0 {
		params["env"] = env
	}
	if len(headers) > 0 {
		params["headers"] = headers
	}

	var m mcpServerCLI
	if err := client.Call(context.Background(), "mcp.create", params, &m); err != nil {
		fmt.Fprintf(os.Stderr, "mcp add: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%t\n", m.ID, m.Name, m.Transport, m.Enabled)
	return 0
}

// splitKV splits a "KEY=VALUE" flag argument, rejecting one with no '='.
func splitKV(s string) (key, value string, ok bool) {
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func cmdMcpList(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind mcp ls")
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var servers []mcpServerCLI
	if err := client.Call(context.Background(), "mcp.list", nil, &servers); err != nil {
		fmt.Fprintf(os.Stderr, "mcp ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tTRANSPORT\tCOMMAND\tARGS\tURL\tENV\tHEADERS\tENABLED")
	for _, m := range servers {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\n",
			m.ID, m.Name, m.Transport, m.Command, formatArgs(m.Args), m.URL, formatKV(m.Env), formatKV(m.Headers), m.Enabled)
	}
	tw.Flush()
	return 0
}

// formatArgs renders a stdio server's args JSON array as a space-joined
// string for ls's ARGS column, or "" for an empty/absent array.
func formatArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var args []string
	if err := json.Unmarshal(raw, &args); err != nil {
		return string(raw)
	}
	return strings.Join(args, " ")
}

// formatKV renders an env/headers JSON object as a sorted, comma-joined
// "key=value" string for ls's ENV/HEADERS columns. Every value here has
// already been redacted by the daemon (mcp.list's result is never the raw
// store row) -- this is display formatting only, not a second redaction
// step.
func formatKV(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + m[k]
	}
	return strings.Join(parts, ",")
}

func cmdMcpRemove(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind mcp rm <id>")
		return 2
	}
	id, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp rm: invalid id %q: %v\n", args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	if err := client.Call(context.Background(), "mcp.delete", map[string]any{"id": id}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "mcp rm: %v\n", err)
		return 1
	}
	return 0
}

// cmdMcpSetEnabled backs both `mcp enable` and `mcp disable`: same RPC,
// same argument shape, differing only in the boolean and the verb used in
// usage/error messages.
func cmdMcpSetEnabled(args []string, enabled bool) int {
	verb := "enable"
	if !enabled {
		verb = "disable"
	}
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: smind mcp %s <id>\n", verb)
		return 2
	}
	id, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp %s: invalid id %q: %v\n", verb, args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var m mcpServerCLI
	if err := client.Call(context.Background(), "mcp.setEnabled", map[string]any{"id": id, "enabled": enabled}, &m); err != nil {
		fmt.Fprintf(os.Stderr, "mcp %s: %v\n", verb, err)
		return 1
	}
	fmt.Printf("%d\t%s\t%t\n", m.ID, m.Name, m.Enabled)
	return 0
}
