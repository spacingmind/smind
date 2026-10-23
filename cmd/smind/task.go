package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
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
// straight into `task approve <runId> <requestId> <optionId>`. The hint
// includes the runId so it is copy-pasteable as-is: cmdTaskApprove's real
// usage is `smind task approve <runId> [requestId] [optionId]` -- a hint
// printing only the requestId would paste into nothing.
func renderPermissionRequest(runID, requestID, summary string, options []permissionOptionParams, detail *permissionDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[permission] %s (request %s)\n", summary, requestID)
	b.WriteString(renderPermissionDetail(detail))
	for _, o := range options {
		fmt.Fprintf(&b, "  - %s: %s (%s)\n", o.ID, o.Label, o.Kind)
	}
	fmt.Fprintf(&b, "  -> approve: smind task approve %s %s\n", runID, requestID)
	fmt.Fprintf(&b, "  -> deny:    smind task deny %s %s\n", runID, requestID)
	return b.String()
}

// renderPermissionResolved formats how a permission request was resolved --
// distinguishing a real human decision from an auto-accept or a
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
	Detail     *permissionDetail        `json:"detail,omitempty"`
	OptionID   string                   `json:"optionId,omitempty"`
	Reason     string                   `json:"reason,omitempty"`
	toolCallEventParams
	rawEventParams
}

// runLogsResult is run.logs's terminal result.
type runLogsResult struct {
	RunID       string `json:"runId"`
	Status      string `json:"status"`
	StopReason  string `json:"stopReason,omitempty"`
	Disposition string `json:"disposition,omitempty"`
	Err         string `json:"err,omitempty"`
	// FailureKind/Window/ResetAt mirror an error run's typed failure
	// (ADR-0041); absent when none. ResetAt is RFC 3339 UTC.
	FailureKind string `json:"failureKind"`
	Window      string `json:"window"`
	ResetAt     string `json:"resetAt"`
	// BaseSHA/HeadSHA/Dirty mirror the run's git provenance (O1), absent
	// while unknown.
	BaseSHA string `json:"baseSha,omitempty"`
	HeadSHA string `json:"headSha,omitempty"`
	Dirty   *bool  `json:"dirty,omitempty"`
	// FallbackProfileID (ADR-0041 F2): for a rate_limited run, the profile its
	// author named as the fallback; 0 = none.
	FallbackProfileID int64         `json:"fallbackProfileId,omitempty"`
	Events            []runLogEvent `json:"events"`
}

func cmdTask(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind task <new|ls|send|runs|attach|wait|logs|stop|permissions|approve|deny|mode|options|set-option|chat|queue> ...")
		return 2
	}
	switch args[0] {
	case "new":
		return cmdTaskNew(args[1:])
	case "ls":
		return cmdTaskList(args[1:])
	case "send":
		return cmdTaskSend(args[1:])
	case "runs":
		return cmdTaskRuns(args[1:])
	case "attach":
		return cmdTaskAttach(args[1:])
	case "wait":
		return cmdTaskWait(args[1:])
	case "logs":
		return cmdTaskLogs(args[1:])
	case "stop":
		return cmdTaskStop(args[1:])
	case "permissions":
		return cmdTaskPermissions(args[1:])
	case "approve":
		return cmdTaskApprove(args[1:])
	case "deny":
		return cmdTaskDeny(args[1:])
	case "mode":
		return cmdTaskMode(args[1:])
	case "options":
		return cmdTaskOptions(args[1:])
	case "set-option":
		return cmdTaskSetOption(args[1:])
	case "chat":
		return cmdTaskChat(args[1:])
	case "queue":
		return cmdTaskQueue(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind task: unknown subcommand %q\n", args[0])
		return 2
	}
}

// cmdTaskNewUsage is printed on any argument error in cmdTaskNew.
const cmdTaskNewUsage = "usage: smind task new <workspaceId> <title> [--space <spaceId>] [--parent <parentTaskId>] [--base <ref> | --branch <existingBranch> | --pr <number>]"

