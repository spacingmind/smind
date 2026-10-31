package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/wsclient"
)

// hostDial is a variable so tests can stub the daemon connection. It returns
// the caller plus a close func, like relayDeviceDial.
var hostDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

const hostUsage = `usage: smind host ls [--json]
       smind host invite [--window 10m] [--via direct|relay] [--addr <ws(s)://host:port>]...
       smind host add <invite> [--alias x] [--password-env VAR] [--allow-cleartext]
       smind host approve <host> --sas <6 digits> --write <workspaceId>...
       smind host reject <host>
       smind host rm <host>
       smind host enable-reverse <host>
       smind host disable-reverse <host>
       smind host rename <host> <alias>
       smind host label set <host> <k:v>...
       smind host label rm <host> <key>...
       smind host label ls [<host>] [--json]
       smind host test <host>
       smind host route <host> --addr <ws(s)://host:port> [--password-env VAR] [--allow-cleartext]
       smind host select --labels k:v,k:v [--any]
       smind host info <host> | --labels k:v,k:v [--any] [--json]
       smind host workspaces <host> | --labels k:v,k:v [--any] [--json]
       smind host tasks <host> | --labels k:v,k:v [--any] [--workspace N] [--cursor N] [--limit N] [--json]
       smind host task <host:id> [--json]
       smind host status <host> <runId> | <host:taskId> [--json]
       smind host activity <host:taskId> [--run RUNID] [--cursor N] [--limit N] [--json]

Pair and manage connected hosts. Needs fleet.enabled: true and the daemon
running. <host> is a host id or its alias.
`

func cmdHost(args []string) int {
	return runHost(context.Background(), args, osStdout, osStderr)
}

// runHost is cmdHost with its environment injected (config from
// $SMIND_HOME). Exit codes: 0 ok, 1 a runtime error, 2 usage. The fleet
// section is checked before anything dials, so with the fleet off no
// connection is attempted at all; only bare `smind host` / -h print usage
// and exit 0 without that check.
func runHost(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, hostUsage)
		return 0
	}
	verb, rest := args[0], args[1:]
	if verb == "-h" || verb == "--help" || verb == "help" {
		fmt.Fprint(stdout, hostUsage)
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "smind host: %v\n", err)
		return 1
	}
	if !cfg.Fleet.Enabled {
		fmt.Fprintln(stderr, "smind host: fleet is off. Set fleet.enabled: true in ~/.smind/config.yaml and restart the daemon.")
		return 1
	}

	switch verb {
	case "ls":
		return runHostLs(ctx, rest, stdout, stderr)
	case "invite":
		return runHostInvite(ctx, rest, stdout, stderr)
	case "add":
		return runHostAdd(ctx, rest, stdout, stderr)
	case "approve":
		return runHostApprove(ctx, rest, stdout, stderr)
	case "reject", "rm", "enable-reverse", "disable-reverse":
		return runHostVerb(ctx, verb, rest, stdout, stderr)
	case "test":
		return runHostTest(ctx, rest, stdout, stderr)
	case "route":
		return runHostRoute(ctx, rest, stdout, stderr)
	case "rename":
		return runHostRename(ctx, rest, stdout, stderr)
	case "select":
		return runHostSelect(ctx, rest, stdout, stderr)
	case "label":
		return runHostLabel(ctx, rest, stdout, stderr)
	case "info", "workspaces", "tasks", "task", "status", "activity":
		return runHostRead(ctx, verb, rest, stdout, stderr)
	case "grant":
		fmt.Fprintln(stderr, "smind host grant: not implemented yet")
		return 2
	default:
		fmt.Fprintf(stderr, "smind host: unknown subcommand %q\n\n%s", verb, hostUsage)
		return 2
	}
}

// openHost dials the daemon the way the relay device commands do. ok is
// false after the error has been printed.
func openHost(ctx context.Context, stderr io.Writer) (call wsCaller, closeFn func(), ok bool) {
	call, closeFn, err := hostDial(ctx)
	if err != nil {
		if isNoDaemon(err) {
			fmt.Fprintln(stderr, "smind host: the daemon is not running (start it with `smind serve`)")
		} else {
			fmt.Fprintf(stderr, "smind host: cannot reach the daemon: %v\n", err)
		}
		return nil, nil, false
	}
	return call, closeFn, true
}

// hostErrMessage renders a daemon refusal as the one line the CLI prints.
func hostErrMessage(err error) string {
	var rpc *wsclient.RPCError
	if errors.As(err, &rpc) {
		return rpc.Message
	}
	return err.Error()
}

