package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mattn/go-isatty"

	"github.com/spacingmind/smind/internal/plugins"
)

const cmdPluginUsage = `usage: smind plugin <command>

  smind plugin install <source>          install DISABLED, then print what it would run
                                           <source>: a directory, git+<url>@<40-hex commit>,
                                           or <archive>#sha256=<64-hex hash>
  smind plugin link <dir>                use a directory in place (development; never copied or deleted)
  smind plugin list [--json]             installed plugins and the global switch
  smind plugin show <id> [--json]        what a plugin would run, where it came from, its state
  smind plugin enable <id> [--yes]       turn it on after you confirm what it would run
  smind plugin disable <id>              turn it off
  smind plugin remove <id> [--keep-data] delete its files, record and data/ (a linked directory is never touched)
  smind plugin update <id> [<source>] [--yes]
                                         stage a new version; one that changes what runs waits for --yes
  smind plugin reload <id>               re-read and re-validate an installed plugin

Plugins run trusted code as you. They are off until plugins.enabled is true in config.yaml
(a human edit; no command or agent can set it). Management is local only.`

// pluginIO is the CLI environment, injected for tests.
type pluginIO struct {
	in          io.Reader
	out, errOut io.Writer
	interactive bool // stdin is a terminal: a y/N prompt can be answered
	dial        func(context.Context) (daemonCaller, error)
}

func cmdPlugin(args []string) int {
	interactive := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	return runPlugin(args, pluginIO{in: os.Stdin, out: os.Stdout, errOut: os.Stderr, interactive: interactive, dial: dialDaemonCaller})
}

func runPlugin(args []string, env pluginIO) int {
	if len(args) == 0 {
		fmt.Fprintln(env.errOut, cmdPluginUsage)
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "install":
		return pluginInstall(rest, env, "install", "plugin.install", "source")
	case "link":
		return pluginInstall(rest, env, "link", "plugin.link", "path")
	case "list", "ls":
		return pluginList(rest, env)
	case "show":
		return pluginShow(rest, env)
	case "enable":
		return pluginEnable(rest, env)
	case "disable":
		return pluginSimple(rest, env, "disable", "plugin.disable", "disabled")
	case "reload":
		return pluginSimple(rest, env, "reload", "plugin.reload", "reloaded")
	case "remove", "rm":
		return pluginRemove(rest, env)
	case "update":
		return pluginUpdate(rest, env)
	case "-h", "--help", "help":
		fmt.Fprintln(env.out, cmdPluginUsage)
		return 0
	}
	fmt.Fprintf(env.errOut, "smind plugin: unknown subcommand %q\n%s\n", verb, cmdPluginUsage)
	return 2
}

// splitFlags separates known boolean flags from positionals.
func splitFlags(args []string, known ...string) (pos []string, flags map[string]bool, bad string) {
	flags = map[string]bool{}
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			ok := false
			for _, k := range known {
				if a == k {
					flags[k], ok = true, true
				}
			}
			if !ok {
				return nil, nil, a
			}
			continue
		}
		pos = append(pos, a)
	}
	return pos, flags, ""
}

func (e pluginIO) call(verb, method string, params, out any) error {
	c, err := e.dial(context.Background())
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(context.Background(), method, params, out)
}

func (e pluginIO) fail(verb string, err error) int {
	fmt.Fprintf(e.errOut, "plugin %s: %v\n", verb, err)
	return 1
}

// absSource makes a local path in an install source absolute, because the
// daemon resolves it from its own working directory. URLs and git sources are
// left alone.
func absSource(arg string) string {
	if strings.HasPrefix(arg, "git+") || strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "http://") {
		return arg
	}
	p, pin, hasPin := strings.Cut(arg, "#sha256=")
	abs, err := filepath.Abs(p)
	if err != nil {
		return arg
	}
	if hasPin {
		return abs + "#sha256=" + pin
	}
	return abs
}

