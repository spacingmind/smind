package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

const cmdTaskWaitUsage = "usage: smind task wait <runId|item:ID> [--timeout <duration>]"

// cmdTaskWait blocks until a run is terminal and reports the outcome in its
// exit code: 0 done, 1 error, 3 stopped, 4 done at a cap (needs-continue),
// 5 --timeout expired (run left running), 6 error from a provider rate limit
// (the output says when it resets).
func cmdTaskWait(args []string) int {
	var runID string
	var timeout time.Duration
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--timeout":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskWaitUsage)
				return 2
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil || d <= 0 {
				fmt.Fprintln(os.Stderr, cmdTaskWaitUsage)
				return 2
			}
			timeout = d
		case strings.HasPrefix(a, "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
			if err != nil || d <= 0 {
				fmt.Fprintln(os.Stderr, cmdTaskWaitUsage)
				return 2
			}
			timeout = d
		case strings.HasPrefix(a, "-"), runID != "":
			fmt.Fprintln(os.Stderr, cmdTaskWaitUsage)
			return 2
		default:
			runID = a
		}
	}
	if runID == "" {
		fmt.Fprintln(os.Stderr, cmdTaskWaitUsage)
		return 2
	}
	// item:<id> is what `task send --detach` prints for a prompt that was
	// queued instead of started: wait for it to be delivered, then for its run.
	var itemID int64
	if rest, ok := strings.CutPrefix(runID, "item:"); ok {
		id, err := parseInt64(rest)
		if err != nil || id <= 0 {
			fmt.Fprintf(os.Stderr, "task wait: invalid queue item %q (want item:<number>)\n%s\n", runID, cmdTaskWaitUsage)
			return 2
		}
		itemID, runID = id, ""
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx := sigCtx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(sigCtx, timeout)
		defer cancel()
	}

	if itemID != 0 {
		rid, code := waitForQueueItem(ctx, sigCtx, client, itemID, timeout)
		if rid == "" {
			return code
		}
		runID = rid
	}

	var finalText strings.Builder
	permIDs := make(chan string, 64)
	permDone := make(chan struct{})
	go func() {
		defer close(permDone)
		announced := map[string]bool{}
		for range permIDs {
			pending, err := fetchPendingPermissions(ctx, client, runID)
			if err != nil {
				continue
			}
			for _, p := range pending {
				if announced[p.RequestID] {
					continue
				}
				announced[p.RequestID] = true
				fmt.Fprintf(os.Stderr, "run %s is waiting for a permission: smind task approve %s %s  |  smind task deny %s %s\n",
					runID, runID, p.RequestID, runID, p.RequestID)
			}
		}
	}()

	var result attachResult
	streamErr := client.CallStream(ctx, "run.attach", map[string]any{"runId": runID}, func(event string, params json.RawMessage) {
		switch event {
		case "chunk":
			var p chunkEventParams
			if json.Unmarshal(params, &p) == nil {
				finalText.WriteString(p.Text)
			}
		case "tool_call":
			finalText.Reset()
		case "permission_request":
			var p struct {
				RequestID string `json:"requestId"`
			}
			if json.Unmarshal(params, &p) == nil && p.RequestID != "" {
				select {
				case permIDs <- p.RequestID:
				default:
				}
			}
		}
	}, &result)
	close(permIDs)
	<-permDone

	cancelled := streamErr != nil && (errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, context.DeadlineExceeded))
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	row, found := fetchRunRowWith(ctx2, client, runID)
	if streamErr != nil && !cancelled {
		// run.attach reports a run that ended stopped/error as an RPC error
		// too, so only an error with no terminal run behind it is fatal
		// (unknown run id, daemon failure).
		if !found || (row.Status != "done" && row.Status != "error" && row.Status != "stopped") {
			fmt.Fprintf(os.Stderr, "task wait: %v\n", streamErr)
			return 1
		}
	}

	if cancelled {
		if sigCtx.Err() != nil {
			fmt.Fprintf(os.Stderr, "detached -- run %s is still running\n", runID)
			return 0
		}
		status := "running"
		if found && row.Status != "" {
			status = row.Status
		}
		fmt.Fprintf(os.Stderr, "timed out after %s; run %s is still %s\n", timeout, runID, status)
		return 5
	}
	if !found {
		fmt.Fprintf(os.Stderr, "run %s not found\n", runID)
		return 1
	}
	fallbackID := int64(0)
	if row.Status == "error" {
		fallbackID = fetchFallbackProfileID(ctx2, client, runID)
	}
	fmt.Println(renderWaitReport(row, finalText.String(), time.Now(), fallbackID))
	return waitExitCode(row)
}

// fetchFallbackProfileID reads run.logs' fallbackProfileId (ADR-0041 F2: set
// only for a rate_limited run whose profile names a fallback); 0 on any
// failure or when absent.
func fetchFallbackProfileID(ctx context.Context, client *wsclient.Client, runID string) int64 {
	var result runLogsResult
	if err := client.Call(ctx, "run.logs", map[string]any{"runId": runID, "tail": 1}, &result); err != nil {
		return 0
	}
	return result.FallbackProfileID
}