// hostToggle is one direction of the host's dial-out policy.
type hostToggle struct {
	Enabled bool `json:"enabled"`
}

// hostEntry is the CLI's own view of one host: exactly the fields the
// daemon contract lists and nothing more, so anything extra the daemon ever
// sends (a secret, a key) is dropped here and can never be printed.
type hostEntry struct {
	HostID    string     `json:"hostId"`
	Alias     string     `json:"alias"`
	Label     string     `json:"label"`
	State     string     `json:"state"`
	Inbound   hostToggle `json:"inbound"`
	CreatedAt string     `json:"createdAt"`
	// PendingApproval marks a pairing this human must approve here.
	PendingApproval bool `json:"pendingApproval"`
	OutboundEnabled bool `json:"outboundEnabled"`
	// Link is the link value; LinkCode and LinkGuidance accompany a refusal.
	Link         string `json:"link"`
	LinkCode     string `json:"linkCode,omitempty"`
	LinkGuidance string `json:"linkGuidance,omitempty"`
	// SAS is the pairing code to compare with the other host; kept only for
	// pending hosts.
	SAS string `json:"sas,omitempty"`
}

// hostWindow is the pairing window the daemon reports alongside the list.
type hostWindow struct {
	ExpiresAt string `json:"expiresAt"`
	Remaining int    `json:"remaining"`
}

type hostListResult struct {
	Hosts  []hostEntry `json:"hosts"`
	Window *hostWindow `json:"window"`
}

// shortHostID renders a host id as the CLI shows it: the first 8 characters.
func shortHostID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// onOff renders a dial-out direction for the ls table.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func runHostLs(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("host ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := false
	fs.BoolVar(&jsonOut, "json", false, "print the host list as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	var res hostListResult
	if err := call(ctx, "fleet.host.list", nil, &res); err != nil {
		fmt.Fprintf(stderr, "smind host ls: %s\n", hostErrMessage(err))
		return 1
	}
	for i := range res.Hosts {
		if res.Hosts[i].State != "pending" {
			res.Hosts[i].SAS = ""
		}
	}
	if jsonOut {
		out := struct {
			Hosts []hostEntry `json:"hosts"`
		}{res.Hosts}
		if out.Hosts == nil {
			out.Hosts = []hostEntry{}
		}
		if err := json.NewEncoder(stdout).Encode(&out); err != nil {
			fmt.Fprintf(stderr, "smind host ls: %v\n", err)
			return 1
		}
		return 0
	}
	renderHosts(stdout, res.Hosts)
	return 0
}

// renderHosts prints the ls table.
func renderHosts(w io.Writer, hosts []hostEntry) {
	if len(hosts) == 0 {
		fmt.Fprintln(w, "(no hosts)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tID\tSTATE\tOUT\tIN\tLINK\tSAS")
	for _, h := range hosts {
		alias := h.Alias
		if alias == "" {
			alias = "-"
		}
		link := h.Link
		if link == "" {
			link = "-"
		}
		if h.LinkCode != "" {
			link += fmt.Sprintf(" (%s)", h.LinkCode)
		}
		if h.LinkGuidance != "" {
			link += ": " + h.LinkGuidance
		}
		sas := h.SAS
		if sas == "" {
			sas = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", alias, shortHostID(h.HostID), h.State, onOff(h.OutboundEnabled), onOff(h.Inbound.Enabled), link, sas)
	}
	_ = tw.Flush()
}

// addrList is the repeatable --addr flag of `host invite`.
type addrList []string

func (a *addrList) String() string { return strings.Join(*a, ",") }

func (a *addrList) Set(v string) error { *a = append(*a, v); return nil }

// hostSplitArgs splits the leading positional arguments from the flag tail,
// so commands documented as `smind host add <invite> --alias x` still parse
// with flag.NewFlagSet (which stops at the first positional). The same shape
// cmdTaskNew uses: positionals first, flag pairs after.
func hostSplitArgs(args []string) (positional, flags []string) {
	for i, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			return args[:i], args[i:]
		}
	}
	return args, nil
}

