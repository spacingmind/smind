package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// ProbeOptions configures ONE sandboxed arm run (ADR-0037). Launch is
// required and comes from sandbox.Prepare.
type ProbeOptions struct {
	Launch *sandbox.Launch
	// Attestation is the canary's proof that the sandbox confines a real CLI;
	// RunPromptProbe re-checks it before every arm start and spawns nothing
	// without a valid one.
	Attestation    sandbox.Attestation
	MaxTurns       int               // > 0 required
	CostCapUSD     float64           // > 0 required: passed as --max-budget-usd AND enforced by the arm.Meter
	OutputCapBytes int64             // 0 = no output cap
	Timeout        time.Duration     // > 0 required
	OnReport       func(ProbeReport) // optional; called exactly once, before RunPromptProbe returns (also on error)
}

// ProbeReport is what the arm run observed.
type ProbeReport struct {
	End               arm.End
	Turns             int
	PermissionDenials int
	CostUSD           float64
	InitTools         []string
	MCPServers        int // non-builtin
	Plugins           int // non-builtin
	StopReason        string
	// Survivors is how many processes of the arm outlived the kill pass.
	Survivors int
}

// ErrProbeEnded is returned (wrapped, with the End) when the arm ended
// anywhere but normally.
var ErrProbeEnded = errors.New("taskrunner: probe arm ended early")

// probeKillGrace is how long KillTree waits between TERM and KILL.
const probeKillGrace = 2 * time.Second

