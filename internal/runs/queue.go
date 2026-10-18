package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// WhenBusy is ADR-0021 §2's delivery mode for a prompt sent to a chat that
// already has a running run: reject keeps today's behavior (the default,
// so old clients are unchanged), queue appends FIFO, interrupt appends
// with priority 1 and stops the running run.
type WhenBusy string

const (
	WhenBusyReject    WhenBusy = "reject"
	WhenBusyQueue     WhenBusy = "queue"
	WhenBusyInterrupt WhenBusy = "interrupt"
)

// chatQueueDeliveryCap bounds one deliverOne pass's item consumption, so
// a pathological queue can't keep the delivery lock held indefinitely.
const chatQueueDeliveryCap = 20

func (m WhenBusy) valid() bool {
	return m == WhenBusyReject || m == WhenBusyQueue || m == WhenBusyInterrupt
}

// StartOutcome is what StartWhenBusy returns: either a started run's ID
// (Queued false) or a queued item's ID (Queued true). The queued shape is
// only reachable by explicitly passing whenBusy, so pre-ADR-0021 callers
// never see it.
type StartOutcome struct {
	RunID       string
	Queued      bool
	QueueItemID int64
}

// chatQueueRunConfig is the JSON stored in chat_queue.run_config: the
// exact start parameters the item will be delivered with.
type chatQueueRunConfig struct {
	Provider       string `json:"provider"`
	PermissionMode string `json:"permissionMode"`
	AutoAccept     bool   `json:"autoAccept"`
	ThinkingLevel  string `json:"thinkingLevel"`
	Model          string `json:"model,omitempty"`
}

// SetStarter wires the dependencies delivery needs to start runs itself
// (the same wm/runner Start's callers pass in), in the same spirit as
// SetNotifier: runs.New runs before they exist, so delivery of items that
// survived a restart (ADR-0021 §5 as amended) waits for this. Wired in
// wsapi.New, where all three are constructed together.
func (reg *Registry) SetStarter(wm *workspace.Manager, runner *taskrunner.Runner) {
	reg.mu.Lock()
	reg.starterWM = wm
	reg.starterRunner = runner
	reg.mu.Unlock()
}

func (reg *Registry) starterDeps() (*workspace.Manager, *taskrunner.Runner, bool) {
	reg.mu.Lock()
	wm, runner := reg.starterWM, reg.starterRunner
	reg.mu.Unlock()
	return wm, runner, wm != nil && runner != nil
}