// cmdTaskNew accepts optional trailing flag pairs after the title:
// "--space <spaceId>", "--parent <parentTaskId>", and the start-point
// pickers "--base <ref>", "--branch <existingBranch>", "--pr <number>",
// in any order among themselves -- unlike cmdTaskLogs's flags, which can
// appear anywhere relative to its positional runId, these only need to
// work in this one documented position (title text comes last otherwise,
// so trailing flag pairs can be unambiguously stripped off before the
// remaining args are joined into the title). --base forks a NEW branch at
// that ref's commit (use origin/develop for a fresh remote base, the
// daemon fetches it first). --branch checks out an existing branch (e.g.
// an unmerged PR branch) with no new branch. --pr builds from a GitHub
// PR. At most one of the three may be given; the daemon's task.create
// enforces the same exclusivity.
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

	// Value kinds for the trailing flags, so one strip loop serves them
	// all: each flag knows whether its value is an int64 id, a ref or
	// branch name taken as-is, or a positive PR number.
	const (
		flagValueID = iota
		flagValueRef
		flagValuePR
	)
	flagKinds := map[string]int{
		"--space":  flagValueID,
		"--parent": flagValueID,
		"--base":   flagValueRef,
		"--branch": flagValueRef,
		"--pr":     flagValuePR,
	}
	rest := args[1:]
	var spaceID, parentID, prNum *int64
	var baseRef, branch string
	for len(rest) >= 2 {
		flag, value := rest[len(rest)-2], rest[len(rest)-1]
		kind, ok := flagKinds[flag]
		if !ok {
			break
		}
		switch kind {
		case flagValueID:
			id, err := parseInt64(value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "task new: invalid %s value %q: %v\n", flag, value, err)
				return 2
			}
			if flag == "--space" {
				spaceID = &id
			} else {
				parentID = &id
			}
		case flagValueRef:
			if flag == "--base" {
				baseRef = value
			} else {
				branch = value
			}
		case flagValuePR:
			n, err := parseInt64(value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "task new: invalid --pr value %q: %v\n", value, err)
				return 2
			}
			if n <= 0 {
				fmt.Fprintf(os.Stderr, "task new: invalid --pr value %q: must be > 0\n", value)
				return 2
			}
			prNum = &n
		}
		rest = rest[:len(rest)-2]
	}
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, cmdTaskNewUsage)
		return 2
	}
	title := strings.Join(rest, " ")

	startModes := 0
	if baseRef != "" {
		startModes++
	}
	if branch != "" {
		startModes++
	}
	if prNum != nil {
		startModes++
	}
	if startModes > 1 {
		fmt.Fprintln(os.Stderr, "task new: pass at most one of --base, --branch, --pr")
		return 2
	}

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
	if parentID != nil {
		params["parentTaskId"] = *parentID
	}
	if baseRef != "" {
		params["baseRef"] = baseRef
	}
	if branch != "" {
		params["branch"] = branch
	}
	if prNum != nil {
		params["prNumber"] = *prNum
	}

	var task store.Task
	err = client.Call(context.Background(), "task.create", params, &task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task new: %v\n", err)
		return 1
	}
	// First line stays "id<TAB>title<TAB>status" for existing parsers; the
	// branch and worktree path follow on their own labelled lines.
	fmt.Printf("%d\t%s\t%s\n", task.ID, task.Title, task.Status)
	if task.Branch != nil {
		fmt.Printf("branch\t%s\n", *task.Branch)
	}
	if task.WorktreePath != nil {
		fmt.Printf("worktree\t%s\n", *task.WorktreePath)
	}
	return 0
}

// cmdTaskListUsage is printed on any argument error in cmdTaskList.
const cmdTaskListUsage = "usage: smind task ls <workspaceId> [--parent <parentTaskId>]"

