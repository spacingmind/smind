package runs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// resolvedRecorder is a Notifier that records only permission.resolved
// notifications.
type resolvedRecorder struct {
	mu   sync.Mutex
	got  []resolvedCall
	wake chan struct{}
}

type resolvedCall struct {
	runID                       string
	taskID, chatID              int64
	requestID, optionID, reason string
}

func newResolvedRecorder() *resolvedRecorder {
	return &resolvedRecorder{wake: make(chan struct{}, 16)}
}

func (n *resolvedRecorder) NotifyRunFailover(string, string, int64, int64) {}
func (n *resolvedRecorder) NotifyRunStatus(RunStatus)                      {}
func (n *resolvedRecorder) NotifyPermissionPending(string, int64, int64, string, string, []taskrunner.PermissionOption) {
}
func (n *resolvedRecorder) NotifyChatQueueUpdated(int64, []store.ChatQueueItem) {}
func (n *resolvedRecorder) NotifyPermissionResolved(runID string, taskID, chatID int64, requestID, optionID, reason string) {
	n.mu.Lock()
	n.got = append(n.got, resolvedCall{runID, taskID, chatID, requestID, optionID, reason})
	n.mu.Unlock()
	n.wake <- struct{}{}
}

func (n *resolvedRecorder) next(t *testing.T) resolvedCall {
	t.Helper()
	select {
	case <-n.wake:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for NotifyPermissionResolved")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.got[len(n.got)-1]
}

func (n *resolvedRecorder) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.got)
}

func startPermissionRun(t *testing.T, n Notifier, timeout time.Duration) (*Registry, string, taskrunner.Event) {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	reg := newTestRegistry(t, st)
	reg.SetNotifier(n)
	if timeout > 0 {
		reg.SetPermissionTimeout(timeout)
	}
	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return reg, runID, waitForPermissionRequest(t, reg, runID, 5*time.Second)
}

func TestPermissionResolvedPublishedOnHumanAnswer(t *testing.T) {
	t.Parallel()
	n := newResolvedRecorder()
	reg, runID, req := startPermissionRun(t, n, 0)
	if err := reg.RespondPermission(runID, req.PermissionRequestID, "allow-1"); err != nil {
		t.Fatalf("RespondPermission() error = %v", err)
	}
	got := n.next(t)
	if got.runID != runID || got.requestID != req.PermissionRequestID || got.optionID != "allow-1" || got.reason != "human" {
		t.Fatalf("resolved = %+v, want run %q request %q allow-1 human", got, runID, req.PermissionRequestID)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
}

func TestPermissionResolvedPublishedOnTimeout(t *testing.T) {
	t.Parallel()
	n := newResolvedRecorder()
	reg, runID, req := startPermissionRun(t, n, 50*time.Millisecond)
	got := n.next(t)
	if got.requestID != req.PermissionRequestID || got.optionID != "deny-1" || got.reason != "timeout" {
		t.Fatalf("resolved = %+v, want deny-1 timeout for %q", got, req.PermissionRequestID)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
}

func TestPermissionResolvedPublishedWhenRunStopped(t *testing.T) {
	t.Parallel()
	n := newResolvedRecorder()
	reg, runID, req := startPermissionRun(t, n, 0)
	if err := reg.Stop(runID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	got := n.next(t)
	if got.requestID != req.PermissionRequestID || got.optionID != "" || got.reason != "runEnded" {
		t.Fatalf("resolved = %+v, want runEnded with no option for %q", got, req.PermissionRequestID)
	}
	waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
}

func TestAutoAcceptPublishesNoPermissionResolved(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{AutoAccept: true})
	n := newResolvedRecorder()
	reg.SetNotifier(n)
	if _, err := (runPermissionDecider{reg: reg, r: r}).Decide(context.Background(), "risky", allowDenyOptions); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if c := n.count(); c != 0 {
		t.Fatalf("NotifyPermissionResolved calls = %d, want 0 for AutoAccept", c)
	}
}