// StartWhenBusy is Start plus ADR-0021 §2's whenBusy handling. An idle
// chat starts immediately under every mode, exactly like Start. A busy
// chat rejects (WhenBusyReject, the pre-queue error verbatim), or
// validates the run config exactly as a direct start would, enqueues, and
// returns the item's id. An interrupt item is enqueued with priority 1
// and stops the running run; its delivery then happens at that run's
// finish, on the chat's resumed session (O1).
//
// source is the provenance ADR-0021 §4 records on the item ('human',
// 'orchestrator', 'agent'); an agent source with fromTaskID/fromChatID
// gets the [message from task #T, chat #C] delivery header. An
// orchestrator source may not enqueue an auto-approving permission
// configuration (ADR-0019 decision 6, at enqueue time per ADR-0021 §6).
func (reg *Registry) StartWhenBusy(ctx context.Context, wm *workspace.Manager, runner *taskrunner.Runner, taskID, chatID int64, provider taskrunner.Provider, prompt string, perm taskrunner.PermissionSettings, thinkingLevel taskrunner.ThinkingLevel, whenBusy WhenBusy, source string, fromTaskID, fromChatID int64, opts ...StartOption) (StartOutcome, error) {
	model := startModel(opts)
	if resolveStartOptions(opts).isolation != nil {
		return StartOutcome{}, errors.New("runs: start: WithIsolation is only supported by Start, not StartWhenBusy")
	}
	if whenBusy == "" {
		whenBusy = WhenBusyReject // the wire default: old clients unchanged
	}
	if !whenBusy.valid() {
		return StartOutcome{}, fmt.Errorf("runs: start: invalid whenBusy %q", whenBusy)
	}

	p, err := prepareStart(wm, runner, taskID, chatID, provider, prompt, perm, thinkingLevel, model)
	if err != nil {
		return StartOutcome{}, err
	}
	// ADR-0019 decision 6 at enqueue time (ADR-0021 §6): every non-human
	// caller (orchestrator or agent) is barred from auto-approving
	// permission configurations; only a human-authored profile may carry
	// one, and a human source here is the interactive wire caller.
	if source != store.ChatQueueSourceHuman && taskrunner.ModeAutoApproves(p.catalog, p.perm) {
		return StartOutcome{}, fmt.Errorf("runs: start: a non-human caller may not pick an auto-approving permission configuration (ADR-0019); ask a human, or use a human-authored profile: pass profileId of a human-authored profile with the mode you need (see profile_list), or choose a mode that is not marked autoApproves in provider_list")
	}
	taskID, chatID, provider, prompt, perm, thinkingLevel = p.taskID, p.chatID, p.provider, p.prompt, p.perm, p.thinkingLevel

	// An agent-sourced prompt carries its provenance header on an
	// immediate start too (ADR-0021 §4) -- the queued path stamps it at
	// delivery via deliverPrompt instead, so it is only prepended here
	// for the run we start ourselves.
	// startPrompt is what an immediate start sends; the stored queue item
	// keeps the bare prompt so deliverPrompt doesn't stamp the header twice.
	startPrompt := prompt
	if source == store.ChatQueueSourceAgent && fromTaskID != 0 && fromChatID != 0 {
		startPrompt = fmt.Sprintf("[message from task #%d, chat #%d]\n%s", fromTaskID, fromChatID, prompt)
	}

	// Resolved before reg.mu is taken, same as Start (store reads never
	// happen under the registry lock; parent links are immutable while a
	// run exists).
	treeRoot, err := reg.treeRootOf(taskID)
	if err != nil {
		return StartOutcome{}, err
	}

	// O1 provenance: same worktree resolution Start does, for the
	// immediate-start run this path builds itself (the queued path's
	// delivery goes through Start, which sets its own).
	worktree := ""
	if t, terr := wm.GetTask(taskID); terr == nil && t.WorktreePath != nil {
		worktree = *t.WorktreePath
	}

	runCtx, cancel := context.WithCancel(ctx)
	r := &run{
		taskID:             taskID,
		chatID:             chatID,
		provider:           provider,
		prompt:             startPrompt,
		runner:             runner,
		perm:               perm,
		thinkingLevel:      thinkingLevel,
		model:              model,
		purpose:            startPurpose(opts),
		origin:             source,
		interrupt:          resolveStartOptions(opts).interrupt,
		worktree:           worktree,
		startedAt:          time.Now(),
		ctx:                runCtx,
		cancel:             cancel,
		closedCh:           make(chan struct{}),
		status:             StatusRunning,
		subscribers:        make(map[int]*subQueue),
		pendingPermissions: make(map[string]chan string),
		treeRoot:           treeRoot,
	}

	// The busy/idle decision and (when idle) this run's registration share
	// one reg.mu critical section, so a concurrent finish can't slip a
	// terminal transition in between the check and the registration and
	// strand the prompt. (A busy chat's enqueue happens after the lock is
	// released; the post-enqueue deliverOne below covers the symmetric
	// race, where the run finished between the check and the enqueue.
	// registerAndDrive performs the registration itself.)
	id, err := newRunID()
	if err != nil {
		cancel()
		return StartOutcome{}, fmt.Errorf("runs: start: %w", err)
	}
	r.id = id

	// ADR-0024 §2: a task whose worktree setup script is still running is
	// busy -- nothing may start in a half-installed worktree. reject errors
	// naming the setup; queue and interrupt (there is no run to stop)
	// enqueue, and the setup-finish hook delivers.
	setupBusy := false
	if t, terr := wm.GetTask(taskID); terr == nil && t.SetupStatus == workspace.SetupRunning {
		setupBusy = true
		if whenBusy == WhenBusyReject {
			cancel()
			return StartOutcome{}, &SetupBusyError{TaskID: taskID}
		}
	}

	if reg.testAfterSetupCheck != nil {
		reg.testAfterSetupCheck()
	}

	var busyRunID string
	var archiving bool
	var treeFull *TreeBusyError
	func() {
		if setupBusy {
			return
		}
		reg.mu.Lock()
		defer reg.mu.Unlock()
		if reg.taskArchivingLocked(taskID) {
			archiving = true
			return
		}
		for _, existing := range reg.runs {
			if existing.chatID != chatID {
				continue
			}
			existing.mu.Lock()
			running := existing.status == StatusRunning
			existing.mu.Unlock()
			if running {
				busyRunID = existing.id
				return
			}
		}
		// Idle chat but the tree is full: every mode refuses (an enqueue
		// is not a concurrent run, but delivering it immediately would be;
		// a busy chat's enqueue below is unaffected because we never reach
		// here with busyRunID set).
		if running := reg.runningInTreeLocked(treeRoot); running >= reg.maxConcurrentRunsPerTreeLocked() {
			treeFull = &TreeBusyError{RootTaskID: treeRoot, Limit: reg.maxConcurrentRunsPerTreeLocked(), Running: running}
			return
		}
		reg.runs[id] = r
	}()
	if archiving {
		cancel()
		return StartOutcome{}, fmt.Errorf("runs: start: %w", ErrTaskArchiving)
	}
	if treeFull != nil {
		cancel()
		return StartOutcome{}, treeFull
	}

	if busyRunID == "" && !setupBusy {
		// Idle: every mode starts immediately, exactly like Start's tail.
		if err := reg.registerAndDrive(r, runCtx, runner); err != nil {
			cancel()
			return StartOutcome{}, err
		}
		return StartOutcome{RunID: id}, nil
	}

	// Busy.
	cancel() // the speculative run struct is unused on this path.

	if whenBusy == WhenBusyReject {
		return StartOutcome{}, &ChatBusyError{ChatID: chatID, RunID: busyRunID}
	}

	cfg, err := json.Marshal(chatQueueRunConfig{
		Provider: string(provider), PermissionMode: perm.Mode, AutoAccept: perm.AutoAccept, ThinkingLevel: string(thinkingLevel), Model: model,
	})
	if err != nil {
		return StartOutcome{}, fmt.Errorf("runs: start: encode run config: %w", err)
	}
	priority := 0
	if whenBusy == WhenBusyInterrupt {
		priority = 1
	}
	item, err := reg.st.EnqueueChatPrompt(chatID, prompt, string(cfg), source, priority, fromTaskID, fromChatID)
	if err != nil {
		return StartOutcome{}, fmt.Errorf("runs: start: %w", err)
	}
	reg.notifyQueueUpdated(chatID)

	if whenBusy == WhenBusyInterrupt && busyRunID != "" {
		if err := reg.Stop(busyRunID); err != nil {
			return StartOutcome{}, fmt.Errorf("runs: start: stop running run: %w", err)
		}
	}

	// Cover the finish-between-check-and-enqueue race: if the chat has gone
	// idle since (e.g. its run finished while we were enquequeueing, before
	// our item existed for finish's own delivery pass), deliver now.
	// deliverOne is a no-op while the chat is busy. This also covers setup
	// finishing between the setup-status read above and the enqueue (its
	// finish hook ran over an empty queue): deliverOne re-reads setup status
	// from the store. Pinned by TestSetup_QueuedPromptNotStranded...
	reg.deliverOne(chatID)

	return StartOutcome{Queued: true, QueueItemID: item.ID}, nil
}