// cmdTaskList lists a workspace's tasks, optionally narrowed to one
// parent's direct children via --parent -- the CLI mirror of task.list's
// optional parentTaskId filter (omitted keeps today's flat, whole-workspace
// listing).
func cmdTaskList(args []string) int {
	var workspaceIDArg, parentArg string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--parent":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskListUsage)
				return 2
			}
			i++
			parentArg = args[i]
		case strings.HasPrefix(a, "--parent="):
			parentArg = strings.TrimPrefix(a, "--parent=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "task ls: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskListUsage)
			return 2
		case workspaceIDArg == "":
			workspaceIDArg = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskListUsage)
			return 2
		}
	}
	if workspaceIDArg == "" {
		fmt.Fprintln(os.Stderr, cmdTaskListUsage)
		return 2
	}
	workspaceID, err := parseInt64(workspaceIDArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task ls: invalid workspaceId %q: %v\n", workspaceIDArg, err)
		return 2
	}
	var parentID int64
	if parentArg != "" {
		parentID, err = parseInt64(parentArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task ls: invalid --parent value %q: %v\n", parentArg, err)
			return 2
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{"workspaceId": workspaceID}
	if parentArg != "" {
		params["parentTaskId"] = parentID
	}
	var tasks []store.Task
	err = client.Call(context.Background(), "task.list", params, &tasks)
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
const cmdTaskSendUsage = "usage: smind task send <taskId> <provider> <prompt> [--chat <chatId>] [--mode <permissionMode>] [--auto-accept] [--model <model>] [--when-busy reject|queue|interrupt] [--thinking off|standard|extended] [--detach] [--verbose]"

// unpinnedModelNote is the stderr note printed when a claude-native run
// starts without --model. With a configured default (config
// providers.claude-native.defaultModel, reported by provider.list) it says
// which model the run will use; without one the Claude CLI runs its own
// default for the login, which may be a costly one, so the note says how to
// pin. "" for every other case, including the explicit --model default (a
// deliberate choice of the provider default).
func unpinnedModelNote(provider, model, configuredDefault string) string {
	if provider != string(taskrunner.ProviderClaudeNative) || model != "" {
		return ""
	}
	if configuredDefault != "" {
		return fmt.Sprintf("note: no --model given; using the configured default model %s (providers.%s.defaultModel). Override with --model.\n", configuredDefault, provider)
	}
	return "note: no --model given; claude-native uses the Claude CLI's own default model for this login (it may be Opus). Pin one with --model sonnet|haiku|opus or a full id.\n"
}

// providerDefaultModel is provider's configured default model as provider.list
// reports it, "" when none is set or the lookup fails (the note then falls
// back to the unconfigured wording).
func providerDefaultModel(ctx context.Context, client *wsclient.Client, provider string) string {
	var res struct {
		Providers []struct {
			ID           string `json:"id"`
			DefaultModel string `json:"defaultModel"`
		} `json:"providers"`
	}
	if err := client.Call(ctx, "provider.list", nil, &res); err != nil {
		return ""
	}
	for _, p := range res.Providers {
		if p.ID == provider {
			return p.DefaultModel
		}
	}
	return ""
}

// approvalPolicyRemovedMsg is printed for the removed --approval-policy
// flag (ADR-0019): a hard error pointing at the replacement.
const approvalPolicyRemovedMsg = "--approval-policy was removed; use --mode <mode> (the provider's own permission mode, e.g. acceptEdits, plan or bypassPermissions for claude-native) and --auto-accept for ACP providers"

// cmdTaskSend starts a run (via run.start, which returns as soon as the
// run is registered) and then streams it in the foreground exactly like
// `task attach` would, printing the runId first so a detached user can
// still find it. See streamRun for the actual streaming/detach behavior;
// run.start's decoupling from run.attach (see internal/wsapi/handlers.go's
// handleRunStart) is what makes Ctrl+C here detach instead of stopping the
// run.
//
// --mode is passed through to run.start's permissionMode field -- one of
// the provider's own permission mode ids (ADR-0019; the provider's
// default when absent) -- and --auto-accept to autoAccept (ACP providers
// only: approve every permission prompt).
//
// --chat targets the run at one of the task's chats (ADR-0016 P4) and is
// likewise passed through to run.start's chatId field; omitted, the task's
// default chat is used, so pre-chats invocations keep working unchanged.
//
// --thinking (off, standard or extended; Claude-only) is passed through to
// run.start's thinkingLevel field and omitted when not given. --detach
// prints the runId alone on stdout and returns as soon as the run has
// started, without streaming it.
func cmdTaskSend(args []string) int {
	// Parsed by hand for the same reason as cmdTaskLogs: the prompt is
	// free-form positional text, so stdlib flag parsing can't reliably
	// separate it from flags.
	var taskIDArg, provider, mode, chatArg, whenBusy, model string
	var autoAccept, detach, verbose bool
	var thinking string
	var promptParts []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--detach":
			detach = true
		case a == "--verbose":
			verbose = true
		case a == "--thinking":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
				return 2
			}
			i++
			thinking = args[i]
		case strings.HasPrefix(a, "--thinking="):
			thinking = strings.TrimPrefix(a, "--thinking=")
		case a == "--when-busy":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
				return 2
			}
			i++
			whenBusy = args[i]
		case strings.HasPrefix(a, "--when-busy="):
			whenBusy = strings.TrimPrefix(a, "--when-busy=")
		case a == "--chat":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
				return 2
			}
			i++
			chatArg = args[i]
		case strings.HasPrefix(a, "--chat="):
			chatArg = strings.TrimPrefix(a, "--chat=")
		case a == "--mode":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
				return 2
			}
			i++
			mode = args[i]
		case strings.HasPrefix(a, "--mode="):
			mode = strings.TrimPrefix(a, "--mode=")
		case a == "--model":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskSendUsage)
				return 2
			}
			i++
			model = args[i]
		case strings.HasPrefix(a, "--model="):
			model = strings.TrimPrefix(a, "--model=")
		case a == "--auto-accept":
			autoAccept = true
		case a == "--approval-policy" || strings.HasPrefix(a, "--approval-policy="):
			fmt.Fprintf(os.Stderr, "task send: %s\n", approvalPolicyRemovedMsg)
			return 2
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
	var chatID int64
	if chatArg != "" {
		chatID, err = parseInt64(chatArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task send: invalid --chat value %q: %v\n", chatArg, err)
			return 2
		}
	}
	switch thinking {
	case "", "off", "standard", "extended":
	default:
		fmt.Fprintf(os.Stderr, "task send: invalid --thinking %q (want off, standard, or extended)\n", thinking)
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
	if mode != "" {
		params["permissionMode"] = mode
	}
	if autoAccept {
		params["autoAccept"] = true
	}
	// --model (ADR-0025): omitted leaves the provider's default. Validated
	// by the daemon against the provider's catalog before anything spawns.
	if model != "" {
		params["model"] = model
	}
	if thinking != "" {
		params["thinkingLevel"] = thinking
	}
	if chatID != 0 {
		params["chatId"] = chatID
	}
	// --when-busy is ADR-0021 §2's busy-chat delivery mode; the CLI
	// default is reject (today's behavior) unless the flag says otherwise.
	if whenBusy != "" {
		switch whenBusy {
		case "reject", "queue", "interrupt":
			params["whenBusy"] = whenBusy
		default:
			fmt.Fprintf(os.Stderr, "task send: invalid --when-busy %q (want reject, queue, or interrupt)\n", whenBusy)
			return 2
		}
	}
	if provider == string(taskrunner.ProviderClaudeNative) && model == "" {
		fmt.Fprint(os.Stderr, unpinnedModelNote(provider, model, providerDefaultModel(ctx, client, provider)))
	}
	// Right after `task new` the workspace setup script may still be
	// running, which rejects a plain run.start. With no explicit
	// --when-busy, ask for queue ourselves (ADR-0024 §2); a task.get
	// failure is ignored and the send proceeds as before.
	if whenBusy == "" {
		var t store.Task
		if err := client.Call(ctx, "task.get", map[string]any{"id": taskID}, &t); err == nil && t.SetupStatus == "running" {
			params["whenBusy"] = "queue"
			fmt.Fprintln(os.Stderr, "note: the task's setup script is still running; queued, it starts when setup finishes (override with --when-busy reject)")
		}
	}
	var start struct {
		RunID       string `json:"runId"`
		Queued      bool   `json:"queued"`
		QueueItemID int64  `json:"queueItemId"`
	}
	err = client.Call(ctx, "run.start", params, &start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task send: %v\n", err)
		return 1
	}
	if start.Queued {
		fmt.Fprintf(os.Stderr, "queued as item %d -- it runs when the chat's current run finishes (smind task queue ls to watch, task queue cancel %d to undo)\n", start.QueueItemID, start.QueueItemID)
		if detach {
			// stdout carries the reference a script can hand to `task wait`.
			fmt.Printf("item:%d\n", start.QueueItemID)
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "run %s started\n", start.RunID)
	if detach {
		fmt.Println(start.RunID)
		return 0
	}

	return streamRun(ctx, client, start.RunID, viewOptions{verbose: verbose, autoAccept: autoAccept})
}

// cmdTaskAttachUsage is printed on any argument error in cmdTaskAttach.
const cmdTaskAttachUsage = "usage: smind task attach <runId> [--verbose]"

// cmdTaskAttach streams an existing run's output, standalone -- the same
// run.attach mechanics as `task send`'s foreground streaming, minus the
// run.start that kicks it off. On an already-finished run, run.attach's
// own backfill-then-immediate-terminal behavior (see
// internal/runs.Registry.Subscribe) means this prints history and exits
// cleanly rather than hanging.
func cmdTaskAttach(args []string) int {
	var runID string
	var verbose bool
	for _, a := range args {
		switch {
		case a == "--verbose":
			verbose = true
		case strings.HasPrefix(a, "-"), runID != "":
			fmt.Fprintln(os.Stderr, cmdTaskAttachUsage)
			return 2
		default:
			runID = a
		}
	}
	if runID == "" {
		fmt.Fprintln(os.Stderr, cmdTaskAttachUsage)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return streamRun(ctx, client, runID, viewOptions{verbose: verbose, autoAccept: runAutoAccept(ctx, client, runID)})
}

// runAutoAccept reports whether run runID auto-approves its permission
// prompts, so a viewer attaching to it can print each as one line. Best
// effort: any failure is false, which only means the full blocks are printed.
func runAutoAccept(ctx context.Context, client *wsclient.Client, runID string) bool {
	var rows []runSummaryRow
	if err := client.Call(ctx, "run.list", map[string]any{}, &rows); err != nil {
		return false
	}
	for _, r := range rows {
		if r.ID == runID {
			return r.AutoAccept
		}
	}
	return false
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
func streamRun(ctx context.Context, client *wsclient.Client, runID string, opts viewOptions) int {
	var result attachResult
	v := newRunViewer(runID, opts)
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
			v.text(p.Text)
		case "tool_call":
			var p toolCallEventParams
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			v.toolCall(p)
		case "raw":
			var p rawEventParams
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			v.raw(p)
		case "permission_request":
			var p struct {
				RequestID string                   `json:"requestId"`
				Summary   string                   `json:"summary"`
				Options   []permissionOptionParams `json:"options"`
				Detail    *permissionDetail        `json:"detail"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			v.permissionRequest(p.RequestID, p.Summary, p.Options, p.Detail)
		case "permission_resolved":
			var p struct {
				RequestID string `json:"requestId"`
				OptionID  string `json:"optionId"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return
			}
			v.permissionResolved(p.RequestID, p.OptionID, p.Reason)
		}
	}, &result)
	v.flush()
	fmt.Println()

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "detached -- run %s is still running; see `smind task logs %s`\n", runID, runID)
			return 0
		}
		fmt.Fprintf(os.Stderr, "run %s: %v\n", runID, err)
		return 1
	}
	fmt.Fprint(os.Stderr, renderDone(runID, result.StopReason))
	return 0
}

