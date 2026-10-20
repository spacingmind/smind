package service

import (
	"strings"
	"testing"
	"time"
)

// slowQueuedNotifier widens the window between a job's goroutine starting and
// the "queued" notification being delivered: it holds every queued
// notification for a while. A job's lifecycle must still be announced in
// order (queued, running, terminal) however long that takes.
type slowQueuedNotifier struct {
	*recNotifier
	hold time.Duration
}

func (n slowQueuedNotifier) NotifyJob(ws, id int64, status string) {
	if status == statusQueued {
		time.Sleep(n.hold)
	}
	n.recNotifier.NotifyJob(ws, id, status)
}

func (n slowQueuedNotifier) order() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return strings.Join(n.jobs, ",")
}

// TestReflectJob_QueuedNotifiedBeforeRunning: the job goroutine used to start
// before "queued" was sent, so a slow notifier (or a busy scheduler) made
// clients see running before queued. The notification now precedes the start.
func TestReflectJob_QueuedNotifiedBeforeRunning(t *testing.T) {
	f := newReflectFx(t)
	slow := slowQueuedNotifier{recNotifier: f.notify, hold: 100 * time.Millisecond}
	f.svc.d.Notify = slow
	f.say(grounded(f.runID, nil))
	f.reflect()
	if got := slow.order(); got != "queued,running,landed" {
		t.Fatalf("job notifications = %s, want queued,running,landed", got)
	}
}

// TestCurateJob_QueuedNotifiedBeforeRunning is the curate-job twin.
func TestCurateJob_QueuedNotifiedBeforeRunning(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(3)
	slow := slowQueuedNotifier{recNotifier: f.notify, hold: 100 * time.Millisecond}
	f.svc.d.Notify = slow
	f.curate()
	if got := slow.order(); got != "queued,running,empty" {
		t.Fatalf("job notifications = %s, want queued,running,empty", got)
	}
}