// DeliverQueued delivers the next queued item of every chat that has one,
// once the start dependencies are wired (ADR-0021 §5 as amended: the
// queue keeps delivering after a restart). The interrupted run is not
// retried -- New has already reconciled it to a terminal state; only the
// queued prompts start.
func (reg *Registry) DeliverQueued() {
	if reg.closing.Load() {
		return
	}
	chats, err := reg.st.ChatsWithQueued()
	if err != nil {
		return
	}
	for _, chatID := range chats {
		reg.deliverOne(chatID)
	}
}

// deliverOne delivers chatID's oldest queued item through the same code
// path as a direct Start (Registry.Start), marking it delivered with the
// new run id -- or cancelled with the reason when it can no longer start,
// moving on to the next item in the same pass.
//
// The whole decision sequence (busy check, NextQueued, Start, MarkDelivered)
// runs under reg.deliverMu, so two concurrent callers -- finish's delivery
// and the enqueue path's race-coverage call -- can never both pull the
// same item and start it twice. Start returns as soon as the run is
// registered (drive is a background goroutine), so holding the lock is
// cheap. reg.mu is never held while taking deliverMu (ordering:
// deliverMu -> reg.mu only).
//
// A no-op whenever the starter isn't wired, the chat has a running run, or
// nothing is queued. The per-pass loop consumes (delivers or cancels) at
// most one item per attempt and never retries the same item (a cancelled
// item is never returned by NextQueued again), capped at chatQueueDeliveryCap
// items so a pathological queue can't keep one lock holder spinning.
func (reg *Registry) deliverOne(chatID int64) {
	if reg.closing.Load() {
		return
	}
	wm, runner, ok := reg.starterDeps()
	if !ok {
		return
	}

	reg.deliverMu.Lock()
	defer reg.deliverMu.Unlock()

	for i := 0; i < chatQueueDeliveryCap; i++ {
		if _, busy := reg.runningRunForChat(chatID); busy {
			return
		}
		if setupRunningForChat(wm, chatID) {
			// Setup finish (ok or failed) delivers via DeliverQueued.
			return
		}
		item, ok, err := reg.st.NextQueued(chatID)
		if err != nil || !ok {
			return
		}

		reg.mu.Lock()
		reg.deliveryAttempts[item.ID]++
		reg.mu.Unlock()

		var cfg chatQueueRunConfig
		if err := json.Unmarshal([]byte(item.RunConfig), &cfg); err != nil {
			cfg = chatQueueRunConfig{}
		}
		chat, err := wm.GetChat(chatID)
		if err != nil {
			reg.cancelQueuedItem(item.ID, store.ChatQueueCancelAuto, fmt.Sprintf("delivery failed: %v", err))
			continue
		}

		startOpts := []StartOption{WithModel(cfg.Model), WithOrigin(item.Source)}
		if item.Priority == 1 {
			startOpts = append(startOpts, WithInterrupt())
		}
		runID, err := reg.Start(context.Background(), wm, runner, chat.TaskID, chatID, taskrunner.Provider(cfg.Provider), deliverPrompt(item), taskrunner.PermissionSettings{Mode: cfg.PermissionMode, AutoAccept: cfg.AutoAccept}, taskrunner.ThinkingLevel(cfg.ThinkingLevel), startOpts...)
		if err != nil {
			var busyErr *ChatBusyError
			var setupErr *SetupBusyError
			var treeErr *TreeBusyError
			if errors.As(err, &setupErr) {
				// Setup was re-run since the check above; its finish delivers.
				return
			}
			if errors.As(err, &busyErr) {
				// Someone else's run won the start race; their finish will
				// deliver this item. Leave it queued.
				return
			}
			if errors.As(err, &treeErr) {
				// The task tree is at its concurrency limit; the slot frees
				// when ANY run in the tree ends, which finish covers via
				// its DeliverQueued retry. Leave it queued.
				return
			}
			if errors.Is(err, ErrTaskArchiving) {
				// The task entered archive between this item being queued
				// and now; leave it queued -- if the archive completes the
				// task is gone (redelivery will then fail loudly), and if
				// the archive aborts the next finish/enqueue delivers it.
				return
			}
			reg.cancelQueuedItem(item.ID, store.ChatQueueCancelAuto, fmt.Sprintf("delivery failed: %v", err))
			continue
		}
		if err := reg.st.MarkDelivered(item.ID, runID); err != nil {
			log.Printf("runs: mark queue item %d delivered (run %s): %v", item.ID, runID, err)
			return
		}
		reg.notifyQueueUpdated(chatID)
		return
	}
	log.Printf("runs: chat %d queue delivery pass hit the %d-item cap; will resume at the next finish", chatID, chatQueueDeliveryCap)
}