// cmdTaskLogsUsage is printed on any argument error -- kept as a constant
// (rather than inline at each call site) so it's identical whichever
// invalid-argument branch in cmdTaskLogs triggers it.
const cmdTaskLogsUsage = "usage: smind task logs <runId> [-f|--follow] [--tail N] [--verbose]"

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
	var follow, verbose bool
	var tail int
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--follow":
			follow = true
		case a == "--verbose":
			verbose = true
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
		return streamRun(ctx, client, runID, viewOptions{verbose: verbose, autoAccept: runAutoAccept(ctx, client, runID)})
	}

	var result runLogsResult
	err = client.Call(context.Background(), "run.logs", map[string]any{"runId": runID, "tail": tail}, &result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task logs: %v\n", err)
		return 1
	}
	printRunLogs(result, viewOptions{verbose: verbose})
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

func printRunLogs(result runLogsResult, opts viewOptions) {
	v := newRunViewer(result.RunID, opts)
	// The listing already holds every outcome, so a request that was
	// auto-accepted collapses to one line without any holding: index the
	// resolutions first, and skip the resolution line a collapsed request
	// already covered.
	autoAccepted := map[string]string{}
	for _, e := range result.Events {
		if e.Type == "permission_resolved" && e.Reason == permissionReasonAutoAccept {
			autoAccepted[e.RequestID] = e.OptionID
		}
	}
	for _, e := range result.Events {
		switch e.Type {
		case "chunk", "user_message", "thinking":
			v.text(e.Text)
		case "tool_call":
			v.toolCall(e.toolCallEventParams)
		case "raw":
			v.raw(e.rawEventParams)
		case "permission_request":
			if optionID, ok := autoAccepted[e.RequestID]; ok {
				v.autoAcceptedLine(e.Summary, e.Detail, optionID)
				continue
			}
			v.mu.Lock()
			v.print(renderPermissionRequest(result.RunID, e.RequestID, e.Summary, e.Options, e.Detail))
			v.mu.Unlock()
		case "permission_resolved":
			if _, ok := autoAccepted[e.RequestID]; ok {
				continue
			}
			v.mu.Lock()
			v.print(renderPermissionResolved(e.RequestID, e.OptionID, e.Reason))
			v.mu.Unlock()
		}
	}
	fmt.Println()

	switch result.Status {
	case "done":
		if result.Disposition != "" {
			fmt.Fprintf(os.Stderr, "run %s: done (%s) -- %s\n", result.RunID, result.StopReason, result.Disposition)
		} else {
			fmt.Fprintf(os.Stderr, "run %s: done (%s)\n", result.RunID, result.StopReason)
		}
		if note := stopNote(result.StopReason); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
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
	Detail    *permissionDetail
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
			pending = append(pending, pendingPermission{RequestID: e.RequestID, Summary: e.Summary, Options: e.Options, Detail: e.Detail})
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
		fmt.Print(renderPermissionRequest(runID, p.RequestID, p.Summary, p.Options, p.Detail))
	}
	return 0
}

