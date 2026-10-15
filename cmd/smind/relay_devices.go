package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"text/tabwriter"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/wsclient"
)

// relayDeviceDial is a variable so tests can stub the daemon connection. It
// returns the caller plus a close func. A dial error means "no daemon": the
// commands then edit the registry file directly.
var relayDeviceDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

// deviceControl is the device administration surface, backed either by the
// running daemon (relay.device.* over /ws) or, with no daemon, by the registry
// file itself.
type deviceControl interface {
	list(ctx context.Context) ([]devices.Info, *devices.Window, error)
	approve(ctx context.Context, id string) (devices.Device, error)
	revoke(ctx context.Context, id string) (devices.Device, error)
	rename(ctx context.Context, id, label string) (devices.Device, error)
	// openWindow opens a pairing window and returns it as the daemon/registry
	// applied it (defaults filled in).
	openWindow(ctx context.Context, d time.Duration, maxNew int) (devices.Window, error)
	// viaDaemon reports whether the running daemon serves the calls.
	viaDaemon() bool
}

// openDeviceControl picks the daemon when one answers on home's own port and
// otherwise the registry file under home. Only the daemon that owns home
// (config.Dir()) is ever dialed, so a command run against another SMIND_HOME
// directory (tests) never touches a daemon that is not its own. noDaemonNote
// is printed to stderr when a dial was attempted and failed.
func openDeviceControl(ctx context.Context, home string, stderr io.Writer, noDaemonNote string) (deviceControl, func(), error) {
	if home == config.Dir() {
		call, closeFn, err := relayDeviceDial(ctx)
		if err == nil {
			return daemonDevices{call: call}, closeFn, nil
		}
		if !isNoDaemon(err) {
			// Not "nothing is listening": a daemon may well be running (a bad
			// token, a hung connection). Editing the file now could be silently
			// overwritten by the daemon's own writes, so report instead.
			return nil, nil, fmt.Errorf("cannot reach the daemon: %w", err)
		}
		fmt.Fprintf(stderr, "smind relay: the daemon is not running (%v); %s\n", err, noDaemonNote)
	}
	reg, err := devices.Open(devices.RegistryPath(home))
	if err != nil {
		return nil, nil, err
	}
	return fileDevices{reg: reg}, func() {}, nil
}

// isNoDaemon reports whether err means nothing is listening where the daemon
// would be (a refused TCP dial), as opposed to any other failure to talk to a
// daemon that may be running.
func isNoDaemon(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial" && !op.Timeout()
}

const noDaemonDeviceNote = "editing the device registry file directly; the change applies when the daemon next starts (a revoke cannot notify a relay)"

// daemonDevices talks to the running daemon.
type daemonDevices struct{ call wsCaller }

func (d daemonDevices) viaDaemon() bool { return true }

type deviceListResult struct {
	Devices []devices.Info  `json:"devices"`
	Window  *devices.Window `json:"window"`
}

func (d daemonDevices) list(ctx context.Context) ([]devices.Info, *devices.Window, error) {
	var res deviceListResult
	if err := d.call(ctx, "relay.device.list", nil, &res); err != nil {
		return nil, nil, err
	}
	return res.Devices, res.Window, nil
}

func (d daemonDevices) one(ctx context.Context, method string, params any) (devices.Device, error) {
	var dev devices.Device
	err := d.call(ctx, method, params, &dev)
	return dev, err
}

func (d daemonDevices) approve(ctx context.Context, id string) (devices.Device, error) {
	return d.one(ctx, "relay.device.approve", map[string]any{"id": id})
}

func (d daemonDevices) revoke(ctx context.Context, id string) (devices.Device, error) {
	return d.one(ctx, "relay.device.revoke", map[string]any{"id": id})
}

func (d daemonDevices) rename(ctx context.Context, id, label string) (devices.Device, error) {
	return d.one(ctx, "relay.device.rename", map[string]any{"id": id, "label": label})
}

func (d daemonDevices) openWindow(ctx context.Context, dur time.Duration, maxNew int) (devices.Window, error) {
	var w *devices.Window
	params := map[string]any{"duration_seconds": int(dur / time.Second), "max_devices": maxNew}
	if err := d.call(ctx, "relay.device.window", params, &w); err != nil {
		return devices.Window{}, err
	}
	if w == nil {
		return devices.Window{}, errors.New("the daemon opened no pairing window")
	}
	return *w, nil
}

// fileDevices edits the registry file directly (no daemon).
type fileDevices struct{ reg *devices.Registry }

func (f fileDevices) viaDaemon() bool { return false }

func (f fileDevices) list(context.Context) ([]devices.Info, *devices.Window, error) {
	var out []devices.Info
	for _, d := range f.reg.List() {
		out = append(out, devices.Info{Device: d})
	}
	w, ok := f.reg.Window()
	if !ok {
		return out, nil, nil
	}
	return out, &w, nil
}

func (f fileDevices) act(id string, do func(string) (devices.Device, error)) (devices.Device, error) {
	full, err := f.reg.Resolve(id)
	if err != nil {
		return devices.Device{}, err
	}
	return do(full)
}

func (f fileDevices) approve(_ context.Context, id string) (devices.Device, error) {
	return f.act(id, f.reg.Approve)
}