func pluginInstall(args []string, env pluginIO, verb, method, field string) int {
	pos, _, bad := splitFlags(args)
	if bad != "" || len(pos) != 1 {
		fmt.Fprintf(env.errOut, "usage: smind plugin %s <%s>\n", verb, map[string]string{"source": "source", "path": "dir"}[field])
		return 2
	}
	src := pos[0]
	if field == "path" || !strings.Contains(src, "://") {
		src = absSource(src)
	}
	var in plugins.Info
	if err := env.call(verb, method, map[string]any{field: src}, &in); err != nil {
		return env.fail(verb, err)
	}
	fmt.Fprintf(env.out, "Installed %s %s (disabled; nothing runs until you enable it).\n\n", in.ID, in.Version)
	printConsent(env.out, in.Consent)
	fmt.Fprintf(env.out, "\nTo turn it on: smind plugin enable %s\n", in.ID)
	return 0
}

type pluginListing struct {
	GlobalEnabled bool           `json:"globalEnabled"`
	Plugins       []plugins.Info `json:"plugins"`
}

func pluginList(args []string, env pluginIO) int {
	pos, flags, bad := splitFlags(args, "--json")
	if bad != "" || len(pos) != 0 {
		fmt.Fprintln(env.errOut, "usage: smind plugin list [--json]")
		return 2
	}
	var l pluginListing
	if err := env.call("list", "plugin.list", nil, &l); err != nil {
		return env.fail("list", err)
	}
	if flags["--json"] {
		b, _ := json.MarshalIndent(l, "", "  ")
		fmt.Fprintln(env.out, string(b))
		return 0
	}
	if !l.GlobalEnabled {
		fmt.Fprintln(env.out, "Plugins are OFF: set plugins.enabled: true in config.yaml (a human edit) to use them.")
	}
	if len(l.Plugins) == 0 {
		fmt.Fprintln(env.out, "No plugins installed.")
		return 0
	}
	tw := tabwriter.NewWriter(env.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tVERSION\tSTATE\tSOURCE\tNAME")
	for _, p := range l.Plugins {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Version, pluginState(p, l.GlobalEnabled), p.SourceKind, p.Name)
	}
	tw.Flush()
	return 0
}

func pluginState(p plugins.Info, global bool) string {
	s := "disabled"
	switch {
	case p.Active:
		s = "active"
	case p.Enabled && !global:
		s = "enabled (switch off)"
	}
	if p.Pending != nil {
		s += ", update " + p.Pending.Consent.Version + " pending"
	}
	if p.LastError != "" {
		s += ", error"
	}
	return s
}

func pluginShow(args []string, env pluginIO) int {
	pos, flags, bad := splitFlags(args, "--json")
	if bad != "" || len(pos) != 1 {
		fmt.Fprintln(env.errOut, "usage: smind plugin show <id> [--json]")
		return 2
	}
	var in plugins.Info
	if err := env.call("show", "plugin.get", map[string]any{"id": pos[0]}, &in); err != nil {
		return env.fail("show", err)
	}
	if flags["--json"] {
		b, _ := json.MarshalIndent(in, "", "  ")
		fmt.Fprintln(env.out, string(b))
		return 0
	}
	state := "disabled"
	if in.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(env.out, "State: %s (confirmed: %t)\n", state, in.Consented)
	if in.LastError != "" {
		fmt.Fprintf(env.out, "Last error: %s\n", in.LastError)
	}
	fmt.Fprintln(env.out)
	printConsent(env.out, in.Consent)
	if in.Pending != nil {
		fmt.Fprintf(env.out, "\nPENDING UPDATE to %s (changes what the plugin runs or may call; apply with: smind plugin update %s --yes):\n\n", in.Pending.Consent.Version, in.ID)
		printConsent(env.out, in.Pending.Consent)
	}
	return 0
}

