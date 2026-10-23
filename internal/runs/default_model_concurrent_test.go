package runs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestStart_DefaultModelFlippedConcurrently starts several runs while another
// goroutine flips Runner.SetDefaultModel between two values: under -race there
// is no data race, and every run records one of the two values (never empty
// or torn).
func TestStart_DefaultModelFlippedConcurrently(t *testing.T) {
	t.Parallel()
	const n = 6
	wm, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)
	runner := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithDefaultModel(taskrunner.ProviderGLM, "fake-model-a"),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "models"}))
	reg.SetStarter(wm, runner)
	tasks := make([]int64, n)
	for i := range tasks {
		tasks[i] = newTestTask(t, wm, "hang").ID
	}

	stop := make(chan struct{})
	var flipper sync.WaitGroup
	flipper.Add(1)
	go func() {
		defer flipper.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				runner.SetDefaultModel(taskrunner.ProviderGLM, "fake-model-a")
			} else {
				runner.SetDefaultModel(taskrunner.ProviderGLM, "fake-model-b")
			}
		}
	}()

	models := make([]string, n)
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := reg.Start(context.Background(), wm, runner, tasks[i], 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
			if err != nil {
				t.Errorf("Start() error = %v", err)
				return
			}
			row, err := st.GetRun(id)
			if err != nil {
				t.Errorf("GetRun(%s) error = %v", id, err)
				return
			}
			ids[i], models[i] = id, row.Model
		}()
	}
	wg.Wait()
	close(stop)
	flipper.Wait()

	for i, m := range models {
		if ids[i] == "" {
			continue
		}
		if m != "fake-model-a" && m != "fake-model-b" {
			t.Errorf("run %d recorded model %q, want fake-model-a or fake-model-b", i, m)
		}
		if err := reg.Stop(ids[i]); err != nil {
			t.Errorf("Stop(%s) error = %v", ids[i], err)
		}
		waitForStatus(t, reg, ids[i], StatusStopped, 10*time.Second)
	}
}
