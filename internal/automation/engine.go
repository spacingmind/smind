package automation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

const (
	// CatchupGrace is how late a cron instant may fire (ADR-0044 section 2.1).
	CatchupGrace = 15 * time.Minute
	// cronDedupeFloor guards a cron instant from firing twice (a double tick,
	// a restart between the audit write and the next-run write) even when the
	// human set no dedupe window.
	cronDedupeFloor = 48 * time.Hour
	// orphanFireAfter: a fire row that never got a task is a failed run.
	orphanFireAfter = 2 * time.Minute
	auditRetention  = 90 * 24 * time.Hour
	maxCronCatchup  = 1000
	defaultPoll     = 5 * time.Minute
	maxPollBackoff  = time.Hour
)

// Config is the daemon-wide switch set, re-read on every tick (a human edit of
// config.yaml applies without a restart; an unreadable file is OFF).
type Config struct {
	Enabled bool
	Paused  bool
	Limits
}

// StartRequest is one task launch. Prompt carries the untrusted-signal
// envelope already; the Starter must not add authority: Mode and AutoAccept
// are the template's, the origin is OriginAutomation.
type StartRequest struct {
	Automation store.Automation
	Def        Def
	Title      string
	Prompt     string
	AuditID    int64
}

// StartResult identifies the task and run a firing started.
type StartResult struct {
	TaskID int64
	RunID  string
}

// RunStatus is the state of the root run an automation started.
type RunStatus struct {
	// State is running, waiting_permission, done, error, stopped or interrupted.
	State string
	// CostUSD is the tree's reported cost, nil when unknown.
	CostUSD *float64
	// FinalText is the run's final assistant message (for the memory block).
	FinalText string
}

// Running reports whether the run has not ended (a permission wait included).
func (s RunStatus) Running() bool { return s.State == "running" || s.State == "waiting_permission" }

// Starter creates the task and starts its run; the daemon adapter lives in
// cmd/smind and goes through the same workspace and runs path as the RPCs.
type Starter interface {
	Start(ctx context.Context, req StartRequest) (StartResult, error)
	Status(taskID int64) (RunStatus, error)
	Stop(taskID int64, reason string) error
}

// Sink receives the engine's events (ids only; no signal text).
type Sink interface {
	Fired(a store.Automation, row store.AutomationAudit)
	// Paused is called when the engine pauses an automation; taskID is the
	// task of the failed run that tripped it (0 when there was none).
	Paused(a store.Automation, reason string, taskID int64)
	Updated(a store.Automation)
}

// ErrDeferred is returned by a Source that must not call the forge now (budget
// or floor): the engine retries at Until without counting a failure.
type ErrDeferred struct{ Until time.Time }

func (e *ErrDeferred) Error() string { return "poll deferred until " + e.Until.Format(time.RFC3339) }

// PollResult is one poll's answer. Cursor is the newest signal id seen (the
// engine stores it after the audit rows are written); a first poll (empty
// cursor) must return the newest cursor and no signals.
type PollResult struct {
	Signals []Signal
	Cursor  string
}

// Source is a polled signal source (ADR-0044 section 1). cron is built in.
type Source interface {
	Kind() string
	Poll(ctx context.Context, a store.Automation, d Def, now time.Time) (PollResult, error)
	// Latest returns the newest matching signal, for a human's fire now.
	Latest(ctx context.Context, a store.Automation, d Def) (Signal, error)
}

// Deps wires an Engine.
type Deps struct {
	Store   *store.Store
	Starter Starter
	Memory  Memory
	Sources []Source
	Config  func() Config
	Sink    Sink
	Now     func() time.Time
}

// Engine evaluates signals against the rails. Tick and Fire are serialised.
type Engine struct {
	d         Deps
	mu        sync.Mutex
	lastPrune time.Time
}

// New builds an Engine; drive it with Tick (see Run).
func New(d Deps) *Engine {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Config == nil {
		d.Config = func() Config { return Config{} }
	}
	return &Engine{d: d}
}

// Run ticks every period until ctx is done.
func (e *Engine) Run(ctx context.Context, period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Tick(ctx)
		}
	}
}

func (e *Engine) source(kind string) Source {
	for _, s := range e.d.Sources {
		if s.Kind() == kind {
			return s
		}
	}
	return nil
}

func (e *Engine) def(a store.Automation) (Def, error) { return DefOf(a) }

