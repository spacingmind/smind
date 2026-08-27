package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The wire-shape types below mirror internal/wsapi's own (unexported)
// result/event types exactly, field for field -- see that package's
// handlers.go. Duplicating them here rather than trying to share Go types
// across the client/server boundary is deliberate: the JSON wire contract
// is the actual interface between the CLI and the daemon (which may, in
// principle, be different versions of the smind binary), not a shared Go
// struct.

// runStartResult is run.start's terminal result.
type runStartResult struct {
	RunID string `json:"runId"`
}

// attachResult is run.attach's terminal result on success.
type attachResult struct {
	RunID      string `json:"runId"`
	StopReason string `json:"stopReason"`
}

// chunkEventParams is the params payload of every "chunk" event
// task.prompt/run.attach emit while streaming.
type chunkEventParams struct {
	Text string `json:"text"`
}

// runLogEvent is one event in a run.logs response.
type runLogEvent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
}

// runLogsResult is run.logs's terminal result.
type runLogsResult struct {
	RunID      string        `json:"runId"`
	Status     string        `json:"status"`
	StopReason string        `json:"stopReason,omitempty"`
	Err        string        `json:"err,omitempty"`
	Events     []runLogEvent `json:"events"`
}

func cmdTask(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind task <new|ls|send|attach|logs|stop> ...")
		return 2
	}
	switch args[0] {
	case "new":
		return cmdTaskNew(args[1:])
	case "ls":
		return cmdTaskList(args[1:])
	case "send":
		return cmdTaskSend(args[1:])
	case "attach":
		return cmdTaskAttach(args[1:])
	case "logs":
		return cmdTaskLogs(args[1:])
	case "stop":
		return cmdTaskStop(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind task: unknown subcommand %q\n", args[0])
		return 2
	}
}

// cmdTaskNewUsage is printed on any argument error in cmdTaskNew.
const cmdTaskNewUsage = "usage: smind task new <workspaceId> <title> [--space <spaceId>]"

// cmdTaskNew accepts an optional trailing "--space <spaceId>" pair after
// the title -- unlike cmdTaskLogs's flags, which can appear anywhere
// relative to its positional runId, --space only needs to work in this one
// documented position (title text comes last otherwise, so a trailing
// "--space <id>" can be unambiguously stripped off before the remaining
// args are joined into the title).
func cmdTaskNew(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdTaskNewUsage)
		return 2
	}
	workspaceID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task new: invalid workspaceId %q: %v\n", args[0], err)
		return 2
	}

	rest := args[1:]
	var spaceID *int64
	if len(rest) >= 2 && rest[len(rest)-2] == "--space" {
		id, err := parseInt64(rest[len(rest)-1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "task new: invalid --space value %q: %v\n", rest[len(rest)-1], err)
			return 2
		}
		spaceID = &id
		rest = rest[:len(rest)-2]
	}
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, cmdTaskNewUsage)
		return 2
	}
	title := strings.Join(rest, " ")

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{"workspaceId": workspaceID, "title": title}
	if spaceID != nil {
		params["spaceId"] = *spaceID
	}

	var task store.Task
	err = client.Call(context.Background(), "task.create", params, &task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task new: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\n", task.ID, task.Title, task.Status)
	return 0
}

func cmdTaskList(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind task ls <workspaceId>")
		return 2
	}
	workspaceID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task ls: invalid workspaceId %q: %v\n", args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var tasks []store.Task
	err = client.Call(context.Background(), "task.list", map[string]any{"workspaceId": workspaceID}, &tasks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tBRANCH")
	for _, t := range tasks {
		branch := ""
		if t.Branch != nil {
			branch = *t.Branch
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", t.ID, t.Title, t.Status, branch)
	}
	tw.Flush()
	return 0
}

// cmdTaskSend starts a run (via run.start, which returns as soon as the
// run is registered) and then streams it in the foreground exactly like
// `task attach` would, printing the runId first so a detached user can
// still find it. See streamRun for the actual streaming/detach behavior;
// run.start's decoupling from run.attach (see internal/wsapi/handlers.go's
// handleRunStart) is what makes Ctrl+C here detach instead of stopping the
// run.
func cmdTaskSend(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: smind task send <taskId> <provider> <prompt>")
		return 2
	}
	taskID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task send: invalid taskId %q: %v\n", args[0], err)
		return 2
	}
	provider := args[1]
	prompt := strings.Join(args[2:], " ")

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	// One signal context covers both run.start and the run.attach stream
	// that follows: run.start virtually always completes almost instantly
	// (it just registers the run) regardless of a cancel racing it (see
	// handleRunStart, which doesn't even look at its own request context),
	// so in the overwhelmingly common case Ctrl+C lands during the
	// streaming phase and detaches only the run.attach call, exactly as
	// intended.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var start runStartResult
	err = client.Call(ctx, "run.start", map[string]any{
		"taskId": taskID, "provider": provider, "prompt": prompt,
	}, &start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task send: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "run %s started\n", start.RunID)

	return streamRun(ctx, client, start.RunID)
}