// printConsent writes what a plugin would run: the text a person confirms.
func printConsent(w io.Writer, c plugins.Consent) {
	fmt.Fprintf(w, "%s (%s) %s\n", c.Name, c.ID, c.Version)
	if c.Description != "" {
		fmt.Fprintf(w, "  %s\n", c.Description)
	}
	fmt.Fprintf(w, "  Source:       %s (%s)\n", c.Source, c.SourceKind)
	fmt.Fprintf(w, "  Content hash: %s\n", c.Hash)
	if c.DataOnly {
		fmt.Fprintln(w, "  Runs code:    no (data only: nothing it declares starts a process)")
	} else {
		fmt.Fprintln(w, "  Runs code:    YES, as you, with your files and credentials")
	}
	if len(c.Process) > 0 {
		fmt.Fprintf(w, "  Process:      %s\n", strings.Join(c.Process, " "))
	}
	for _, s := range c.McpServers {
		if s.Transport == "stdio" {
			fmt.Fprintf(w, "  MCP server:   %s: %s\n", s.Name, strings.Join(s.Command, " "))
		} else {
			fmt.Fprintf(w, "  MCP server:   %s: %s %s\n", s.Name, s.Transport, s.URL)
		}
	}
	for _, s := range c.Skills {
		fmt.Fprintf(w, "  Skill:        %s:%s (text the agent reads; delivered read-only)\n", c.ID, s)
	}
	for _, p := range c.Profiles {
		fmt.Fprintf(w, "  Profile:      %s:%s on %s\n", c.ID, p.Name, p.Provider)
		for _, e := range p.Escalations {
			fmt.Fprintf(w, "    ATTENTION:  %s\n", e)
		}
	}
	for _, p := range c.AcpProviders {
		fmt.Fprintf(w, "  ACP provider: %s: %s\n", p.ID, strings.Join(p.Command, " "))
	}
	for _, h := range c.HookEvents {
		fmt.Fprintf(w, "  Hook:         %s (on error: %s)\n", h.Event, h.OnError)
	}
	if len(c.Capabilities) > 0 {
		fmt.Fprintf(w, "  May call:     %s\n", strings.Join(c.Capabilities, ", "))
	} else {
		fmt.Fprintln(w, "  May call:     nothing")
	}
	if c.LinkWarning != "" {
		fmt.Fprintf(w, "  WARNING:      %s\n", c.LinkWarning)
	}
}