// Tick runs one pass: reconcile finished runs, then due cron instants and due
// polls of every enabled automation. With automations.enabled false it does
// nothing at all (no polling, no firing).
func (e *Engine) Tick(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg := e.d.Config()
	if !cfg.Enabled {
		return
	}
	now := e.d.Now().UTC()
	auts, err := e.d.Store.ListAutomations()
	if err != nil {
		log.Printf("automation: list: %v", err)
		return
	}
	for _, a := range auts {
		if !a.Enabled {
			continue
		}
		def, err := e.def(a)
		if err != nil {
			log.Printf("automation %d: %v", a.ID, err)
			continue
		}
		e.reconcile(a, def, now)
		a, _ = e.d.Store.GetAutomation(a.ID)
		if a.SignalKind == KindCron {
			e.tickCron(ctx, cfg, a, def, now)
		} else {
			e.tickPoll(ctx, cfg, a, def, now)
		}
	}
	if now.Sub(e.lastPrune) > 24*time.Hour {
		e.lastPrune = now
		if _, err := e.d.Store.PruneAutomationAudit(now.Add(-auditRetention)); err != nil {
			log.Printf("automation: prune audit: %v", err)
		}
	}
}

func (e *Engine) tickCron(ctx context.Context, cfg Config, a store.Automation, d Def, now time.Time) {
	c, err := ParseCron(d.Signal.Cron)
	if err != nil {
		log.Printf("automation %d: %v", a.ID, err)
		return
	}
	if a.NextRunAt == nil { // just enabled: schedule, never fire the past
		if next, ok := c.Next(now); ok {
			_ = e.d.Store.SetAutomationNextRun(a.ID, &next)
		}
		return
	}
	var due []time.Time
	for t := *a.NextRunAt; !t.After(now) && len(due) < maxCronCatchup; {
		due = append(due, t)
		next, ok := c.Next(t)
		if !ok {
			break
		}
		t = next
	}
	if len(due) == 0 {
		return
	}
	last := due[len(due)-1]
	missed := len(due)
	if now.Sub(last) <= CatchupGrace {
		missed--
		late := now.Sub(last) > 2*time.Minute
		sig := Signal{
			Kind:        KindCron,
			Fingerprint: fmt.Sprintf("cron:%d:%s", a.ID, last.Format(time.RFC3339)),
			Payload:     map[string]any{"scheduledAt": last.Format(time.RFC3339), "catchup": late},
			Catchup:     late,
		}
		e.evaluate(ctx, cfg, a, d, sig, false, now)
	}
	if missed > 0 {
		e.audit(a, store.AutomationAudit{
			SignalKind: KindCron, Fingerprint: fmt.Sprintf("cron:%d:missed:%s", a.ID, due[0].Format(time.RFC3339)),
			Decision: store.AuditMissed, Reason: fmt.Sprintf("%d scheduled instant(s) passed while the daemon was not running", missed),
		}, now)
	}
	if next, ok := c.Next(now); ok {
		_ = e.d.Store.SetAutomationNextRun(a.ID, &next)
	}
}

func (e *Engine) tickPoll(ctx context.Context, cfg Config, a store.Automation, d Def, now time.Time) {
	src := e.source(a.SignalKind)
	if src == nil {
		return
	}
	if a.PollNextAllowedAt != nil && now.Before(*a.PollNextAllowedAt) {
		return
	}
	interval := defaultPoll
	if d.Rails.PollMinutes > 0 {
		interval = time.Duration(d.Rails.PollMinutes) * time.Minute
	}
	res, err := src.Poll(ctx, a, d, now)
	var def *ErrDeferred
	switch {
	case errors.As(err, &def):
		until := def.Until.UTC()
		_ = e.d.Store.SetAutomationPoll(a.ID, a.PollCursor, &until, a.PollFailures)
		return
	case err != nil:
		fails := a.PollFailures + 1
		back := interval << min(fails, 6)
		if back > maxPollBackoff {
			back = maxPollBackoff
		}
		until := now.Add(back)
		_ = e.d.Store.SetAutomationPoll(a.ID, a.PollCursor, &until, fails)
		log.Printf("automation %d: poll failed (%d in a row): %v", a.ID, fails, err)
		return
	}
	for _, sig := range res.Signals {
		e.evaluate(ctx, cfg, a, d, sig, false, now)
		a, _ = e.d.Store.GetAutomation(a.ID)
	}
	// The cursor advances only after every audit row is written.
	next := now.Add(interval)
	_ = e.d.Store.SetAutomationPoll(a.ID, res.Cursor, &next, 0)
}

// audit appends a non-fire row.
func (e *Engine) audit(a store.Automation, r store.AutomationAudit, now time.Time) store.AutomationAudit {
	r.AutomationID, r.CreatedAt = a.ID, now
	row, err := e.d.Store.InsertAutomationAudit(r)
	if err != nil {
		log.Printf("automation %d: audit: %v", a.ID, err)
	}
	return row
}

