package runs

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Parent notifier defaults (ADR-0041 B3).
const (
	defaultNotifyDebounce = 20 * time.Second
	defaultNotifyMaxHold  = 60 * time.Second
	// notifyStartupWindow bounds the startup scan to recent runs.
	notifyStartupWindow = 24 * time.Hour
)

// SetNotifyParent switches the parent notifier on or off (default on). Off
// still claims finished child runs so a later re-enable does not replay them.
func (reg *Registry) SetNotifyParent(on bool) {
	reg.notifyMu.Lock()
	reg.notifyParent = on
	reg.notifyMu.Unlock()
}

// SetNotifyDebounce sets how long a notification is held for coalescing. A
// value <= 0 delivers at once (no hold).
func (reg *Registry) SetNotifyDebounce(d time.Duration) {
	reg.notifyMu.Lock()
	reg.notifyDebounce = d
	reg.notifyMu.Unlock()
}

// evaluateChildNotifications checks taskID and each ancestor for a quiet
// subtree with unannounced child replies and queues one parent message for
// each. Never call it with reg.mu or any run's mu held.
func (reg *Registry) evaluateChildNotifications(taskID int64) {
	reg.evaluateChildNotificationsOpt(taskID, true)
}

func (reg *Registry) evaluateChildNotificationsOpt(taskID int64, deliver bool) {
	if reg.closing.Load() {
		return
	}
	reg.notifyMu.Lock()
	var chats []int64
	seen := map[int64]bool{}
	cur := taskID
	for i := 0; i < treeRootWalkHopLimit && cur != 0 && !seen[cur]; i++ {
		seen[cur] = true
		if chat, wake := reg.notifyTaskIfQuiet(cur); chat != 0 && wake {
			chats = append(chats, chat)
		}
		t, err := reg.st.GetTask(cur)
		if err != nil || t.ParentTaskID == nil {
			break
		}
		cur = *t.ParentTaskID
	}
	debounce := reg.notifyDebounce
	reg.notifyMu.Unlock()

	if len(chats) == 0 {
		return
	}
	reg.armQueueWake()
	if debounce <= 0 && deliver {
		// After notifyMu is released: deliverOne takes deliverMu, and a
		// cancel inside it re-enters the notifier.
		for _, c := range chats {
			reg.deliverOne(c)
		}
	}
}

// notifyTaskIfQuiet announces taskID's unnotified finished replies when its
// whole subtree is idle. It returns the chat it queued on (0 = none).
// Caller holds notifyMu.
func (reg *Registry) notifyTaskIfQuiet(taskID int64) (chatID int64, queued bool) {
	pending, err := reg.st.ListUnnotifiedRepliesForTask(taskID)
	if err != nil || len(pending) == 0 {
		return 0, false
	}
	busy, err := reg.st.SubtreeBusy(taskID)
	if err != nil || busy {
		return 0, false
	}
	ids := make([]string, len(pending))
	for i, r := range pending {
		ids[i] = r.ID
	}
	drop := func(why string) (int64, bool) {
		if _, err := reg.st.ClaimRunsParentNotified(ids, time.Now()); err != nil {
			log.Printf("runs: parent notify task %d: claim: %v", taskID, err)
		}
		if why != "" {
			log.Printf("runs: parent notify task %d: dropped (%s)", taskID, why)
		}
		return 0, false
	}

	subject, ok, err := reg.st.LatestFinishedRun(taskID)
	if err != nil {
		return 0, false
	}
	target := pending[len(pending)-1].ReplyChatID
	if !reg.notifyParent {
		return drop("")
	}
	if !ok || !notifyEligible(subject) {
		return drop("")
	}
	chat, err := reg.st.GetChat(target)
	if err != nil || chat.ArchivedAt != nil {
		return drop("parent chat missing or archived")
	}
	ptask, err := reg.st.GetTask(chat.TaskID)
	if err != nil || ptask.ArchivedAt != nil {
		return drop("parent task missing or archived")
	}
	prev, ok, err := reg.st.LatestRunForChat(target)
	if err != nil {
		return 0, false
	}
	if !ok {
		return drop("parent chat has no run to copy settings from")
	}
	cfgJSON, err := json.Marshal(chatQueueRunConfig{
		Provider: prev.Provider, PermissionMode: prev.PermissionMode, AutoAccept: prev.AutoAccept, Model: prev.Model,
		// The woken run keeps the seat: without the profile it would lose the
		// role instructions and the orchestration MCP entry (found by the first
		// in-smind dogfood run: a Lead woken by a child event could no longer call
		// task_archive).
		ProfileID: prev.ProfileID,
	})
	if err != nil {
		return 0, false
	}

	task, _ := reg.st.GetTask(taskID)
	block := BuildChildEventBlock(reg.childEventFacts(task, subject))
	_, _, err = reg.st.EnqueueChildEvent(store.ChildEventInput{
		ChatID: target, RunIDs: ids, Block: block, RunConfig: string(cfgJSON),
		Now: time.Now(), Debounce: reg.notifyDebounce, MaxHold: reg.notifyMaxHold,
	})
	switch {
	case errors.Is(err, store.ErrChildEventAlreadyNotified):
		return 0, false
	case errors.Is(err, store.ErrQueueFull):
		log.Printf("runs: parent notify task %d: chat %d queue full; will retry", taskID, target)
		return 0, false
	case err != nil:
		log.Printf("runs: parent notify task %d: %v", taskID, err)
		return 0, false
	}
	reg.notifyQueueUpdated(target)
	return target, true
}

