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

// chunkEventParams is the params payload of every "chunk", "user_message",
// and "thinking" event task.prompt/run.attach emit while streaming -- all
// three are just "a chunk of text from some role" on the wire, per
// docs/decisions/0008-structured-run-events.md.
type chunkEventParams struct {
	Text string `json:"text"`
}

// toolCallEventParams is the params payload of every "tool_call" event
// task.prompt/run.attach emit while streaming, and (embedded in
// runLogEvent) the shape of a "tool_call" run.logs entry. Old readers of
// this file (before docs/decisions/0008-structured-run-events.md) simply
// don't have this type or the "tool_call"/"user_message"/"thinking" cases
// below -- an older `smind` CLI ignores those event names/fields
// entirely, same as any other unrecognized wire addition.
type toolCallEventParams struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName,omitempty"`
	Title      string          `json:"title,omitempty"`
	Status     string          `json:"status,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// rawEventParams is the params payload of a "raw" event task.prompt/
// run.attach emit -- an ACP session-update kind the daemon's normalizer
// doesn't recognize (e.g. "plan"), forwarded instead of dropped. See
// docs/decisions/0010-preserve-unknown-acp-event-kinds.md. Like
// tool_call/user_message/thinking, an older CLI build simply doesn't have
// this type or the "raw" case below and ignores the event name entirely.
type rawEventParams struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// renderRaw formats one "raw" event as a single readable line: the
// unrecognized kind, and its raw payload's compact JSON -- there is no
// typed shape to render more richly than that.
//
// Kind is quoted (strconv.Quote) rather than printed verbatim: unlike
// Payload -- raw wire bytes that can never contain a literal newline,
// since ACP's transport is itself newline-delimited JSON -- Kind is a
// JSON-decoded Go string, so a provider whose sessionUpdate value embeds
// "\n"/control characters would otherwise let a single event smear across
// multiple physical lines, breaking any line-oriented consumer of `task
// logs`/`task attach` output.
func renderRaw(p rawEventParams) string {
	return fmt.Sprintf("[raw] %s: %s\n", strconv.Quote(p.Kind), p.Payload)
}

// permissionOptionParams is one choice offered by a "permission_request"
// run.logs entry -- mirrors internal/wsapi's permissionOptionParams.
type permissionOptionParams struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// renderPermissionRequest formats a pending permission request so a human
// watching `task attach`/`task logs -f` can actually see it needs a
// decision -- unlike tool_call/raw, this previously had no case in
// printRunLogs/streamRun at all, so a run would sit waiting for
// runs.defaultPermissionTimeout (5 minutes) with nothing on screen
// explaining why. Each option's ID is shown explicitly so it can be pasted
// straight into `task approve <runId> <requestId> <optionId>`.
func renderPermissionRequest(requestID, summary string, options []permissionOptionParams) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[permission] %s (request %s)\n", summary, requestID)
	for _, o := range options {
		fmt.Fprintf(&b, "  - %s: %s (%s)\n", o.ID, o.Label, o.Kind)
	}
	fmt.Fprintf(&b, "  -> smind task approve %s\n", requestID)
	return b.String()
}

// renderPermissionResolved formats how a permission request was resolved --
// distinguishing a real human decision from an auto-safe allow or a
// timeout deny (runs.PermissionResolution) matters because a silent
// timeout-deny is exactly the failure mode a human watching the run needs
// to notice, not mistake for the agent giving up on its own.
func renderPermissionResolved(requestID, optionID, reason string) string {
	return fmt.Sprintf("[permission] %s -> %s (%s)\n", requestID, optionID, reason)
}