// evaluate applies the rails to one signal and writes exactly one audit row
// (ADR-0044 section 3): kill switch, paused, deduped, capped, fire.
func (e *Engine) evaluate(ctx context.Context, cfg Config, a store.Automation, d Def, sig Signal, manual bool, now time.Time) store.AutomationAudit {
	payload := mustJSON(sanitizePayload(sig.Payload))
	row := store.AutomationAudit{
		SignalKind: sig.Kind, Fingerprint: sig.Fingerprint, Manual: manual, Catchup: sig.Catchup, Payload: payload,
	}
	deny := func(decision, reason string) store.AutomationAudit {
		row.Decision, row.Reason = decision, reason
		return e.audit(a, row, now)
	}
	switch {
	case !cfg.Enabled:
		return deny(store.AuditKillSwitch, "automations.enabled is false")
	case cfg.Paused:
		return deny(store.AuditPaused, "automations.paused is true (global pause)")
	case a.Status == store.AutomationPaused:
		return deny(store.AuditPaused, a.PausedReason)
	}
	window := d.Rails.DedupeWindow()
	if window == 0 && sig.Kind == KindCron {
		window = cronDedupeFloor
	}
	if !manual && window > 0 {
		dup, err := e.d.Store.HasAutomationFireSince(a.ID, sig.Fingerprint, now.Add(-window))
		if err == nil && dup {
			return deny(store.AuditDeduped, "an identical signal already fired inside the dedupe window")
		}
	}
	open, err := e.d.Store.OpenAutomationFirings(a.ID)
	if err == nil && len(open) > 0 {
		return deny(store.AuditCapped, "running: a task this automation started has not finished")
	}
	n, err := e.d.Store.CountAutomationFiresSince(a.ID, now.Add(-time.Hour))
	if err == nil && n >= d.Rails.MaxRunsPerHour {
		return deny(store.AuditCapped, fmt.Sprintf("rate: %d runs in the last hour (maxRunsPerHour %d)", n, d.Rails.MaxRunsPerHour))
	}
	if a.BudgetBlocked {
		return deny(store.AuditCapped, fmt.Sprintf("budget: the last run reached its $%.2f budget; resume or edit the automation", d.Template.BudgetUSD))
	}
	if err := d.WithDefaults().Validate(cfg.Limits); err != nil {
		row.Decision, row.Reason = store.AuditFailed, "template no longer valid: "+err.Error()
		out := e.audit(a, row, now)
		e.recordFailure(a.ID, "the template is no longer valid: "+err.Error(), 0, now)
		return out
	}

	// Fire. The row is written BEFORE the task starts, so a crash between the
	// two leaves a visible fire row with no task (a failed run), never a silent
	// loss, and its fingerprint is already recorded.
	row.Decision = store.AuditFire
	row = e.audit(a, row, now)
	_ = e.d.Store.SetAutomationFired(a.ID, now)
	memory := ""
	if d.Rails.MemoryEnabled {
		if m, err := e.d.Memory.Read(a.ID, d.Rails.MemoryMaxBytes); err != nil {
			log.Printf("automation %d: memory: %v", a.ID, err)
		} else {
			memory = m
		}
	}
	p := BuildPrompt(d.Template, sig, now, memory)
	res, err := e.d.Starter.Start(ctx, StartRequest{Automation: a, Def: d, Title: p.Title, Prompt: p.Text, AuditID: row.ID})
	if err != nil {
		_ = e.d.Store.FinishAutomationAudit(row.ID, "start_failed", err.Error(), now)
		row.Outcome, row.Note = "start_failed", err.Error()
		e.recordFailure(a.ID, "could not start the task: "+err.Error(), res.TaskID, now)
		return row
	}
	_ = e.d.Store.SetAutomationAuditTask(row.ID, res.TaskID, res.RunID)
	row.TaskID, row.RunID = &res.TaskID, res.RunID
	if e.d.Sink != nil {
		e.d.Sink.Fired(a, row)
	}
	return row
}

// Fire is a human's fire now (ADR-0044 section 5.3): the same rails, except
// the dedupe window (the human is the signal). A workflow source uses its
// latest matching run.
func (e *Engine) Fire(ctx context.Context, id int64) (store.AutomationAudit, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	a, err := e.d.Store.GetAutomation(id)
	if err != nil {
		return store.AutomationAudit{}, err
	}
	d, err := e.def(a)
	if err != nil {
		return store.AutomationAudit{}, err
	}
	now := e.d.Now().UTC()
	e.reconcile(a, d, now) // a run that ended since the last tick must not read as "running"
	a, _ = e.d.Store.GetAutomation(id)
	sig := Signal{
		Kind:        a.SignalKind,
		Fingerprint: fmt.Sprintf("manual:%d:%d", a.ID, now.UnixNano()),
		Payload:     map[string]any{"scheduledAt": now.Format(time.RFC3339), "manual": true},
	}
	if a.SignalKind != KindCron {
		src := e.source(a.SignalKind)
		if src == nil {
			return store.AutomationAudit{}, fmt.Errorf("no source for %s", a.SignalKind)
		}
		latest, err := src.Latest(ctx, a, d)
		if err != nil {
			return store.AutomationAudit{}, fmt.Errorf("fire now: %w", err)
		}
		sig.Payload = latest.Payload
		sig.Fingerprint = fmt.Sprintf("manual:%d:%d:%v", a.ID, now.UnixNano(), latest.Payload["runId"])
	}
	return e.evaluate(ctx, e.d.Config(), a, d, sig, true, now), nil
}