func (f fileDevices) revoke(_ context.Context, id string) (devices.Device, error) {
	return f.act(id, f.reg.Revoke)
}

func (f fileDevices) rename(_ context.Context, id, label string) (devices.Device, error) {
	return f.act(id, func(full string) (devices.Device, error) { return f.reg.Rename(full, label) })
}

func (f fileDevices) openWindow(_ context.Context, d time.Duration, maxNew int) (devices.Window, error) {
	if err := f.reg.OpenWindow(d, maxNew); err != nil {
		return devices.Window{}, err
	}
	w, _ := f.reg.Window()
	return w, nil
}

// deviceErrMessage turns a daemon RPC error or a registry sentinel into the
// one-line wording the CLI prints.
func deviceErrMessage(err error) string {
	var rpc *wsclient.RPCError
	switch {
	case errors.Is(err, devices.ErrNotFound):
		return "no such device"
	case errors.Is(err, devices.ErrAmbiguous):
		return "device id prefix is ambiguous; use more characters"
	case errors.As(err, &rpc):
		return rpc.Message
	}
	return err.Error()
}

const relayDevicesUsage = `usage: smind relay devices ls [--pending]
       smind relay devices approve <id>
       smind relay devices revoke <id>
       smind relay devices rename <id> <label>

<id> is a device id or any unique prefix of it (at least 4 characters).
With the daemon running the commands go through it; otherwise they edit the
device registry file directly and apply when the daemon next starts.
`

func cmdRelayDevices(args []string) int {
	return runRelayDevices(context.Background(), args, config.Dir(), osStdout, osStderr)
}

// runRelayDevices is cmdRelayDevices with its environment injected. Exit
// codes: 0 ok, 1 runtime error, 2 usage.
func runRelayDevices(ctx context.Context, args []string, home string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, relayDevicesUsage)
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, relayDevicesUsage)
		return 0
	case "ls", "approve", "revoke", "rename":
	default:
		fmt.Fprintf(stderr, "smind relay devices: unknown subcommand %q\n\n%s", verb, relayDevicesUsage)
		return 2
	}

	pending := false
	if verb == "ls" {
		fs := flag.NewFlagSet("relay devices ls", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.BoolVar(&pending, "pending", false, "only devices awaiting approval")
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() != 0 {
			fmt.Fprint(stderr, relayDevicesUsage)
			return 2
		}
	} else {
		want := 1
		if verb == "rename" {
			want = 2
		}
		if len(rest) != want || rest[0] == "" {
			fmt.Fprint(stderr, relayDevicesUsage)
			return 2
		}
	}

	ctl, closeFn, err := openDeviceControl(ctx, home, stderr, noDaemonDeviceNote)
	if err != nil {
		fmt.Fprintf(stderr, "smind relay devices: %v\n", err)
		return 1
	}
	defer closeFn()

	if verb == "ls" {
		list, _, err := ctl.list(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "smind relay devices ls: %s\n", deviceErrMessage(err))
			return 1
		}
		renderDevices(stdout, list, pending, ctl.viaDaemon())
		return 0
	}

	var dev devices.Device
	switch verb {
	case "approve":
		dev, err = ctl.approve(ctx, rest[0])
	case "revoke":
		dev, err = ctl.revoke(ctx, rest[0])
	case "rename":
		dev, err = ctl.rename(ctx, rest[0], rest[1])
	}
	if err != nil {
		fmt.Fprintf(stderr, "smind relay devices %s: %s\n", verb, deviceErrMessage(err))
		return 1
	}
	switch verb {
	case "approve":
		fmt.Fprintf(stdout, "device %s approved\n", dev.ID)
	case "revoke":
		fmt.Fprintf(stdout, "device %s revoked\n", dev.ID)
	case "rename":
		fmt.Fprintf(stdout, "device %s renamed to %q\n", dev.ID, cleanText(dev.Label))
	}
	return 0
}

// deviceTime formats t in local time at minute precision ("-" when unset).
func deviceTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02T15:04Z07:00")
}

// renderDevices prints the ls table. connected is only known when the daemon
// answered; without it the column shows "-".
func renderDevices(w io.Writer, list []devices.Info, pendingOnly, connectedKnown bool) {
	var rows []devices.Info
	nPending := 0
	for _, d := range list {
		if d.State == devices.StatePending {
			nPending++
		}
		if pendingOnly && d.State != devices.StatePending {
			continue
		}
		rows = append(rows, d)
	}
	if len(rows) == 0 {
		if pendingOnly {
			fmt.Fprintln(w, "(no devices awaiting approval)")
		} else {
			fmt.Fprintln(w, "(no devices)")
		}
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tLABEL\tSTATE\tCONNECTED\tFIRST SEEN\tLAST SEEN")
	for _, d := range rows {
		conn := "-"
		if connectedKnown {
			conn = "no"
			if d.Connected {
				conn = "yes"
			}
		}
		label := cleanText(d.Label)
		if label == "" {
			label = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, label, d.State, conn, deviceTime(d.FirstSeen), deviceTime(d.LastSeen))
	}
	_ = tw.Flush()
	if nPending > 0 {
		fmt.Fprintf(w, "\n%d device(s) awaiting approval: smind relay devices approve <id>\n", nPending)
	}
}