// cmdTaskApproveUsage is printed on any argument error in cmdTaskApprove.
const cmdTaskApproveUsage = "usage: smind task approve <runId> [requestId] [optionId]"

// cmdTaskApprove answers a run's pending permission request via
// run.respondPermission -- closing the gap where a run whose provider
// permission mode escalated a request has no CLI-side way to be unblocked
// at all short of the 5-minute timeout-deny (runs.defaultPermissionTimeout).
//
// requestId defaults to the oldest still-pending request (there is
// normally only one at a time -- a run blocks on Decide before issuing
// another tool call). optionId defaults to the first option whose Kind is
// "allow_once"/"allow_always", mirroring
// internal/runs.firstOptionByKind's own selection so `task approve
// <runId>` with no further arguments does the obvious thing.
func cmdTaskApprove(args []string) int {
	return cmdTaskDecide("approve", cmdTaskApproveUsage, true, 3, args)
}

// cmdTaskDenyUsage is printed on any argument error in cmdTaskDeny.
const cmdTaskDenyUsage = "usage: smind task deny <runId> [requestId]"

// cmdTaskDeny is the human-only counterpart of cmdTaskApprove: it rejects a
// run's pending permission request with the request's own reject option.
// CLI only -- there is deliberately no MCP deny tool (ADR-0017 #1).
func cmdTaskDeny(args []string) int {
	return cmdTaskDecide("deny", cmdTaskDenyUsage, false, 2, args)
}

// isHelpArg reports whether a CLI argument asks for usage.
func isHelpArg(a string) bool { return a == "-h" || a == "--help" || a == "help" }