// reconcile settles the fire rows whose task has ended: outcome, failure
// accounting, the memory block, the budget block, and the wall-clock stop.
func (e *Engine) reconcile(a store.Automation, d Def, now time.Time) {
	open, err := e.d.Store.OpenAutomationFirings(a.ID)
	if err != nil {
		return
	}
	for _, row := range open {
		if row.TaskID == nil {
			if now.Sub(row.CreatedAt) > orphanFireAfter {
				_ = e.d.Store.FinishAutomationAudit(row.ID, "start_failed", "no task was started (daemon stopped between the audit write and the launch)", now)
				e.recordFailure(a.ID, fmt.Sprintf("a firing never started a task (audit #%d)", row.ID), 0, now)
			}
			continue
		}
		st, err := e.d.Starter.Status(*row.TaskID)
		if err != nil {
			continue
		}
		if st.Running() {
			if limit := time.Duration(d.Template.MaxRunMinutes) * time.Minute; limit > 0 && now.Sub(row.CreatedAt) > limit {
				if err := e.d.Starter.Stop(*row.TaskID, "automation_timeout"); err != nil {
					log.Printf("automation %d: stop task %d: %v", a.ID, *row.TaskID, err)
					continue
				}
				_ = e.d.Store.FinishAutomationAudit(row.ID, "timeout", fmt.Sprintf("stopped after %d minutes (maxRunMinutes)", d.Template.MaxRunMinutes), now)
				e.recordFailure(a.ID, fmt.Sprintf("task #%d hit the %d-minute wall clock", *row.TaskID, d.Template.MaxRunMinutes), *row.TaskID, now)
			}
			continue
		}
		note := ""
		if st.State == "done" && d.Rails.MemoryEnabled {
			note = e.storeMemory(a.ID, d, st.FinalText)
		}
		if st.CostUSD != nil && *st.CostUSD >= d.Template.BudgetUSD {
			_ = e.d.Store.SetAutomationBudgetBlocked(a.ID, true)
			note = joinNote(note, fmt.Sprintf("reported cost $%.2f reached the $%.2f budget", *st.CostUSD, d.Template.BudgetUSD))
		}
		_ = e.d.Store.FinishAutomationAudit(row.ID, st.State, note, now)
		switch st.State {
		case "done":
			_ = e.d.Store.SetAutomationFailures(a.ID, 0)
		case "error", "interrupted":
			e.recordFailure(a.ID, fmt.Sprintf("task #%d ended `%s`", *row.TaskID, st.State), *row.TaskID, now)
		}
	}
	if fresh, err := e.d.Store.GetAutomation(a.ID); err == nil && e.d.Sink != nil {
		e.d.Sink.Updated(fresh)
	}
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func (e *Engine) storeMemory(id int64, d Def, final string) string {
	block, ok := ExtractBlock(final)
	if !ok {
		return ""
	}
	if err := e.d.Memory.Write(id, block, d.Rails.MemoryMaxBytes); err != nil {
		var rej *MemoryRejected
		if errors.As(err, &rej) {
			return "memory: rejected, " + rej.Reason
		}
		return "memory: not written: " + err.Error()
	}
	return "memory: updated"
}

// recordFailure bumps the consecutive-failure counter and pauses the
// automation, with a visible reason, at pauseAfterFailures.
func (e *Engine) recordFailure(id int64, detail string, taskID int64, now time.Time) {
	a, err := e.d.Store.GetAutomation(id)
	if err != nil {
		return
	}
	n := a.ConsecutiveFailures + 1
	_ = e.d.Store.SetAutomationFailures(id, n)
	d, err := e.def(a)
	if err != nil {
		return
	}
	limit := d.WithDefaults().Rails.PauseAfterFailures
	if n >= limit && a.Status != store.AutomationPaused {
		reason := fmt.Sprintf("paused after %d consecutive failed runs; last: %s", n, detail)
		_ = e.d.Store.PauseAutomation(id, reason, now)
		if e.d.Sink != nil {
			e.d.Sink.Paused(a, reason, taskID)
		}
	}
}