// confirm asks y/N. A non-interactive run never confirms by itself: it needs --yes.
func (e pluginIO) confirm(prompt string, yes bool) (bool, string) {
	if yes {
		return true, ""
	}
	if !e.interactive {
		return false, "refusing without confirmation: this is not a terminal, so pass --yes to confirm what is shown above"
	}
	fmt.Fprintf(e.out, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(e.in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, ""
	}
	return false, "not confirmed"
}

func pluginEnable(args []string, env pluginIO) int {
	pos, flags, bad := splitFlags(args, "--yes")
	if bad != "" || len(pos) != 1 {
		fmt.Fprintln(env.errOut, "usage: smind plugin enable <id> [--yes]")
		return 2
	}
	var in plugins.Info
	if err := env.call("enable", "plugin.get", map[string]any{"id": pos[0]}, &in); err != nil {
		return env.fail("enable", err)
	}
	printConsent(env.out, in.Consent)
	fmt.Fprintln(env.out)
	if ok, why := env.confirm(fmt.Sprintf("Enable %s and let it do the above?", in.ID), flags["--yes"]); !ok {
		fmt.Fprintf(env.errOut, "plugin enable: %s\n", why)
		return 1
	}
	// The confirmation names the exact summary the person saw.
	var out plugins.Info
	if err := env.call("enable", "plugin.enable", map[string]any{"id": in.ID, "confirm": in.Consent.Fingerprint}, &out); err != nil {
		return env.fail("enable", err)
	}
	fmt.Fprintf(env.out, "Enabled %s %s.\n", out.ID, out.Version)
	if out.Consent.LinkWarning != "" {
		fmt.Fprintf(env.out, "WARNING: %s\n", out.Consent.LinkWarning)
	}
	return 0
}

func pluginSimple(args []string, env pluginIO, verb, method, done string) int {
	pos, _, bad := splitFlags(args)
	if bad != "" || len(pos) != 1 {
		fmt.Fprintf(env.errOut, "usage: smind plugin %s <id>\n", verb)
		return 2
	}
	var out plugins.Info
	if err := env.call(verb, method, map[string]any{"id": pos[0]}, &out); err != nil {
		return env.fail(verb, err)
	}
	fmt.Fprintf(env.out, "%s %s.\n", capitalize(done), out.ID)
	if out.LastError != "" {
		fmt.Fprintf(env.out, "Note: %s\n", out.LastError)
	}
	return 0
}

func capitalize(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func pluginRemove(args []string, env pluginIO) int {
	pos, flags, bad := splitFlags(args, "--keep-data")
	if bad != "" || len(pos) != 1 {
		fmt.Fprintln(env.errOut, "usage: smind plugin remove <id> [--keep-data]")
		return 2
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := env.call("remove", "plugin.remove", map[string]any{"id": pos[0], "keepData": flags["--keep-data"]}, &out); err != nil {
		return env.fail("remove", err)
	}
	if flags["--keep-data"] {
		fmt.Fprintf(env.out, "Removed %s (its data/ directory was kept).\n", out.ID)
	} else {
		fmt.Fprintf(env.out, "Removed %s and its data.\n", out.ID)
	}
	return 0
}

func pluginUpdate(args []string, env pluginIO) int {
	pos, flags, bad := splitFlags(args, "--yes")
	if bad != "" || len(pos) < 1 || len(pos) > 2 {
		fmt.Fprintln(env.errOut, "usage: smind plugin update <id> [<source>] [--yes]")
		return 2
	}
	id, src := pos[0], ""
	if len(pos) == 2 {
		src = absSource(pos[1])
	}
	var cur plugins.Info
	if err := env.call("update", "plugin.get", map[string]any{"id": id}, &cur); err != nil {
		return env.fail("update", err)
	}
	var res plugins.UpdateResult
	// A pending update is applied as staged, not re-fetched.
	if src == "" && cur.Pending != nil {
		res = plugins.UpdateResult{Pending: true, Info: cur}
	} else if err := env.call("update", "plugin.update", map[string]any{"id": id, "source": src}, &res); err != nil {
		return env.fail("update", err)
	}
	if res.Applied {
		fmt.Fprintf(env.out, "Updated %s to %s (no change to what it runs or may call).\n", id, res.Info.Version)
		return 0
	}
	if res.Info.Pending == nil {
		fmt.Fprintf(env.out, "Nothing to apply for %s.\n", id)
		return 0
	}
	fmt.Fprintf(env.out, "Update %s -> %s changes what the plugin runs or may call. The current version %s keeps running until you confirm.\n\nCurrent:\n", id, res.Info.Pending.Consent.Version, res.Info.Version)
	printConsent(env.out, res.Info.Consent)
	fmt.Fprintln(env.out, "\nNew:")
	printConsent(env.out, res.Info.Pending.Consent)
	fmt.Fprintln(env.out)
	if ok, why := env.confirm(fmt.Sprintf("Apply the update to %s?", id), flags["--yes"]); !ok {
		fmt.Fprintf(env.out, "Update staged but not applied (%s). Apply later with: smind plugin update %s --yes\n", why, id)
		return 1
	}
	if err := env.call("update", "plugin.update", map[string]any{"id": id, "apply": true}, &res); err != nil {
		return env.fail("update", err)
	}
	fmt.Fprintf(env.out, "Updated %s to %s.\n", id, res.Info.Version)
	return 0
}