func runHostInvite(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	positional, args := hostSplitArgs(args)
	fs := flag.NewFlagSet("host invite", flag.ContinueOnError)
	fs.SetOutput(stderr)
	window := fs.String("window", "10m", "how long the invitation stays valid")
	via := fs.String("via", "direct", "how the other host reaches this one: direct or relay")
	var addrs addrList
	fs.Var(&addrs, "addr", "direct address (ws(s)://host:port); repeatable")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || len(positional) != 0 {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	dur, err := time.ParseDuration(*window)
	if err != nil || dur <= 0 {
		fmt.Fprintf(stderr, "smind host invite: --window %q is not a positive duration\n", *window)
		return 2
	}
	if *via != "direct" && *via != "relay" {
		fmt.Fprintf(stderr, "smind host invite: --via must be direct or relay, got %q\n", *via)
		return 2
	}
	addresses := []string(addrs)
	if addresses == nil {
		addresses = []string{}
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	var res struct {
		Invite    string `json:"invite"`
		ExpiresAt string `json:"expiresAt"`
		HostID    string `json:"hostId"`
	}
	params := map[string]any{"windowSeconds": int(dur / time.Second), "via": *via, "addresses": addresses}
	if err := call(ctx, "fleet.host.invite", params, &res); err != nil {
		fmt.Fprintf(stderr, "smind host invite: %s\n", hostErrMessage(err))
		return 1
	}
	fmt.Fprintln(stdout, res.Invite)
	fmt.Fprintf(stdout, "Run on the other host: smind host add '%s'\n", res.Invite)
	fmt.Fprintf(stdout, "Then compare the code with the other host and approve here with: smind host approve %s --sas <code> --write <workspaceId>\n", shortHostID(res.HostID))
	return 0
}

func runHostAdd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	positional, args := hostSplitArgs(args)
	fs := flag.NewFlagSet("host add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	alias := fs.String("alias", "", "local name for the host")
	passwordEnv := fs.String("password-env", "", "environment variable holding the host password (never a flag value, so it stays out of shell history)")
	allowCleartext := fs.Bool("allow-cleartext", false, "accept a cleartext ws:// route outside the encrypted overlay (prompts readable on the network)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(positional) != 1 || positional[0] == "" || fs.NArg() != 0 {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	// The password comes from the environment only; unset means empty and
	// the daemon says whether that is acceptable.
	password := ""
	if *passwordEnv != "" {
		password = os.Getenv(*passwordEnv)
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	var res struct {
		HostID  string `json:"hostId"`
		Alias   string `json:"alias"`
		State   string `json:"state"`
		SAS     string `json:"sas"`
		Warning string `json:"warning"`
	}
	params := map[string]any{"invite": positional[0], "alias": *alias, "password": password, "allowCleartext": *allowCleartext}
	if err := call(ctx, "fleet.host.add", params, &res); err != nil {
		fmt.Fprintf(stderr, "smind host add: %s\n", hostErrMessage(err))
		return 1
	}
	fmt.Fprintf(stdout, "Added %s (%s). Compare this code with the other host: %s\n", res.Alias, res.State, res.SAS)
	if res.Warning != "" {
		fmt.Fprintf(stderr, "warning: %s\n", res.Warning)
	}
	return 0
}

// runHostRoute sets the direct address this daemon dials a paired host on,
// for example after that host enabled its reverse link to us.
func runHostRoute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	positional, args := hostSplitArgs(args)
	fs := flag.NewFlagSet("host route", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "", "direct address of the host (ws(s)://host:port)")
	passwordEnv := fs.String("password-env", "", "environment variable holding the host password (never a flag value, so it stays out of shell history)")
	allowCleartext := fs.Bool("allow-cleartext", false, "accept a cleartext ws:// route outside the encrypted overlay (prompts readable on the network)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(positional) != 1 || positional[0] == "" || fs.NArg() != 0 || *addr == "" {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	password := ""
	if *passwordEnv != "" {
		password = os.Getenv(*passwordEnv)
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	params := map[string]any{"host": positional[0], "address": *addr, "password": password, "allowCleartext": *allowCleartext}
	var res struct {
		OK bool `json:"ok"`
	}
	if err := call(ctx, "fleet.host.route", params, &res); err != nil {
		fmt.Fprintf(stderr, "smind host route: %s\n", hostErrMessage(err))
		return 1
	}
	fmt.Fprintf(stdout, "route set for %s\n", positional[0])
	return 0
}

// workspaceIDs is the repeatable --write flag of `host approve`.
type workspaceIDs []int

func (w *workspaceIDs) String() string {
	parts := make([]string, len(*w))
	for i, n := range *w {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func (w *workspaceIDs) Set(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%q is not a workspace id number", v)
	}
	*w = append(*w, n)
	return nil
}

func runHostApprove(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	positional, args := hostSplitArgs(args)
	fs := flag.NewFlagSet("host approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var writes workspaceIDs
	fs.Var(&writes, "write", "workspace id the host may create tasks in; repeatable")
	sas := fs.String("sas", "", "the 6-digit code shown by `smind host ls`, as compared with the other host")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(positional) != 1 || positional[0] == "" || fs.NArg() != 0 {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	if strings.TrimSpace(*sas) == "" {
		fmt.Fprintln(stderr, "smind host approve: approve needs --sas: compare the code shown by `smind host ls` with the other host and type it")
		return 2
	}
	if len(writes) == 0 {
		fmt.Fprintln(stderr, "smind host approve: approve needs at least one --write <workspaceId>: the host may only create tasks in workspaces you choose")
		return 2
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	dev, err := hostAct(ctx, call, "fleet.host.approve", positional[0], map[string]any{"sas": *sas, "writeWorkspaces": []int(writes)})
	if err != nil {
		fmt.Fprintf(stderr, "smind host approve: %s\n", hostErrMessage(err))
		return 1
	}
	fmt.Fprintf(stdout, "approved %s\n", hostDisplayName(dev))
	return 0
}

// hostAction is the common result of the one-host verbs that report the
// host back.
type hostAction struct {
	HostID  string `json:"hostId"`
	Alias   string `json:"alias"`
	State   string `json:"state"`
	Warning string `json:"warning"`
	// Host is the nested view the reverse verbs answer with.
	Host *struct {
		HostID string `json:"hostId"`
		Alias  string `json:"alias"`
	} `json:"host"`
}

// hostDisplayName picks what to call a host in a confirmation line.
func hostDisplayName(a hostAction) string {
	if a.Alias == "" && a.HostID == "" && a.Host != nil {
		a.Alias, a.HostID = a.Host.Alias, a.Host.HostID
	}
	if a.Alias != "" {
		return a.Alias
	}
	return shortHostID(a.HostID)
}

// hostAct calls one fleet.host.* method with {"host": host} plus extra
// params, decoding the common host report.
func hostAct(ctx context.Context, call wsCaller, method, host string, extra map[string]any) (hostAction, error) {
	params := map[string]any{"host": host}
	for k, v := range extra {
		params[k] = v
	}
	var res hostAction
	err := call(ctx, method, params, &res)
	return res, err
}

func runHostVerb(ctx context.Context, verb string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	method := map[string]string{
		"reject":          "fleet.host.reject",
		"rm":              "fleet.host.rm",
		"enable-reverse":  "fleet.host.enable_reverse",
		"disable-reverse": "fleet.host.disable_reverse",
	}[verb]

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	dev, err := hostAct(ctx, call, method, args[0], nil)
	if err != nil {
		fmt.Fprintf(stderr, "smind host %s: %s\n", verb, hostErrMessage(err))
		return 1
	}
	name := hostDisplayName(dev)
	if name == "" {
		name = args[0] // reject and rm answer {ok:true}: name what the human typed
	}
	if dev.Warning != "" {
		fmt.Fprintf(stderr, "warning: %s\n", dev.Warning)
	}
	switch verb {
	case "reject":
		fmt.Fprintf(stdout, "rejected %s\n", name)
	case "rm":
		fmt.Fprintf(stdout, "removed %s\n", name)
	case "enable-reverse":
		fmt.Fprintf(stdout, "reverse dial-out enabled for %s\n", name)
	case "disable-reverse":
		fmt.Fprintf(stdout, "reverse dial-out disabled for %s\n", name)
	}
	return 0
}

func runHostTest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	var res struct {
		OK        bool   `json:"ok"`
		LatencyMs int    `json:"latencyMs"`
		State     string `json:"state"`
		Code      string `json:"code"`
		Detail    string `json:"detail"`
	}
	if err := call(ctx, "fleet.host.test", map[string]any{"host": args[0]}, &res); err != nil {
		fmt.Fprintf(stderr, "smind host test: %s\n", hostErrMessage(err))
		return 1
	}
	verdict := "failed"
	if res.OK {
		verdict = "ok"
	}
	detail := ""
	if res.Detail != "" {
		detail = ", " + res.Detail
	}
	fmt.Fprintf(stdout, "host %s: %s (%s, %d ms, %s%s)\n", args[0], verdict, res.State, res.LatencyMs, res.Code, detail)
	return 0
}

func runHostRename(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] == "" || args[1] == "" {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}

	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	dev, err := hostAct(ctx, call, "fleet.host.rename", args[0], map[string]any{"alias": args[1]})
	if err != nil {
		fmt.Fprintf(stderr, "smind host rename: %s\n", hostErrMessage(err))
		return 1
	}
	fmt.Fprintf(stdout, "renamed %s to %q\n", shortHostID(dev.HostID), args[1])
	return 0
}