// cmdTaskDecide answers a pending permission request, picking the first
// allow option (allow=true) or the first reject option (allow=false).
func cmdTaskDecide(verb, usage string, allow bool, maxArgs int, args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		fmt.Println(usage)
		return 0
	}
	if len(args) < 1 || len(args) > maxArgs {
		fmt.Fprintln(os.Stderr, usage)
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
		fmt.Fprintf(os.Stderr, "task %s: %v\n", verb, err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintf(os.Stderr, "task %s: no pending permission request\n", verb)
		return 1
	}

	var target *pendingPermission
	if len(args) >= 2 {
		requestID := args[1]
		for i := range pending {
			if pending[i].RequestID == requestID {
				target = &pending[i]
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "task %s: request %q is not pending\n", verb, requestID)
			return 1
		}
	} else {
		target = &pending[0]
	}

	optionID := ""
	if len(args) == 3 {
		// The human named the option: any of the request's options is valid.
		ids := make([]string, 0, len(target.Options))
		for _, o := range target.Options {
			ids = append(ids, o.ID)
			if o.ID == args[2] {
				optionID = o.ID
			}
		}
		if optionID == "" {
			fmt.Fprintf(os.Stderr, "task %s: request %q has no option %q; valid: %s\n", verb, target.RequestID, args[2], strings.Join(ids, ", "))
			return 1
		}
	}
	for _, o := range target.Options {
		if optionID != "" {
			break
		}
		isAllow := o.Kind == "allow_once" || o.Kind == "allow_always"
		isReject := o.Kind == "reject_once" || o.Kind == "reject_always"
		if (allow && isAllow) || (!allow && isReject) {
			optionID = o.ID
			break
		}
	}
	if optionID == "" {
		kind := "allow"
		if !allow {
			kind = "reject"
		}
		fmt.Fprintf(os.Stderr, "task %s: request %q has no %s option: %+v\n", verb, target.RequestID, kind, target.Options)
		return 1
	}

	err = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": target.RequestID, "optionId": optionID,
	}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task %s: %v\n", verb, err)
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

// cmdTaskChatUsage is printed when `task chat` is invoked with no (or an
// unknown) subcommand.
const cmdTaskChatUsage = "usage: smind task chat <ls|new|rename|archive> ..."

// cmdTaskChat dispatches the chat management subcommands (ADR-0016 P4):
// ls/new operate per task, rename/archive per chat id.
func cmdTaskChat(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cmdTaskChatUsage)
		return 2
	}
	switch args[0] {
	case "ls":
		return cmdTaskChatLs(args[1:])
	case "new":
		return cmdTaskChatNew(args[1:])
	case "rename":
		return cmdTaskChatRename(args[1:])
	case "archive":
		return cmdTaskChatArchive(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind task chat: unknown subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, cmdTaskChatUsage)
		return 2
	}
}

// cmdTaskChatLsUsage is printed on any argument error in cmdTaskChatLs.
const cmdTaskChatLsUsage = "usage: smind task chat ls <taskId> [--all]"

// chatRow is one entry of a chat.list result -- the CLI-side mirror of
// store.Chat's wire shape (bare Go field names; see taskChatFromWire).
type chatRow struct {
	ID         int64
	TaskID     int64
	Title      string
	Provider   *string
	ArchivedAt *time.Time
}

// cmdTaskChatLs lists a task's chats, active only by default (--all
// includes archived ones). Provider prints "-" while the chat is unbound
// (no run yet), and ARCHIVED prints "yes" for archived chats so the
// column is greppable either way.
func cmdTaskChatLs(args []string) int {
	var taskIDArg string
	var all bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--all":
			all = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "task chat ls: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskChatLsUsage)
			return 2
		case taskIDArg == "":
			taskIDArg = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskChatLsUsage)
			return 2
		}
	}
	if taskIDArg == "" {
		fmt.Fprintln(os.Stderr, cmdTaskChatLsUsage)
		return 2
	}
	taskID, err := parseInt64(taskIDArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task chat ls: invalid taskId %q: %v\n", taskIDArg, err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{"taskId": taskID}
	if all {
		params["includeArchived"] = true
	}
	var chats []chatRow
	if err := client.Call(context.Background(), "chat.list", params, &chats); err != nil {
		fmt.Fprintf(os.Stderr, "task chat ls: %v\n", err)
		return 1
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tPROVIDER\tARCHIVED")
	for _, c := range chats {
		provider := "-"
		if c.Provider != nil {
			provider = *c.Provider
		}
		archived := "no"
		if c.ArchivedAt != nil {
			archived = "yes"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", c.ID, c.Title, provider, archived)
	}
	tw.Flush()
	return 0
}

// cmdTaskChatNewUsage is printed on any argument error in cmdTaskChatNew.
const cmdTaskChatNewUsage = "usage: smind task chat new <taskId> [title]"

// cmdTaskChatNew creates a chat under taskId; title is optional (the
// daemon stores an omitted title as a genuinely untitled chat, never
// defaulting it to "Chat" -- see handleChatCreate). Extra words are
// joined into the title, same convention as `task new`.
func cmdTaskChatNew(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, cmdTaskChatNewUsage)
		return 2
	}
	taskID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task chat new: invalid taskId %q: %v\n", args[0], err)
		return 2
	}
	title := strings.Join(args[1:], " ")

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var chat chatRow
	if err := client.Call(context.Background(), "chat.create", map[string]any{"taskId": taskID, "title": title}, &chat); err != nil {
		fmt.Fprintf(os.Stderr, "task chat new: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\n", chat.ID, chat.Title)
	return 0
}

// cmdTaskChatRenameUsage is printed on any argument error in cmdTaskChatRename.
const cmdTaskChatRenameUsage = "usage: smind task chat rename <chatId> <title>"

