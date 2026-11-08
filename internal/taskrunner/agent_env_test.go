package taskrunner

import (
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func assertAgentMarker(t *testing.T, task store.Task) {
	t.Helper()
	if got := lastEnvEntry(t, readLaunchEnv(t, task), AgentRunEnv); got != AgentRunEnv+"=1" {
		t.Fatalf("%s = %q, want the agent-run marker", AgentRunEnv, got)
	}
}

// Every harness launch of an agent run carries SMIND_AGENT_RUN, which is what
// tells a smind command typed in the agent's shell that it is not the human's.
func TestLaunchEnv_AgentRunMarker_Claude(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	assertAgentMarker(t, task)
}

func TestLaunchEnv_AgentRunMarker_Codex(t *testing.T) {
	t.Setenv("FAKE_DUMP_ENV", "1")
	wm, task := newTestTask(t, "")
	runScratchTurn(t, codexRunner(wm), task, ProviderCodexNative)
	assertAgentMarker(t, task)
}

func TestLaunchEnv_AgentRunMarker_ACP(t *testing.T) {
	t.Setenv("FAKE_DUMP_ENV", "1")
	wm, task := newTestTask(t, "")
	runScratchTurn(t, glmRunner(wm), task, ProviderKimi)
	assertAgentMarker(t, task)
}

func TestLaunchEnv_AgentRunMarker_GLM(t *testing.T) {
	t.Setenv("FAKE_DUMP_ENV", "1")
	wm, task := newTestTask(t, "")
	runScratchTurn(t, New(wm, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath})), task, ProviderGLM)
	assertAgentMarker(t, task)
}
