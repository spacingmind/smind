package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/crash"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type panicHooks struct{}

func (panicHooks) BeforeStart(context.Context, HookStart) (HookEdits, error) {
	panic("SECRET-HOOK-TEXT")
}

// crash-run-drive-panic-finalises-run: a panic inside drive is recorded and
// the run still ends as an error, instead of staying "running" forever.
func TestCrashRunDrivePanicFinalisesRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	crash.Init(crash.Config{Dir: dir, Source: "daemon", Version: "test", MaxPerMinute: 1000})
	t.Cleanup(func() { crash.Init(crash.Config{}) })

	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}), taskrunner.WithSessionStore(taskrunner.NewMemorySessionStore()))
	reg := newTestRegistry(t, st)
	reg.SetLaunchHooks(panicHooks{})

	runID, err := reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "my prompt", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusError, 10*time.Second)
	files, _ := filepath.Glob(filepath.Join(dir, "crash-*.json"))
	if len(files) != 1 {
		t.Fatalf("reports = %d, want 1", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"site":"runs.drive"`) || strings.Contains(string(data), "SECRET-HOOK-TEXT") {
		t.Fatalf("report lost its site or leaked the panic text: %s", data)
	}
}
