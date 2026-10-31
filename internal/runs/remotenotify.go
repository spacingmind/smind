package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// RemoteChildKind is what a fleet watch has to say about its remote task
// (ADR-0048): a finished run, a status that could not be observed, or a
// host that can no longer be reached.
type RemoteChildKind string

const (
	RemoteFinished        RemoteChildKind = "finished"
	RemoteStatusUnknown   RemoteChildKind = "status_unknown"
	RemoteHostUnavailable RemoteChildKind = "host_unavailable"

	// remoteTokenMax caps one sanitized remote status-line token.
	remoteTokenMax = 48
)

// RemoteChildFacts is everything BuildRemoteChildEventBlock renders about
// one remote child task. Pure data: no registry access. CostUSD is nil
// when the remote did not report a cost.
type RemoteChildFacts struct {
	Kind                                    RemoteChildKind
	Host, RemoteTaskID, Title, RunID, State string
	StopReason, FailureKind                 string
	CostUSD                                 *float64
	LastText                                string
	Failures                                int
}

// BuildRemoteChildEventBlock renders the status line plus the untrusted
// <child-response> element for one remote child task. The unknown-state
// kinds carry no response text: the parent must go look, not act on a guess.
func BuildRemoteChildEventBlock(f RemoteChildFacts) string {
	global := f.Host + ":" + f.RemoteTaskID
	title := childEventTitle(f.Title)
	switch f.Kind {
	case RemoteStatusUnknown:
		return fmt.Sprintf("- remote task %s \"%s\": status unknown after %d failed observations; "+
			"it may still be running or may already have finished. "+
			"Inspect it (task_status with host) before re-delegating; do not treat it as finished.", global, title, f.Failures)
	case RemoteHostUnavailable:
		return fmt.Sprintf("- remote task %s \"%s\": host unavailable (it was removed or can no longer be reached); "+
			"the task's state is unknown. Do not treat it as finished.", global, title)
	}

	// State, StopReason, FailureKind and RunID are remote-controlled and
	// sit in the status line, outside the untrusted element: reduce them
	// to opaque tokens so a hostile host cannot inject prompt text.
	parts := []string{remoteToken(f.State)}
	if f.StopReason != "" {
		parts = append(parts, remoteToken(f.StopReason))
	}
	if f.FailureKind != "" {
		parts = append(parts, remoteToken(f.FailureKind))
	}
	if f.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("cost $%.2f", *f.CostUSD))
	} else {
		parts = append(parts, "cost not reported")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- remote task %s \"%s\" run %s: %s", global, title, remoteToken(f.RunID), strings.Join(parts, " · "))
	if text := strings.TrimSpace(f.LastText); text != "" {
		fmt.Fprintf(&b, "\n<child-response host=\"%s\" task=\"%s\" untrusted=\"true\">%s</child-response>",
			f.Host, global, escapeChildText(text))
	}
	return b.String()
}

// remoteToken reduces a remote-supplied status-line token to [A-Za-z0-9_.-],
// cut at 48 runes, so it cannot carry newlines, markup or quoting into the
// parent's prompt; a token emptied by the filter reads "unknown".
func remoteToken(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == remoteTokenMax {
			break
		}
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
			n++
		}
	}
	if n == 0 {
		return "unknown"
	}
	return b.String()
}

// NotifyRemoteChild delivers a fleet watch's remote-child facts to the
// local parent chat, exactly once: the watch row going open -> closed is
// the claim gate (there is no local run row to claim). It returns nil when
// the watch was closed without a notification (already notified, parent
// gone, notify off, registry closing) and the error for the caller to
// retry -- a queue-full or store failure leaves the watch open. Never call
// it with reg.mu or any run's mu held.
func (reg *Registry) NotifyRemoteChild(w store.FleetWatch, f RemoteChildFacts) error {
	if reg.closing.Load() {
		// The watch stays open: the startup scan re-delivers it later.
		return nil
	}
	closeGone := func() {
		if _, err := reg.st.CloseFleetWatchIfOpen(w.ID, "parent_gone"); err != nil {
			log.Printf("runs: remote notify watch %d: close: %v", w.ID, err)
		}
	}
	chat, err := reg.st.GetChat(w.ChatID)
	if err != nil || chat.ArchivedAt != nil {
		closeGone()
		log.Printf("runs: remote notify watch %d: parent chat missing or archived", w.ID)
		return nil
	}
	ptask, err := reg.st.GetTask(chat.TaskID)
	if err != nil || ptask.ArchivedAt != nil {
		closeGone()
		log.Printf("runs: remote notify watch %d: parent task missing or archived", w.ID)
		return nil
	}

	reg.notifyMu.Lock()
	debounce, maxHold := reg.notifyDebounce, reg.notifyMaxHold
	if !reg.notifyParent {
		reg.notifyMu.Unlock()
		if _, err := reg.st.CloseFleetWatchIfOpen(w.ID, "notify_off"); err != nil {
			log.Printf("runs: remote notify watch %d: close: %v", w.ID, err)
		}
		return nil
	}
	prev, ok, err := reg.st.LatestRunForChat(w.ChatID)
	if err != nil {
		reg.notifyMu.Unlock()
		return err
	}
	if !ok {
		reg.notifyMu.Unlock()
		if _, err := reg.st.CloseFleetWatchIfOpen(w.ID, "no_parent_run"); err != nil {
			log.Printf("runs: remote notify watch %d: close: %v", w.ID, err)
		}
		return nil
	}
	cfgJSON, err := json.Marshal(chatQueueRunConfig{
		Provider: prev.Provider, PermissionMode: prev.PermissionMode, AutoAccept: prev.AutoAccept, Model: prev.Model,
		ProfileID: prev.ProfileID,
	})
	if err != nil {
		reg.notifyMu.Unlock()
		return err
	}
	// Held across the enqueue like the local B3 path: the watch close and
	// the queue append must land without a concurrent notification interleaving.
	_, err = reg.st.EnqueueChildEventClosingWatch(store.ChildEventInput{
		ChatID: w.ChatID, Block: BuildRemoteChildEventBlock(f), RunConfig: string(cfgJSON),
		Now: time.Now(), Debounce: debounce, MaxHold: maxHold,
	}, w.ID, string(f.Kind))
	reg.notifyMu.Unlock()
	switch {
	case errors.Is(err, store.ErrChildEventAlreadyNotified):
		return nil
	case err != nil:
		return err
	}
	reg.notifyQueueUpdated(w.ChatID)
	reg.armQueueWake()
	if debounce <= 0 {
		// After notifyMu is released: deliverOne takes deliverMu, and a
		// cancel inside it re-enters the notifier.
		reg.deliverOne(w.ChatID)
	}
	return nil
}
