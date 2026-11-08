package wsapi

import (
	"slices"
	"testing"
)

// ADR-0055 §3: answering a question is human-only. The RPCs exist, no plugin
// session can reach them, and the question topics (agent text) are not offered
// to plugins either.
func TestWsapi_AnswerQuestionIsHumanOnly(t *testing.T) {
	e := newProcEnv(t)
	for _, m := range []string{"run.answerQuestion", "run.pendingQuestions"} {
		if _, ok := e.api.hs[m]; !ok {
			t.Errorf("%s is not a wsapi method", m)
		}
		if !pluginNever(m) {
			t.Errorf("%s is reachable from a plugin session", m)
		}
		for capName, ms := range PluginCapabilityTable {
			for _, pm := range ms {
				if pm.Name == m {
					t.Errorf("%s is granted by plugin capability %q", m, capName)
				}
			}
		}
	}
	for _, topic := range []string{TopicQuestionPending, TopicQuestionResolved} {
		if !knownTopics[topic] {
			t.Errorf("%s is not a subscribable topic", topic)
		}
		if pluginTopicSet[topic] {
			t.Errorf("%s (agent text) is offered to plugins", topic)
		}
	}
}

// An agent's own connection (identified, run scoped: what the MCP server and
// every `smind mcp serve` tool use) can reach neither question method: they are
// not on RunScopedMethods and an identified connection is refused before any
// handler runs (ADR-0055 §3, ADR-0017). The other half of the guarantee, the
// CLI refusing inside an agent shell, is TestTaskAnswer_RefusedInsideAnAgentRun.
func TestWsapi_QuestionMethodsRefusedForRunScoped(t *testing.T) {
	t.Parallel()
	for _, m := range []string{"run.answerQuestion", "run.pendingQuestions"} {
		if slices.Contains(RunScopedMethods, m) {
			t.Fatalf("%s is on RunScopedMethods", m)
		}
	}
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	for _, m := range []string{"run.answerQuestion", "run.pendingQuestions"} {
		wantForbidden(t, callWS(t, ws, m, map[string]any{"runId": e.runID, "requestId": "x", "action": "decline"}), "not available to agents")
	}
}