// cmdTaskAttach streams an existing run's output, standalone -- the same
// run.attach mechanics as `task send`'s foreground streaming, minus the
// run.start that kicks it off. On an already-finished run, run.attach's
// own backfill-then-immediate-terminal behavior (see
// internal/runs.Registry.Subscribe) means this prints history and exits
// cleanly rather than hanging.
func cmdTaskAttach(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind task attach <runId>")
		return 2
	}
	runID := args[0]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return streamRun(ctx, client, runID)
}

// streamRun drives one run.attach call, printing each "chunk" event's text
// to stdout as it arrives (real incremental streaming: wsclient.CallStream
// invokes the callback per event off the wire, and fmt.Print writes
// directly to os.Stdout with no extra buffering layer in between) until the
// run reaches a terminal state or ctx is cancelled (SIGINT/SIGTERM).
//
// ctx cancellation is what implements Ctrl+C-detach: wsclient.CallStream
// only ever returns a context.Canceled/DeadlineExceeded error when this
// call's own ctx triggered the cancellation (see its doc comment), which
// server-side only detaches (run.attach's own context going Done never
// stops the run -- see internal/wsapi/handlers.go's attachAndStream) --
// distinct from a real terminal error/stop reported by the run itself,
// which arrives as a plain *wsclient.RPCError instead.
func streamRun(ctx context.Context, client *wsclient.Client, runID string) int {
	var result attachResult
	err := client.CallStream(ctx, "run.attach", map[string]any{"runId": runID}, func(event string, params json.RawMessage) {
		if event != "chunk" {
			return
		}
		var p chunkEventParams
		if err := json.Unmarshal(params, &p); err != nil {
			return
		}
		fmt.Print(p.Text)
	}, &result)
	fmt.Println()

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "detached -- run %s is still running; see `smind task logs %s`\n", runID, runID)
			return 0
		}
		fmt.Fprintf(os.Stderr, "run %s: %v\n", runID, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "run %s finished: %s\n", runID, result.StopReason)
	return 0
}

// cmdTaskLogsUsage is printed on any argument error -- kept as a constant
// (rather than inline at each call site) so it's identical whichever
// invalid-argument branch in cmdTaskLogs triggers it.
const cmdTaskLogsUsage = "usage: smind task logs <runId> [-f|--follow] [--tail N]"

// cmdTaskLogs is a one-shot run.logs by default (prints history + status,
// exits). --tail N passes tail through unchanged. -f/--follow streams
// exactly like `task attach` would -- if the run is still going that's a
// real backfill-then-live follow, and if it already finished, run.attach's
// own already-finished behavior means -f ends up printing the same history
// and exiting immediately, matching the non-follow case (there's nothing
// left to follow), without cmdTaskLogs needing to special-case that itself.
func cmdTaskLogs(args []string) int {
	// Parsed by hand instead of flag.FlagSet: the documented usage
	// (<runId> before the flags) is exactly the order Go's stdlib flag
	// package refuses to handle -- flag.Parse stops consuming flags at the
	// first non-flag argument, so "task logs <runId> --tail 5" would leave
	// "--tail" and "5" as unparsed positional args and fail. This loop
	// accepts the runId and the flags in any order/position instead.
	var runID string
	var follow bool
	var tail int
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--follow":
			follow = true
		case a == "--tail":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskLogsUsage)
				return 2
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "task logs: invalid --tail value %q: %v\n", args[i], err)
				return 2
			}
			tail = n
		case strings.HasPrefix(a, "--tail="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--tail="))
			if err != nil {
				fmt.Fprintf(os.Stderr, "task logs: invalid --tail value %q: %v\n", a, err)
				return 2
			}
			tail = n
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "task logs: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskLogsUsage)
			return 2
		case runID == "":
			runID = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskLogsUsage)
			return 2
		}
	}
	if runID == "" {
		fmt.Fprintln(os.Stderr, cmdTaskLogsUsage)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	if follow {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return streamRun(ctx, client, runID)
	}

	var result runLogsResult
	err = client.Call(context.Background(), "run.logs", map[string]any{"runId": runID, "tail": tail}, &result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task logs: %v\n", err)
		return 1
	}
	printRunLogs(result)
	return 0
}

func printRunLogs(result runLogsResult) {
	for _, e := range result.Events {
		if e.Type == "chunk" {
			fmt.Print(e.Text)
		}
	}
	fmt.Println()

	switch result.Status {
	case "done":
		fmt.Fprintf(os.Stderr, "run %s: done (%s)\n", result.RunID, result.StopReason)
	case "error":
		fmt.Fprintf(os.Stderr, "run %s: error: %s\n", result.RunID, result.Err)
	case "stopped":
		fmt.Fprintf(os.Stderr, "run %s: stopped\n", result.RunID)
	default:
		fmt.Fprintf(os.Stderr, "run %s: %s\n", result.RunID, result.Status)
	}
}

func cmdTaskStop(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind task stop <runId>")
		return 2
	}
	runID := args[0]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	if err := client.Call(context.Background(), "run.stop", map[string]any{"runId": runID}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "task stop: %v\n", err)
		return 1
	}
	fmt.Printf("run %s stopped\n", runID)
	return 0
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