// SetupBusyError is the whenBusy=reject rejection while a task's worktree
// setup script is still running (ADR-0024 §2).
type SetupBusyError struct{ TaskID int64 }

func (e *SetupBusyError) Error() string {
	return fmt.Sprintf("runs: start: task %d's worktree setup script is still running; wait for it to finish or pass whenBusy=queue", e.TaskID)
}

// setupRunningForChat reports whether chatID's task has a setup script
// still running.
func setupRunningForChat(wm *workspace.Manager, chatID int64) bool {
	chat, err := wm.GetChat(chatID)
	if err != nil {
		return false
	}
	t, err := wm.GetTask(chat.TaskID)
	return err == nil && t.SetupStatus == workspace.SetupRunning
}

// deliverPrompt applies ADR-0021 §4's provenance header: an agent-source
// item is delivered with a one-line [message from task #T, chat #C]
// header before the prompt; human and orchestrator items are verbatim.
func deliverPrompt(item store.ChatQueueItem) string {
	if item.Source != store.ChatQueueSourceAgent || item.FromTaskID == nil || item.FromChatID == nil {
		return item.Prompt
	}
	return fmt.Sprintf("[message from task #%d, chat #%d]\n%s", *item.FromTaskID, *item.FromChatID, item.Prompt)
}

// cancelQueuedItem marks a queued item cancelled with a kind
// (store.ChatQueueCancelUser/ChatQueueCancelAuto) and a display reason,
// and emits the queue snapshot event (ADR-0021 §8).
func (reg *Registry) cancelQueuedItem(id int64, kind, reason string) {
	item, err := reg.st.GetChatQueueItem(id)
	if err != nil {
		return
	}
	if err := reg.st.MarkCancelled(id, kind, reason); err != nil {
		return
	}
	reg.notifyQueueUpdated(item.ChatID)
}

