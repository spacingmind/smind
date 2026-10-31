package runs

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestFakeAgent_FailureScenarios: the fake agent's auth-error, model-error and
// transient-error scenarios end in the class their name says, with a next
// action; the real classifier and registry produce it (no live provider).
func TestFakeAgent_FailureScenarios(t *testing.T) {
	for scenario, want := range map[string]string{
		"auth-error":      "auth_expired",
		"model-error":     "model_not_in_plan",
		"transient-error": "transient_429",
		"residual-429":    "rate_limited",
	} {
		t.Run(scenario, func(t *testing.T) {
			wm, st := newTestWorkspaceManager(t)
			task := newTestTask(t, wm, scenario)
			reg := newTestRegistry(t, st)
			runner := taskrunner.New(wm,
				taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
				taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
			reg.SetStarter(wm, runner)
			id, err := reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "go", taskrunner.PermissionSettings{}, "")
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			waitForStatus(t, reg, id, StatusError, 10*time.Second)
			_, status, _ := reg.History(id)
			if status.FailureKind != want || status.FailureAction == nil || status.FailureAction.Label == "" {
				t.Fatalf("status = kind %q action %+v, want %q with an action", status.FailureKind, status.FailureAction, want)
			}
		})
	}
}