// cmdTaskChatRename renames a chat; title words are joined, same as
// everywhere else.
func cmdTaskChatRename(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cmdTaskChatRenameUsage)
		return 2
	}
	chatID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task chat rename: invalid chatId %q: %v\n", args[0], err)
		return 2
	}
	title := strings.Join(args[1:], " ")

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var chat chatRow
	if err := client.Call(context.Background(), "chat.rename", map[string]any{"id": chatID, "title": title}, &chat); err != nil {
		fmt.Fprintf(os.Stderr, "task chat rename: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\n", chat.ID, chat.Title)
	return 0
}

// cmdTaskChatArchiveUsage is printed on any argument error in cmdTaskChatArchive.
const cmdTaskChatArchiveUsage = "usage: smind task chat archive <chatId>"

// cmdTaskChatArchive archives a chat. The daemon refuses while the chat
// has a running run (see handleChatArchive) -- that rejection reaches the
// user verbatim here.
func cmdTaskChatArchive(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, cmdTaskChatArchiveUsage)
		return 2
	}
	chatID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task chat archive: invalid chatId %q: %v\n", args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var chat chatRow
	if err := client.Call(context.Background(), "chat.archive", map[string]any{"id": chatID}, &chat); err != nil {
		fmt.Fprintf(os.Stderr, "task chat archive: %v\n", err)
		return 1
	}
	fmt.Printf("chat %d archived\n", chat.ID)
	return 0
}

// cmdTaskRunsUsage is printed on any argument error in cmdTaskRuns.
const cmdTaskRunsUsage = "usage: smind task runs <taskId> [--chat <chatId>]"

// runSummaryRow is one entry of a run.list result -- the CLI-side mirror
// of runs.RunSummary's wire shape (bare Go field names, same convention
// as chatRow; see internal/wsapi/chat_test.go's runStatusForTest).
type runSummaryRow struct {
	ID        string
	TaskID    int64
	ChatID    int64
	Provider  string
	Prompt    string
	Status    string
	StartedAt time.Time
	// Origin is who authored the run's prompt ("human", "orchestrator",
	// "agent", "child_event"; "" when unknown).
	Origin     string
	StopReason string
	// ThinkingLevel is the run's thinking level ("" when unspecified).
	ThinkingLevel string
	// AutoAccept is whether the run auto-approves its permission prompts.
	AutoAccept bool
	// Model is the requested model ("" = the provider's default); Usage.Model
	// is the model the agent reported using, when it reported one.
	Model string
	Usage *struct {
		Model        *string  `json:"model"`
		InputTokens  *int64   `json:"inputTokens"`
		OutputTokens *int64   `json:"outputTokens"`
		CostUSD      *float64 `json:"costUsd"`
	} `json:"usage"`
	FinishedAt *time.Time
	Err        string
	BaseSHA    *string
	HeadSHA    *string
	// Disposition is "needs-continue" for a run that ended done at a cap.
	Disposition string
	// FailureKind is "rate_limited" for a run that hit a provider quota;
	// FailureWindow names the window, FailureResetAt is when it resets.
	FailureKind    string
	FailureWindow  string
	FailureResetAt *time.Time
}

// runModelCell is a run's MODEL column: the requested model; when none was
// requested, the model the agent reported using in parentheses (that is what
// "Default" resolved to); "-" when neither is known.
func runModelCell(r runSummaryRow) string {
	if r.Model != "" {
		return r.Model
	}
	if r.Usage != nil && r.Usage.Model != nil && *r.Usage.Model != "" {
		return "(" + *r.Usage.Model + ")"
	}
	return "-"
}

// runStatusCell is a run's STATUS column: the status, and its disposition when
// it has one (a run "done" at a turn cap reads "done (needs-continue)": not
// finished). A failed run with an error text reads "error: <cause>"; a
// rate-limited one reads "error: rate limited (5h window), resets ...".
func runStatusCell(r runSummaryRow, now time.Time) string {
	status, disposition, errText := r.Status, r.Disposition, r.Err
	if status == "error" && r.FailureKind == "rate_limited" {
		return status + ": " + rateLimitedText(r.FailureWindow, r.FailureResetAt, now)
	}
	if status == "error" && strings.TrimSpace(errText) != "" {
		return status + ": " + runErrCause(errText)
	}
	if disposition == "" {
		return status
	}
	return status + " (" + disposition + ")"
}

