package runs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestStart_ChatProviderMismatchNamesFix: a chat is bound to its first
// provider (ADR-0016); sending with another one fails, and the error says how
// to recover, with the real task id, instead of leaving the caller guessing.
func TestStart_ChatProviderMismatchNamesFix(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID := e.startRun(t, "first") // binds the default chat to glm
	t.Cleanup(func() { _ = e.reg.Stop(runID) })

	_, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "second", taskrunner.PermissionSettings{}, "")
	if err == nil {
		t.Fatal("Start(other provider) error = nil, want the binding error")
	}
	for _, want := range []string{
		`is bound to provider "glm", got "claude-native"`,
		"start a new chat for this provider",
		fmt.Sprintf("smind task chat new %d", e.task.ID),
		"--chat <id>",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}