// notifyEligible: a finished run worth telling the parent about.
func notifyEligible(r store.Run) bool {
	if r.Purpose != "" && r.Purpose != store.RunPurposeUser {
		return false
	}
	switch Status(r.Status) {
	case StatusDone, StatusError:
		return true
	case StatusStopped, StatusInterrupted:
		return r.StopReason == StopReasonDaemonRestart
	}
	return false
}

func (reg *Registry) childEventFacts(task store.Task, run store.Run) ChildEventFacts {
	f := ChildEventFacts{
		TaskID: task.ID, Title: task.Title, RunID: run.ID, Status: run.Status, StopReason: run.StopReason,
		Disposition: DispositionOf(DispositionFacts{
			Status: Status(run.Status), StopReason: run.StopReason, FailureKind: run.FailureKind, FailureResetAt: run.FailureResetAt,
			HasSession: run.Status == string(StatusError) && resumableKind(run.FailureKind) && chatHasSession(reg.st, run.ChatID), Now: time.Now(),
		}),
		FailureKind: run.FailureKind, FailureWindow: run.FailureWindow, FailureResetAt: run.FailureResetAt, FailureResetInferred: run.FailureResetInferred,
	}
	if a := failureActionOf(run.FailureKind, run.Provider, run.Model, run.FailureWindow, run.FailureResetAt, nil, 0); a != nil {
		f.FailureNext = a.Label
	}
	if run.BaseSHA != nil {
		f.BaseSHA = *run.BaseSHA
	}
	if run.HeadSHA != nil {
		f.HeadSHA = *run.HeadSHA
	}
	if u, err := reg.st.GetRunUsage(run.ID); err == nil {
		f.CostUSD = u.CostUSD
	}
	if hist, _, err := reg.History(run.ID); err == nil {
		f.LastText = trailingAgentText(hist)
	}
	return f
}

// trailingAgentText is the last contiguous assistant text of a run: the
// text chunks after the final content event, ignoring the turn's trailing
// bookkeeping events (done, usage, notes, rate-limit and mcp snapshots).
func trailingAgentText(history []Event) string {
	end := len(history)
	for end > 0 && isBookkeepingEvent(history[end-1].Type) {
		end--
	}
	history = history[:end]
	i := len(history)
	for i > 0 && history[i-1].Type == taskrunner.EventTypeText {
		i--
	}
	var b strings.Builder
	for _, e := range history[i:] {
		b.WriteString(e.Text)
	}
	return b.String()
}

// NotifyPendingChildren is the startup scan: it announces recent finished
// child runs (including ones the restart interrupted) that were never
// announced. Safe to repeat: claims make it idempotent.
func (reg *Registry) NotifyPendingChildren() {
	if reg.closing.Load() {
		return
	}
	ids, err := reg.st.TasksWithUnnotifiedReplies(time.Now().Add(-notifyStartupWindow))
	if err != nil {
		return
	}
	for _, id := range ids {
		reg.evaluateChildNotifications(id)
	}
}

func isBookkeepingEvent(t taskrunner.EventType) bool {
	switch t {
	case taskrunner.EventTypeDone, taskrunner.EventTypeUsage, taskrunner.EventTypeSessionNote,
		taskrunner.EventTypePermissionModeChanged, taskrunner.EventTypeMcpServers, taskrunner.EventTypeRateLimit:
		return true
	}
	return false
}
