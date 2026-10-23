package runs

import (
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// failoverRecorder is a Notifier that records only NotifyRunFailover.
type failoverRecorder struct {
	mu   sync.Mutex
	seen []failoverCall
}

type failoverCall struct {
	from, to         string
	taskID, toChatID int64
}

func (n *failoverRecorder) NotifyRunStatus(RunStatus) {}
func (n *failoverRecorder) NotifyPermissionPending(string, int64, int64, string, string, []taskrunner.PermissionOption) {
}
func (n *failoverRecorder) NotifyPermissionResolved(string, int64, int64, string, string, string) {}
func (n *failoverRecorder) NotifyChatQueueUpdated(int64, []store.ChatQueueItem)                   {}
func (n *failoverRecorder) NotifyRunFailover(from, to string, taskID, toChatID int64) {
	n.mu.Lock()
	n.seen = append(n.seen, failoverCall{from, to, taskID, toChatID})
	n.mu.Unlock()
}
func (n *failoverRecorder) calls() []failoverCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]failoverCall(nil), n.seen...)
}

func TestRunFailoverEventShape(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	rec := &failoverRecorder{}
	e.reg.SetNotifier(rec)
	e.reg.retryGrace = 0

	// A "wait" retry: same chat.
	waitPID := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "wait-failed", failedOpts{profileID: waitPID})
	e.finishRateLimited(r, time.Now().Add(300*time.Millisecond))
	var retry failoverCall
	waitUntil(t, "wait retry event", func() bool {
		for _, c := range rec.calls() {
			if c.from == "wait-failed" {
				retry = c
				return true
			}
		}
		return false
	})
	if retry.to == "" || retry.taskID != e.task.ID || retry.toChatID != r.chatID {
		t.Fatalf("wait retry event = %+v, want task %d chat %d", retry, e.task.ID, r.chatID)
	}
	waitForStatus(t, e.reg, retry.to, StatusDone, 10*time.Second)

	// A fallback successor: a new chat.
	fbPID, _ := fallbackProfiles(t, e.st)
	r2 := e.failedRun(t, "fb-failed", failedOpts{profileID: fbPID})
	e.finishRateLimited(r2, time.Now().Add(time.Hour))
	row := successorRow(t, e, "fb-failed")
	var fb failoverCall
	for _, c := range rec.calls() {
		if c.from == "fb-failed" {
			fb = c
		}
	}
	if fb.to != row.ID || fb.taskID != e.task.ID || fb.toChatID != row.ChatID || fb.toChatID == r2.chatID {
		t.Fatalf("fallback event = %+v, successor row chat %d (failed chat %d)", fb, row.ChatID, r2.chatID)
	}
	waitForStatus(t, e.reg, row.ID, StatusDone, 10*time.Second)
}
