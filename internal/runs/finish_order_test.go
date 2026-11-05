package runs

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestReaderSeeingDoneSeesNativeRef: native_ref is persisted before "done" is
// visible; the skeleton form may still lag it.
func TestReaderSeeingDoneSeesNativeRef(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}), taskrunner.WithSessionStore(taskrunner.NewMemorySessionStore()))
	reg := newTestRegistry(t, st)

	var runID string
	idReady := make(chan struct{}) // closed once runID is set (the hook may fire before Start returns)
	atPublished := make(chan string, 1)
	release := make(chan struct{})
	reg.finishStage = func(stage string) {
		if stage != "published" {
			return
		}
		<-idReady
		sk, _ := st.GetRunSkeleton(runID)
		atPublished <- sk.NativeRef
		<-release
	}
	var err error
	runID, err = reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "my prompt", taskrunner.PermissionSettings{}, "")
	close(idReady)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	var ref string
	select {
	case ref = <-atPublished:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("finish never reached the published stage")
	}
	if ref == "" {
		close(release)
		t.Fatal("native_ref empty when done was published")
	}
	if _, status, err := reg.History(runID); err != nil || status.Status != StatusDone {
		close(release)
		t.Fatalf("status at published = %+v, %v; want done", status, err)
	}
	if sk, _ := st.GetRunSkeleton(runID); sk.SkeletonAt != nil {
		close(release)
		t.Fatal("skeleton already applied at the published stage")
	}
	close(release)
	waitForStatus(t, reg, runID, StatusDone, 10*time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for {
		sk, err := st.GetRunSkeleton(runID)
		if err == nil && sk.SkeletonAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("skeleton never applied")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCloseAllWaitsForTheFinalizer: a run whose status is already done but
// whose finalizer is still running keeps CloseAll from returning.
func TestCloseAllWaitsForTheFinalizer(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}), taskrunner.WithSessionStore(taskrunner.NewMemorySessionStore()))
	reg := newTestRegistry(t, st)

	atFinalizing := make(chan struct{}, 1)
	release := make(chan struct{})
	reg.finishStage = func(stage string) {
		if stage == "finalizing" {
			atFinalizing <- struct{}{}
			<-release
		}
	}
	runID, err := reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "p", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 10*time.Second)
	select {
	case <-atFinalizing:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("finish never reached the finalizing stage")
	}

	closed := make(chan struct{})
	go func() { reg.CloseAll(); close(closed) }()
	select {
	case <-closed:
		close(release)
		t.Fatal("CloseAll returned while the finalizer was still running")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("CloseAll did not return after the finalizer was released")
	}
	sk, err := st.GetRunSkeleton(runID)
	if err != nil || sk.SkeletonAt == nil {
		t.Fatalf("skeleton after CloseAll = %+v, %v", sk, err)
	}
}