// renderWaitReport is renderWaitSummary plus, for a rate-limited run whose
// profile names a fallback, a hint line on how to start the work there.
func renderWaitReport(row runSummaryRow, finalText string, now time.Time, fallbackProfileID int64) string {
	out := renderWaitSummary(row, finalText, now)
	if fallbackProfileID != 0 {
		out += fmt.Sprintf("\nfallback: profile %d (use: smind task chat new <task>, then task send --chat ...)", fallbackProfileID)
	}
	return out
}

// fetchRunRowWith reads the run's row from run.list.
func fetchRunRowWith(ctx context.Context, client *wsclient.Client, runID string) (runSummaryRow, bool) {
	var rows []runSummaryRow
	if err := client.Call(ctx, "run.list", map[string]any{}, &rows); err != nil {
		return runSummaryRow{}, false
	}
	for _, r := range rows {
		if r.ID == runID {
			return r, true
		}
	}
	return runSummaryRow{}, false
}

// waitExitCode maps a terminal run to cmdTaskWait's exit code.
func waitExitCode(row runSummaryRow) int {
	switch row.Status {
	case "done":
		if row.Disposition == "needs-continue" {
			return 4
		}
		return 0
	case "stopped":
		return 3
	case "error":
		if row.FailureKind == "rate_limited" {
			return 6
		}
		return 1
	default:
		return 1
	}
}

// formatDuration renders d as 1h02m05s, 3m12s, 12s or 850ms.
func formatDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm%02ds", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d >= time.Second:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

var manyNewlines = regexp.MustCompile(`\n{3,}`)

const waitFinalTextMax = 600

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// renderWaitSummary is the stdout report of a finished run.
func renderWaitSummary(row runSummaryRow, finalText string, now time.Time) string {
	outcome := row.Status
	if row.Disposition == "needs-continue" {
		outcome += " (needs-continue)"
	}
	stop := ""
	if row.StopReason != "" {
		stop = " (" + row.StopReason + ")"
	}
	end := now
	if row.FinishedAt != nil {
		end = *row.FinishedAt
	}
	dur := formatDuration(end.Sub(row.StartedAt))

	model := row.Model
	if model == "" {
		if row.Usage != nil && row.Usage.Model != nil && *row.Usage.Model != "" {
			model = "(" + *row.Usage.Model + ")"
		} else {
			model = "default"
		}
	}

	usage := "no usage reported"
	if row.Usage != nil {
		var parts []string
		if row.Usage.InputTokens != nil {
			parts = append(parts, fmt.Sprintf("%d in", *row.Usage.InputTokens))
		}
		if row.Usage.OutputTokens != nil {
			parts = append(parts, fmt.Sprintf("%d out", *row.Usage.OutputTokens))
		}
		if len(parts) > 0 {
			usage = strings.Join(parts, " / ") + " tokens"
		}
		if row.Usage.CostUSD != nil {
			cost := fmt.Sprintf("$%.2f", *row.Usage.CostUSD)
			if len(parts) > 0 {
				usage += " / " + cost
			} else {
				usage = cost
			}
		}
	}

	git := "git state unknown"
	if row.BaseSHA != nil && row.HeadSHA != nil {
		if *row.BaseSHA == *row.HeadSHA {
			git = "no new commits"
		} else {
			git = fmt.Sprintf("head %s (base %s)", shortSHA(*row.HeadSHA), shortSHA(*row.BaseSHA))
		}
	}

	// A failed run's last message is often the agent's own "[error] ..." line,
	// which the error line below already carries (the run's Err leads with it).
	if row.Status == "error" && strings.HasPrefix(strings.TrimSpace(finalText), "[error]") {
		finalText = ""
	}
	who := row.ID
	if row.TaskID > 0 {
		who = fmt.Sprintf("%s (task #%d)", row.ID, row.TaskID)
	}
	out := fmt.Sprintf("run %s %s%s after %s; model %s; %s; %s.", who, outcome, stop, dur, model, usage, git)
	if row.Status == "error" && row.FailureKind == "rate_limited" {
		out += "\n" + rateLimitedText(row.FailureWindow, row.FailureResetAt, row.FailureResetInferred, now)
	}
	if text := trimFinalText(finalText); text != "" {
		out += "\n\n" + text
	}
	if row.Status == "error" {
		out += "\nerror: " + strings.Join(strings.Fields(row.Err), " ")
	}
	return out
}

func trimFinalText(s string) string {
	s = strings.TrimSpace(manyNewlines.ReplaceAllString(s, "\n\n"))
	r := []rune(s)
	if len(r) > waitFinalTextMax {
		return string(r[:waitFinalTextMax]) + "…"
	}
	return s
}