// runLogEvent is one event in a run.logs response.
type runLogEvent struct {
	Type       string                   `json:"type"`
	Text       string                   `json:"text,omitempty"`
	StopReason string                   `json:"stopReason,omitempty"`
	RequestID  string                   `json:"requestId,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	Options    []permissionOptionParams `json:"options,omitempty"`
	OptionID   string                   `json:"optionId,omitempty"`
	Reason     string                   `json:"reason,omitempty"`
	toolCallEventParams
	rawEventParams
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
		fmt.Fprintln(os.Stderr, "usage: smind task <new|ls|send|attach|logs|stop|permissions|approve|options|set-option> ...")
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
	case "permissions":
		return cmdTaskPermissions(args[1:])
	case "approve":
		return cmdTaskApprove(args[1:])
	case "options":
		return cmdTaskOptions(args[1:])
	case "set-option":
		return cmdTaskSetOption(args[1:])
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

// cmdTaskSendUsage is printed on any argument error in cmdTaskSend.
const cmdTaskSendUsage = "usage: smind task send <taskId> <provider> <prompt> [--approval-policy manual|auto-safe]"

// cmdTaskSend starts a run (via run.start, which returns as soon as the
// run is registered) and then streams it in the foreground exactly like
// `task attach` would, printing the runId first so a detached user can
// still find it. See streamRun for the actual streaming/detach behavior;
// run.start's decoupling from run.attach (see internal/wsapi/handlers.go's
// handleRunStart) is what makes Ctrl+C here detach instead of stopping the
// run.
//
// --approval-policy is passed through to run.start's approvalPolicy field
// (default manual when absent -- see internal/runs.Registry.Start);
// auto-safe lets a headless/monitored run self-approve the allowlisted
// read-only verification commands (gofmt/go vet/go test) instead of
// stalling on a permission card nobody is watching.
func cmdTaskSend(args []string) int {
	// Parsed by hand for the same reason as cmdTaskLogs: the prompt is
	// free-form positional text, so stdlib flag parsing can't reliably
	// separate it from flags.
	var taskIDArg, provider, approvalPolicy string
	var promptParts []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--approval-policy":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
				return 2
			}
			i++
			approvalPolicy = args[i]
		case strings.HasPrefix(a, "--approval-policy="):
			approvalPolicy = strings.TrimPrefix(a, "--approval-policy=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "task send: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
			return 2
		case taskIDArg == "":
			taskIDArg = a
		case provider == "":
			provider = a
		default:
			promptParts = append(promptParts, a)
		}
	}
	if taskIDArg == "" || provider == "" || len(promptParts) == 0 {
		fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
		return 2
	}
	taskID, err := parseInt64(taskIDArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task send: invalid taskId %q: %v\n", taskIDArg, err)
		return 2
	}
	prompt := strings.Join(promptParts, " ")

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

	params := map[string]any{
		"taskId": taskID, "provider": provider, "prompt": prompt,
	}
	if approvalPolicy != "" {
		params["approvalPolicy"] = approvalPolicy
	}
	var start runStartResult
	err = client.Call(ctx, "run.start", params, &start)
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
	names := toolCallNames{}
	err := client.CallStream(ctx, "run.attach", map[string]any{"runId": runID}, func(event string, params json.RawMessage) {
		switch event {
		case "chunk", "user_message", "thinking":
			// All three are just "a chunk of text from some role" on the
			// wire (see chunkEventParams's doc comment): printed the same
			// way chunk always has been, with no per-fragment framing,
			// since a fragment boundary here is a streaming artifact, not
			// a place a reader would want a line break.
			var p chunkEventParams
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			fmt.Print(p.Text)
		case "tool_call":
			var p toolCallEventParams
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			fmt.Print(names.render(p))
		case "raw":
			var p rawEventParams
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			fmt.Print(renderRaw(p))
		case "permission_request":
			var p struct {
				RequestID string                   `json:"requestId"`
				Summary   string                   `json:"summary"`
				Options   []permissionOptionParams `json:"options"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			fmt.Print(renderPermissionRequest(p.RequestID, p.Summary, p.Options))
		case "permission_resolved":
			var p struct {
				RequestID string `json:"requestId"`
				OptionID  string `json:"optionId"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			fmt.Print(renderPermissionResolved(p.RequestID, p.OptionID, p.Reason))
		}
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

// toolCallNames remembers each toolCallId's display name from whichever
// event first carried one. A tool call's completing event carries only its
// id, status and result -- Claude Code never repeats the name, and ACP's
// tool_call_update is a partial update (see taskrunner.EventTypeToolCall)
// -- so without this a completion would render as a bare wire id
// ("[tool] toolu_01ABC: done") instead of "[tool] Bash: done". It is the
// CLI's minimal version of the merge-by-id the web UI does for its cards.
type toolCallNames map[string]string

// render formats one tool_call event as a single readable line --
// name/title and lifecycle status, plus the raw input while it's still
// running -- so `task attach`/`task logs`'s output stays legible without
// trying to be the full card the web UI renders (see
// docs/decisions/0008-structured-run-events.md). Prefers Title (ACP's
// human-readable summary) over ToolName (the wire tool name) when both
// are present, then a name remembered from an earlier event for the same
// id, and finally the id itself (never render an empty name).
func (n toolCallNames) render(p toolCallEventParams) string {
	name := p.ToolName
	if p.Title != "" {
		name = p.Title
	}
	switch {
	case name != "" && p.ToolCallID != "":
		n[p.ToolCallID] = name
	case name == "":
		name = n[p.ToolCallID]
	}
	if name == "" {
		name = p.ToolCallID
	}
	switch p.Status {
	case "success":
		return fmt.Sprintf("[tool] %s: done\n", name)
	case "failure":
		if len(p.Result) > 0 {
			return fmt.Sprintf("[tool] %s: failed: %s\n", name, p.Result)
		}
		return fmt.Sprintf("[tool] %s: failed\n", name)
	default:
		if len(p.Input) > 0 {
			return fmt.Sprintf("[tool] %s: %s\n", name, p.Input)
		}
		return fmt.Sprintf("[tool] %s\n", name)
	}
}

func printRunLogs(result runLogsResult) {
	names := toolCallNames{}
	for _, e := range result.Events {
		switch e.Type {
		case "chunk", "user_message", "thinking":
			fmt.Print(e.Text)
		case "tool_call":
			fmt.Print(names.render(e.toolCallEventParams))
		case "raw":
			fmt.Print(renderRaw(e.rawEventParams))
		case "permission_request":
			fmt.Print(renderPermissionRequest(e.RequestID, e.Summary, e.Options))
		case "permission_resolved":
			fmt.Print(renderPermissionResolved(e.RequestID, e.OptionID, e.Reason))
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

// pendingPermission is one still-unresolved permission_request entry found
// by scanning a run.logs response -- run.logs has no dedicated "list
// pending" call of its own (a permission_request is just one more event
// type in the same history everything else comes through), so both
// cmdTaskPermissions and cmdTaskApprove derive "pending" the same way:
// every permission_request whose requestId never shows up on a later
// permission_resolved entry.
type pendingPermission struct {
	RequestID string
	Summary   string
	Options   []permissionOptionParams
}

// fetchPendingPermissions calls run.logs for runID and returns every
// permission_request entry not yet matched by a permission_resolved entry,
// oldest first.
func fetchPendingPermissions(ctx context.Context, client *wsclient.Client, runID string) ([]pendingPermission, error) {
	var result runLogsResult
	if err := client.Call(ctx, "run.logs", map[string]any{"runId": runID}, &result); err != nil {
		return nil, err
	}
	resolved := map[string]bool{}
	for _, e := range result.Events {
		if e.Type == "permission_resolved" {
			resolved[e.RequestID] = true
		}
	}
	var pending []pendingPermission
	for _, e := range result.Events {
		if e.Type == "permission_request" && !resolved[e.RequestID] {
			pending = append(pending, pendingPermission{RequestID: e.RequestID, Summary: e.Summary, Options: e.Options})
		}
	}
	return pending, nil
}

// cmdTaskPermissions lists a run's still-pending permission requests --
// the read half of the same gap task approve closes: without this, seeing
// that a run needs a decision means either watching `task attach` live
// (easy to miss the one line among a long agent transcript) or reading
// run.logs's raw JSON by hand.
func cmdTaskPermissions(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind task permissions <runId>")
		return 2
	}
	runID := args[0]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	pending, err := fetchPendingPermissions(context.Background(), client, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task permissions: %v\n", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Println("no pending permission requests")
		return 0
	}
	for _, p := range pending {
		fmt.Print(renderPermissionRequest(p.RequestID, p.Summary, p.Options))
	}
	return 0
}

// cmdTaskApproveUsage is printed on any argument error in cmdTaskApprove.
const cmdTaskApproveUsage = "usage: smind task approve <runId> [requestId] [optionId]"

// cmdTaskApprove answers a run's pending permission request via
// run.respondPermission -- closing the gap where a run with
// --approval-policy manual (the default) or an auto-safe run hitting a
// command outside its allowlist has no CLI-side way to be unblocked at
// all short of the 5-minute timeout-deny (runs.defaultPermissionTimeout).
//
// requestId defaults to the oldest still-pending request (there is
// normally only one at a time -- a run blocks on Decide before issuing
// another tool call). optionId defaults to the first option whose Kind is
// "allow_once"/"allow_always", mirroring
// internal/runs.firstOptionByKind's own selection so `task approve
// <runId>` with no further arguments does the obvious thing.
func cmdTaskApprove(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, cmdTaskApproveUsage)
		return 2
	}
	runID := args[0]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	pending, err := fetchPendingPermissions(context.Background(), client, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task approve: %v\n", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintln(os.Stderr, "task approve: no pending permission request")
		return 1
	}

	var target *pendingPermission
	if len(args) == 2 {
		requestID := args[1]
		for i := range pending {
			if pending[i].RequestID == requestID {
				target = &pending[i]
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "task approve: request %q is not pending\n", requestID)
			return 1
		}
	} else {
		target = &pending[0]
	}

	optionID := ""
	for _, o := range target.Options {
		if o.Kind == "allow_once" || o.Kind == "allow_always" {
			optionID = o.ID
			break
		}
	}
	if optionID == "" {
		fmt.Fprintf(os.Stderr, "task approve: request %q has no allow option: %+v\n", target.RequestID, target.Options)
		return 1
	}

	err = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": target.RequestID, "optionId": optionID,
	}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task approve: %v\n", err)
		return 1
	}
	fmt.Printf("request %s -> %s\n", target.RequestID, optionID)
	return 0
}