// RunPromptProbe runs ONE prompt as a sandboxed probe arm (ADR-0037 §3): the
// CLI is started through po.Launch (the launcher IS the CLI path, so nothing
// here resolves or widens the launch), with cwd = the arm dir, under the arm's
// caps. The task is only used for ids by callers; its worktree is never
// touched.
//
// Like RunPromptIsolated, events is closed before returning in every case and
// err != nil with a Done event of StopReason probe_<end> means the arm ended
// early. After the run, always: close the client, then kill the arm's whole
// process group; a survivor fails an otherwise normal run.
func (r *Runner) RunPromptProbe(ctx context.Context, taskID, chatID int64, provider Provider, prompt, model string, po ProbeOptions, events chan<- Event) (err error) {
	defer close(events)
	var rep ProbeReport
	defer func() {
		if po.OnReport != nil {
			po.OnReport(rep)
		}
	}()
	fail := func(e error) error { rep.End = arm.EndError; return e }

	if !r.isClaude(provider) {
		return fail(fmt.Errorf("%w (got %q)", ErrIsolationUnsupported, provider))
	}
	switch {
	case po.Launch == nil:
		return fail(errors.New("taskrunner: probe run requires a sandbox launch"))
	case !po.Launch.Prepared():
		return fail(errors.New("taskrunner: probe run requires a launch from sandbox.Prepare"))
	case po.MaxTurns <= 0:
		return fail(errors.New("taskrunner: probe run requires MaxTurns > 0"))
	case po.CostCapUSD <= 0:
		return fail(errors.New("taskrunner: probe run requires CostCapUSD > 0"))
	case po.Timeout <= 0:
		return fail(errors.New("taskrunner: probe run requires Timeout > 0"))
	}
	if aerr := po.Attestation.Require(time.Now()); aerr != nil {
		return fail(fmt.Errorf("taskrunner: probe arm refused: %w", aerr))
	}
	if !sandbox.Supported() {
		return fail(sandbox.ErrUnsupported)
	}
	if err := ValidateModelSyntax(model); err != nil {
		return fail(fmt.Errorf("taskrunner: %w", err))
	}

	// The launch's own options are the whole CLI shape; only the caps are
	// added. No WithEnv (Options made the only one), no extra dirs, MCP or
	// resume, no claudeLaunchSpec.
	opts := append(po.Launch.Options(),
		claudecode.WithMaxTurns(po.MaxTurns),
		claudecode.WithMaxBudgetUSD(po.CostCapUSD))
	if model != "" && model != ClaudeModelDefault {
		opts = append(opts, claudecode.WithModel(model))
	}
	client, cerr := r.newClaudeClient(po.Launch.ArmDir, opts...)
	if cerr != nil {
		// A half-started CLI may still have written its pidfile.
		_ = arm.KillTree(context.Background(), po.Launch.PidFile, po.Launch.ArmDir, probeKillGrace)
		return fail(fmt.Errorf("taskrunner: spawn probe claude code agent: %w", cerr))
	}
	defer func() {
		_ = client.Close()
		kerr := arm.KillTree(context.Background(), po.Launch.PidFile, po.Launch.ArmDir, probeKillGrace)
		if kerr == nil {
			return
		}
		// Whatever the end state, a cleanup that did not finish is surfaced: a
		// survivor, or a pid that is not this arm's CLI (never swallowed).
		var se *arm.SurvivorsError
		if errors.As(kerr, &se) {
			rep.Survivors = len(se.PIDs)
		}
		if rep.End == arm.EndDone {
			rep.End = arm.EndError
		}
		cleanup := fmt.Errorf("probe arm processes survived: %w", kerr)
		if errors.Is(kerr, arm.ErrNotArmProcess) {
			cleanup = fmt.Errorf("probe arm cleanup refused (processes may have survived): %w", kerr)
		}
		if err == nil {
			err = fmt.Errorf("taskrunner: %w", cleanup)
		} else {
			err = fmt.Errorf("%w; %w", err, cleanup)
		}
	}()

	tctx, cancelTimeout := context.WithTimeout(ctx, po.Timeout)
	defer cancelTimeout()
	runCtx, cancel := context.WithCancel(tctx)
	defer cancel()

	meter := arm.NewMeter(arm.Caps{MaxTurns: po.MaxTurns, CostUSD: po.CostCapUSD, OutputBytes: po.OutputCapBytes})
	var (
		mu       sync.Mutex
		violated string
		tripped  bool
		obs      sandbox.Observation
		// callTool remembers the tool name of each call id: the CLI's result
		// event for a call carries the id but not the name.
		callTool = map[string]string{}
	)
	updates := make(chan claudecode.Message)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for msg := range updates {
			if sys, ok := msg.(claudecode.SystemMessage); ok && sys.Subtype == "init" {
				mu.Lock()
				obs.InitTools, obs.MCPServers, obs.Plugins = sandbox.ParseInit(sys.Raw, po.Launch.ProjectionDir)
				if f := sandbox.CheckInit(obs); len(f) > 0 && !tripped {
					tripped, violated = true, "init reported a wrong tool surface: "+f[0].Detail
					cancel()
				}
				mu.Unlock()
				continue
			}
			for _, e := range claudeEvents(msg) {
				mu.Lock()
				if e.Type == EventTypeToolCall && !tripped {
					name := e.ToolName
					if name == "" {
						name = callTool[e.ToolCallID]
					} else if e.ToolCallID != "" {
						callTool[e.ToolCallID] = name
					}
					if !allowedProbeTool(name) {
						tripped, violated = true, "tool call "+safeToolName(name)
						cancel()
					}
				}
				if !tripped && meter.Output(len(e.Text)+len(e.ToolInput)+len(e.ToolResult)) == arm.EndOutput {
					cancel()
				}
				drop := tripped || meter.Tripped() != ""
				mu.Unlock()
				if drop {
					continue
				}
				select {
				case events <- e:
				case <-runCtx.Done():
				}
			}
		}
	}()

	result, perr := client.Prompt(runCtx, prompt, updates)
	<-forwardDone

	mu.Lock()
	rep.InitTools, rep.MCPServers, rep.Plugins = obs.InitTools, obs.MCPServers, obs.Plugins
	if perr == nil && !tripped && len(obs.InitTools) == 0 {
		// The real CLI always reports its tools; no report means nothing proves
		// the surface, so fail closed.
		tripped, violated = true, "init reported a wrong tool surface: no init message"
	}
	didTrip, reason := tripped, violated
	mu.Unlock()
	rep.Turns, rep.PermissionDenials, rep.StopReason = result.NumTurns, len(result.PermissionDenials), result.StopReason

	end := arm.EndDone
	var cause string
	switch {
	case didTrip:
		end, cause = arm.EndError, reason
	case meter.Tripped() == arm.EndOutput:
		end = arm.EndOutput
	case perr != nil && tctx.Err() != nil && ctx.Err() == nil:
		end = arm.EndTimeout
	case perr != nil && ctx.Err() != nil:
		end = arm.EndKilled
	case perr != nil:
		end, cause = arm.EndError, perr.Error()
	default:
		end, cause = probeResultEnd(result)
	}
	if perr == nil && !didTrip {
		// Feed the final usage: an unreported cost is charged at the cap.
		u := claudeUsage(result)
		var cost float64
		if u.CostUSD != nil {
			cost = *u.CostUSD
		}
		if meter.Usage(cost, u.Source != UsageSourceNotReported) == arm.EndCapped && end == arm.EndDone {
			end = arm.EndCapped
		}
	}
	rep.CostUSD, rep.End = meter.Cost(), end

	if end == arm.EndDone {
		r.sendUsage(ctx, events, claudeUsage(result))
		select {
		case events <- Event{Type: EventTypeDone, StopReason: result.StopReason, Raw: result}:
		case <-ctx.Done():
		}
		return nil
	}
	// Kill first: a slow consumer must not keep the arm alive while the final
	// Done event waits for a reader.
	_ = client.Close()
	r.sendFailedUsage(ctx, events, result)
	// Not selecting on ctx.Done: a cancelled caller (end killed) still reads
	// the final event, and select would pick the closed ctx at random. The
	// timer bounds the wait when nobody reads.
	select {
	case events <- Event{Type: EventTypeDone, StopReason: "probe_" + string(end)}:
	case <-time.After(2 * time.Second):
	}
	if didTrip {
		return fmt.Errorf("%w: %s", ErrIsolationViolated, reason)
	}
	if cause != "" {
		return fmt.Errorf("%w: %s: %s", ErrProbeEnded, end, clipBytes(cause, 200))
	}
	return fmt.Errorf("%w: %s", ErrProbeEnded, end)
}

func allowedProbeTool(name string) bool {
	for _, t := range sandbox.AllowedTools() {
		if t == name {
			return true
		}
	}
	return false
}

// probeResultEnd classifies a result message. The CLI enforces the turn and
// budget caps and reports them as error results whose terminal reason, stop
// reason or errors carry max_turns / max_budget; the free-text result is only
// consulted for error results (a model could otherwise write those words).
func probeResultEnd(res claudecode.ResultMessage) (arm.End, string) {
	text := strings.ToLower(res.TerminalReason + " " + res.StopReason + " " + strings.Join(res.Errors, " "))
	if res.IsError {
		text += " " + strings.ToLower(res.Result)
	}
	switch {
	case strings.Contains(text, "max_turns"):
		return arm.EndTurns, ""
	case strings.Contains(text, "max_budget"):
		return arm.EndCapped, ""
	case res.IsError:
		return arm.EndError, resultFailure(res)
	}
	return arm.EndDone, ""
}
