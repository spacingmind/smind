package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
func (reg *Registry) StartWhenBusy(ctx context.Context, wm *workspace.Manager, runner *taskrunner.Runner, taskID, chatID int64, provider taskrunner.Provider, prompt string, perm taskrunner.PermissionSettings, thinkingLevel taskrunner.ThinkingLevel, whenBusy WhenBusy, source string, fromTaskID, fromChatID int64) (StartOutcome, error) {
	if !whenBusy.valid() {
		return StartOutcome{}, fmt.Errorf("runs: start: invalid whenBusy %q", whenBusy)
	}

	p, err := prepareStart(wm, runner, taskID, chatID, provider, prompt, perm, thinkingLevel)
	if err != nil {
		return StartOutcome{}, err
	}
	if source == store.ChatQueueSourceOrchestrator && taskrunner.ModeAutoApproves(p.catalog, p.perm) {
		return StartOutcome{}, fmt.Errorf("runs: start: an orchestrating agent may not pick an auto-approving permission configuration (ADR-0019); ask a human, or use a human-authored profile")
	}
	taskID, chatID, provider, prompt, perm, thinkingLevel = p.taskID, p.chatID, p.provider, p.prompt, p.perm, p.thinkingLevel

	runCtx, cancel := context.WithCancel(ctx)
	r := &run{
		taskID:             taskID,
		chatID:             chatID,
		provider:           provider,
		prompt:             prompt,
		runner:             runner,
		perm:               perm,
		thinkingLevel:      thinkingLevel,
		startedAt:          time.Now(),
		ctx:                runCtx,
		cancel:             cancel,
		closedCh:           make(chan struct{}),
		status:             StatusRunning,
		subscribers:        make(map[int]*subQueue),
		pendingPermissions: make(map[string]chan string),
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

	var busyRunID string
	func() {
		reg.mu.Lock()
		defer reg.mu.Unlock()
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
		reg.runs[id] = r
	}()

	if busyRunID == "" {
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
		Provider: string(provider), PermissionMode: perm.Mode, AutoAccept: perm.AutoAccept, ThinkingLevel: string(thinkingLevel),
	})
	if err != nil {
		return StartOutcome{}, fmt.Errorf("runs: start: encode run config: %w", err)
	}
	priority := 0
	if whenBusy == WhenBusyInterrupt {
		priority = 1
	}
	item, err := reg.st.EnqueueChatPrompt(chatID, prompt, string(cfg), source, priority)
	if err != nil {
		return StartOutcome{}, fmt.Errorf("runs: start: %w", err)
	}
	if source == store.ChatQueueSourceAgent && fromTaskID != 0 && fromChatID != 0 {
		if err := reg.st.SetChatQueueItemProvenance(item.ID, fromTaskID, fromChatID); err != nil {
			return StartOutcome{}, fmt.Errorf("runs: start: %w", err)
		}
	}
	reg.notifyQueueUpdated(chatID)

	if whenBusy == WhenBusyInterrupt {
		if err := reg.Stop(busyRunID); err != nil {
			return StartOutcome{}, fmt.Errorf("runs: start: stop running run: %w", err)
		}
	}

	// Cover the finish-between-check-and-enqueue race: if the chat has gone
	// idle since (e.g. its run finished while we were enquequeueing, before
	// our item existed for finish's own delivery pass), deliver now.
	// deliverOne is a no-op while the chat is busy.
	reg.deliverOne(chatID)

	return StartOutcome{Queued: true, QueueItemID: item.ID}, nil
}

// DeliverQueued delivers the next queued item of every chat that has one,
// once the start dependencies are wired (ADR-0021 §5 as amended: the
// queue keeps delivering after a restart). The interrupted run is not
// retried -- New has already reconciled it to a terminal state; only the
// queued prompts start.
func (reg *Registry) DeliverQueued() {
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
// moving on to the next item in the same pass. It never loops on one bad
// item: each pass consumes (delivers or cancels) at most one item per
// attempt, and a cancelled item is never returned by NextQueued again.
// A no-op whenever the chat has a running run or nothing queued.
func (reg *Registry) deliverOne(chatID int64) {
	wm, runner, ok := reg.starterDeps()
	if !ok {
		return
	}
	if _, busy := reg.runningRunForChat(chatID); busy {
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
		reg.cancelQueuedItem(item.ID, fmt.Sprintf("delivery failed: %v", err))
		reg.deliverOne(chatID)
		return
	}

	runID, err := reg.Start(context.Background(), wm, runner, chat.TaskID, chatID, taskrunner.Provider(cfg.Provider), deliverPrompt(item), taskrunner.PermissionSettings{Mode: cfg.PermissionMode, AutoAccept: cfg.AutoAccept}, taskrunner.ThinkingLevel(cfg.ThinkingLevel))
	if err != nil {
		var busyErr *ChatBusyError
		if errors.As(err, &busyErr) {
			// Someone else's run won the start race; their finish will
			// deliver this item. Leave it queued.
			return
		}
		reg.cancelQueuedItem(item.ID, fmt.Sprintf("delivery failed: %v", err))
		reg.deliverOne(chatID)
		return
	}
	if err := reg.st.MarkDelivered(item.ID, runID); err != nil {
		return
	}
	reg.notifyQueueUpdated(chatID)
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

// cancelQueuedItem marks a queued item cancelled with a reason and emits
// the queue snapshot event (ADR-0021 §8).
func (reg *Registry) cancelQueuedItem(id int64, reason string) {
	item, err := reg.st.GetChatQueueItem(id)
	if err != nil {
		return
	}
	if err := reg.st.MarkCancelled(id, reason); err != nil {
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