// notifyQueueUpdated fires the notifier (if any) with the chat's full
// queue snapshot -- emitted on enqueue, deliver, cancel, and auto-cancel
// (ADR-0021 §8, ADR-0009 snapshot shape).
func (reg *Registry) notifyQueueUpdated(chatID int64) {
	n := reg.getNotifier()
	if n == nil {
		return
	}
	items, err := reg.st.ListChatQueue(chatID)
	if err != nil {
		return
	}
	n.NotifyChatQueueUpdated(chatID, items)
}

// ListChatQueue returns chatID's queue items (all statuses, oldest first)
// for the chat.queueList RPC (ADR-0021 §8).
func (reg *Registry) ListChatQueue(chatID int64) ([]store.ChatQueueItem, error) {
	return reg.st.ListChatQueue(chatID)
}

// CancelQueuedItem cancels a still-queued item by id, for the
// chat.queueCancel RPC (ADR-0021 §8). Anything not queued (already
// delivered, already cancelled, unknown) is an error, never a silent
// no-op.
func (reg *Registry) CancelQueuedItem(id int64) error {
	item, err := reg.st.GetChatQueueItem(id)
	if err != nil {
		return err
	}
	if item.Status != store.ChatQueueStatusQueued {
		return fmt.Errorf("queue item %d is %s, not queued", id, item.Status)
	}
	reg.cancelQueuedItem(id, store.ChatQueueCancelUser, "cancelled by user")
	return nil
}

// StartOption is an optional per-run start parameter of Start and
// StartWhenBusy, so adding one never changes the positional signature.
type StartOption func(*startOptions)

type startOptions struct {
	model     string
	purpose   string
	origin    string
	interrupt bool
	isolation *taskrunner.IsolationOptions
}

// WithOrigin records who authored the run's prompt (store.ChatQueueSource*:
// human, orchestrator, agent) on the run, for ADR-0026's window builder.
// Omitted = ” (unknown). StartWhenBusy stamps its own source argument on an
// immediate start, so callers only need this on a direct Start.
func WithOrigin(source string) StartOption {
	return func(o *startOptions) { o.origin = source }
}

// WithInterrupt marks the run's prompt as a mid-run interrupt (a delivered
// priority-1 queue item).
func WithInterrupt() StartOption {
	return func(o *startOptions) { o.interrupt = true }
}

// WithIsolation runs the turn through Runner.RunPromptIsolated (no tools, no
// MCP, no ambient context; ADR-0026) instead of RunPrompt. Only a system run
// may be isolated -- it requires WithPurpose -- and only on claude-native
// (D13); Start refuses anything else before registering or persisting a
// run. The run's permission requests stay the system auto-deny. Start only:
// StartWhenBusy rejects it, because a queued item cannot carry it.
func WithIsolation(iso taskrunner.IsolationOptions) StartOption {
	return func(o *startOptions) { o.isolation = &iso }
}

// validateIsolation is Start's pre-registration check of WithIsolation.
func validateIsolation(provider taskrunner.Provider, opts []StartOption) error {
	o := resolveStartOptions(opts)
	if o.isolation == nil {
		return nil
	}
	if startPurpose(opts) == "" {
		return errors.New("runs: start: WithIsolation requires a system purpose (WithPurpose)")
	}
	if provider != taskrunner.ProviderClaudeNative {
		return fmt.Errorf("runs: start: %w (got %q)", taskrunner.ErrIsolationUnsupported, provider)
	}
	return nil
}

func resolveStartOptions(opts []StartOption) startOptions {
	var o startOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithPurpose marks the run as a system run (ADR-0026: "reflect",
// "curate"). System runs are hidden from List/ListFiltered unless asked
// for, auto-deny every permission request, and never publish permission
// notifications. "" is an ordinary user run.
func WithPurpose(purpose string) StartOption {
	return func(o *startOptions) { o.purpose = purpose }
}

func startPurpose(opts []StartOption) string {
	var o startOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.purpose == store.RunPurposeUser {
		return ""
	}
	return o.purpose
}

// WithModel names the model the run is asked to use (ADR-0025). "" is the
// provider's default. It is validated like every other start parameter
// (syntax always, membership once the provider's models are discovered),
// persisted on the run row, and kept in a queued item's run config.
func WithModel(model string) StartOption {
	return func(o *startOptions) { o.model = model }
}

func startModel(opts []StartOption) string {
	var o startOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o.model
}