// configOption is the CLI-side mirror of internal/wsapi's
// configOptionParams, field for field -- same wire-shape duplication
// convention as runLogsResult and the other result types above (the JSON
// wire contract is the interface between CLI and daemon, not a shared Go
// struct). CurrentValue's shape varies by option type ({"type":"id",
// "value":...} for select options, a bool for boolean options), so it's
// carried as raw JSON and printed compact.
type configOption struct {
	ConfigID     string          `json:"configId"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Category     string          `json:"category,omitempty"`
	Type         string          `json:"type"`
	CurrentValue json.RawMessage `json:"currentValue,omitempty"`
}

// runConfigOptions is the result of run.listConfigOptions and
// run.setConfigOption.
type runConfigOptions struct {
	Options []configOption `json:"options"`
}

// cmdTaskOptionsUsage is printed on any argument error in cmdTaskOptions.
const cmdTaskOptionsUsage = "usage: smind task options <runId>"

// cmdTaskOptions lists a run's ACP session config options. An empty list
// prints an explanatory note rather than nothing: it means either the
// provider doesn't support config options at all (claude-native,
// codex-native) or the run has no live session to have discovered any.
func cmdTaskOptions(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, cmdTaskOptionsUsage)
		return 2
	}
	runID := args[0]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var result runConfigOptions
	if err := client.Call(context.Background(), "run.listConfigOptions", map[string]any{"runId": runID}, &result); err != nil {
		fmt.Fprintf(os.Stderr, "task options: %v\n", err)
		return 1
	}
	printConfigOptions(runID, result.Options)
	return 0
}

// cmdTaskSetOptionUsage is printed on any argument error in cmdTaskSetOption.
const cmdTaskSetOptionUsage = "usage: smind task set-option <runId> <configId> <value>"

// cmdTaskSetOption sets one config option on a run's live ACP session and
// prints the refreshed option list the daemon sends back. Failures print
// the daemon's rejection reason (unknown option id, non-ACP provider, no
// live session) before the non-zero exit.
func cmdTaskSetOption(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, cmdTaskSetOptionUsage)
		return 2
	}
	runID, configID, value := args[0], args[1], args[2]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var result runConfigOptions
	if err := client.Call(context.Background(), "run.setConfigOption", map[string]any{"runId": runID, "configId": configID, "value": value}, &result); err != nil {
		fmt.Fprintf(os.Stderr, "task set-option: %v\n", err)
		return 1
	}
	fmt.Printf("set %s = %s on run %s\n", configID, value, runID)
	printConfigOptions(runID, result.Options)
	return 0
}

// printConfigOptions renders an option list as id/name/category/value
// rows (tab-separated, same convention as `task new`/`task ls` output),
// or the explanatory note when there's nothing to show.
func printConfigOptions(runID string, options []configOption) {
	if len(options) == 0 {
		fmt.Printf("run %s: no config options (provider does not support this, or no live session)\n", runID)
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, o := range options {
		value := "-"
		if len(o.CurrentValue) > 0 {
			value = string(o.CurrentValue)
		}
		category := "-"
		if o.Category != "" {
			category = o.Category
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", o.ConfigID, o.Name, category, value)
	}
	w.Flush()
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