// cmdTaskRuns lists a task's runs, most recent first (run.list's own
// ordering). --chat narrows it to one chat of the task. run.list filters
// server-side by chatId only, so the taskId restriction is applied here,
// client-side, over the same single response -- with --chat given, the
// chat already belongs to exactly one task, so the two filters agree.
func cmdTaskRuns(args []string) int {
	var taskIDArg, chatArg string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--chat":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskRunsUsage)
				return 2
			}
			i++
			chatArg = args[i]
		case strings.HasPrefix(a, "--chat="):
			chatArg = strings.TrimPrefix(a, "--chat=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "task runs: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskRunsUsage)
			return 2
		case taskIDArg == "":
			taskIDArg = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskRunsUsage)
			return 2
		}
	}
	if taskIDArg == "" {
		fmt.Fprintln(os.Stderr, cmdTaskRunsUsage)
		return 2
	}
	taskID, err := parseInt64(taskIDArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task runs: invalid taskId %q: %v\n", taskIDArg, err)
		return 2
	}
	var chatID int64
	if chatArg != "" {
		chatID, err = parseInt64(chatArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task runs: invalid --chat value %q: %v\n", chatArg, err)
			return 2
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	params := map[string]any{}
	if chatID != 0 {
		params["chatId"] = chatID
	}
	var all []runSummaryRow
	if err := client.Call(context.Background(), "run.list", params, &all); err != nil {
		fmt.Fprintf(os.Stderr, "task runs: %v\n", err)
		return 1
	}

	var rows []runSummaryRow
	needGit := false
	for _, r := range all {
		if r.TaskID != taskID {
			continue
		}
		rows = append(rows, r)
		if r.BaseSHA != nil && r.HeadSHA != nil && *r.BaseSHA != "" && *r.HeadSHA != "" {
			needGit = true
		}
	}
	worktree := ""
	if needGit {
		var task struct {
			WorktreePath *string
		}
		if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err == nil && task.WorktreePath != nil {
			worktree = *task.WorktreePath
		}
	}
	now := time.Now()

	writeRunsTable(os.Stdout, rows, worktree, now)
	return 0
}

// writeRunsTable prints the `task runs` table.
func writeRunsTable(w io.Writer, rows []runSummaryRow, worktree string, now time.Time) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCHAT\tPROVIDER\tORIGIN\tMODEL\tSTARTED\tDURATION\tUSAGE\tCOMMITS\tSTATUS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.ChatID, r.Provider, runOriginCell(r), runModelCell(r),
			runStartedCell(r.StartedAt, now), runDurationCell(r, now), runUsageCell(r), runCommitsCell(r, worktree),
			runStatusCell(r, now))
	}
	tw.Flush()
}

// runOriginCell is the ORIGIN column: who authored the run's prompt, "-" when unknown.
func runOriginCell(r runSummaryRow) string {
	if r.Origin == "" {
		return "-"
	}
	return r.Origin
}

// cmdTaskQueueUsage is printed on any argument error in the queue group.
const cmdTaskQueueUsage = "usage: smind task queue <ls --chat <chatId>|cancel <itemId>>"

// cmdTaskQueue dispatches the queue subcommands (ADR-0021 §8's CLI).
func cmdTaskQueue(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cmdTaskQueueUsage)
		return 2
	}
	switch args[0] {
	case "ls":
		return cmdTaskQueueLs(args[1:])
	case "cancel":
		return cmdTaskQueueCancel(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind task queue: unknown subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, cmdTaskQueueUsage)
		return 2
	}
}

// queueItemRow is one entry of a chat.queueList result -- the CLI-side
// mirror of the wire shape, same convention as chatRow.
type queueItemRow struct {
	ID           int64     `json:"ID"`
	Prompt       string    `json:"Prompt"`
	Source       string    `json:"Source"`
	Priority     int       `json:"Priority"`
	Status       string    `json:"Status"`
	RunID        *string   `json:"RunID"`
	CancelReason string    `json:"CancelReason"`
	CreatedAt    time.Time `json:"CreatedAt"`
}

// cmdTaskQueueLs lists a chat's queue items, all statuses, oldest first.
func cmdTaskQueueLs(args []string) int {
	var chatArg string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--chat":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskQueueUsage)
				return 2
			}
			i++
			chatArg = args[i]
		case strings.HasPrefix(a, "--chat="):
			chatArg = strings.TrimPrefix(a, "--chat=")
		default:
			fmt.Fprintln(os.Stderr, cmdTaskQueueUsage)
			return 2
		}
	}
	if chatArg == "" {
		fmt.Fprintln(os.Stderr, cmdTaskQueueUsage)
		return 2
	}
	chatID, err := parseInt64(chatArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task queue ls: invalid --chat value %q: %v\n", chatArg, err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var result struct {
		Items []queueItemRow `json:"items"`
	}
	if err := client.Call(context.Background(), "chat.queueList", map[string]any{"chatId": chatID}, &result); err != nil {
		fmt.Fprintf(os.Stderr, "task queue ls: %v\n", err)
		return 1
	}
	if len(result.Items) == 0 {
		fmt.Println("queue empty")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tSOURCE\tRUN\tPROMPT")
	for _, item := range result.Items {
		prompt := item.Prompt
		if len(prompt) > 40 {
			prompt = prompt[:40] + "..."
		}
		runID := "-"
		if item.RunID != nil && *item.RunID != "" {
			runID = *item.RunID
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Source, runID, prompt)
	}
	tw.Flush()
	return 0
}

// cmdTaskQueueCancel cancels a still-queued item by id. Cancelling a
// delivered or already-cancelled item is the daemon's error to surface.
func cmdTaskQueueCancel(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smind task queue cancel <itemId>")
		return 2
	}
	itemID, err := parseInt64(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "task queue cancel: invalid itemId %q: %v\n", args[0], err)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	if err := client.Call(context.Background(), "chat.queueCancel", map[string]any{"itemId": itemID}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "task queue cancel: %v\n", err)
		return 1
	}
	fmt.Printf("queue item %d cancelled\n", itemID)
	return 0
}
