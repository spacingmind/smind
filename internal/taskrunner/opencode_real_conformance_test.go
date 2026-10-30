package taskrunner

// Real-agent conformance tests for OpenCode over ACP: they drive the
// installed `opencode acp` binary through Runner against the fake
// OpenAI-compatible backend, proving the shipped integration works with
// the real wire protocol (configOptions discovery, permission ask flow,
// cancel, session resume) -- not just against internal/taskrunner's own
// fakeagent. All of them skip when `opencode` is not on PATH.

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeReal_ProbeDiscoversConfigOptions(t *testing.T) {
	requireOpenCodeOnPath(t)
	f := fakeOpenAIServer(t)
	_, wm, _ := newTestStoreAndManager(t)
	r := opencodeRealRunner(t, wm, opencodeIsolation(t, f.URL()))

	ctx, cancel := openCodeRealCtx(t)
	defer cancel()
	if err := r.probeACPModes(ctx, opencodeRealProvider); err != nil {
		t.Fatalf("probeACPModes() error = %v", err)
	}

	// OpenCode exposes models and modes as session/new configOptions
	// (category "model"/"mode"), never as top-level models/modes -- the
	// probe must have read the options, and the fake provider's model must
	// be among them alongside OpenCode's built-ins.
	models, ok := r.acpModelCatalog(opencodeRealProvider)
	if !ok {
		t.Fatal("no discovered model catalog")
	}
	if !modelInfoHasID(models.models, "fake/fake-model") {
		t.Fatalf("discovered models %v missing fake/fake-model", models.models)
	}
	catalog, ok := r.acpCatalog(opencodeRealProvider)
	if !ok {
		t.Fatal("no discovered mode catalog")
	}
	if !modeInfoHasID(catalog.modes, "build") || !modeInfoHasID(catalog.modes, "plan") {
		t.Fatalf("discovered modes %v missing build/plan", catalog.modes)
	}
	if catalog.defaultMode == "" {
		t.Fatalf("discovered default mode empty (catalog %+v)", catalog)
	}
	f.assertOnlySentinelAuth(t)
}

func TestOpenCodeReal_PermissionRequestedAndAllowed(t *testing.T) {
	requireOpenCodeOnPath(t)
	f := fakeOpenAIServer(t)
	_, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	r := opencodeRealRunner(t, wm, opencodeIsolation(t, f.URL()))

	// "once" is OpenCode's allow-once option id; the shipped
	// OPENCODE_PERMISSION ask policy is what makes it ask at all.
	decider := &recordingDecider{optionID: "once"}
	ctx, cancel := openCodeRealCtx(t)
	defer cancel()
	got, err := runOpenCodeRealPrompt(ctx, r, task, "RUNTOOL please run a command", decider)
	if err != nil {
		t.Fatalf("RunPrompt() error = %v (events: %+v)", err, got)
	}

	if decider.gotSummary == "" {
		t.Fatal("decider was never called: OpenCode did not ask for the bash tool (the shipped OPENCODE_PERMISSION ask policy did not apply)")
	}
	var sawTool, sawText, sawDone bool
	for _, e := range got {
		switch e.Type {
		case EventTypeToolCall:
			if e.ToolTitle == "bash" || e.ToolName == "bash" {
				sawTool = true
			}
		case EventTypeText:
			if strings.Contains(e.Text, "FAKE") {
				sawText = true
			}
		case EventTypeDone:
			sawDone = true
		}
	}
	if !sawTool {
		t.Fatalf("events carry no bash tool call: %+v", got)
	}
	if !sawText {
		t.Fatalf("events carry no assistant text containing FAKE: %+v", got)
	}
	if !sawDone {
		t.Fatalf("events carry no done event: %+v", got)
	}
	f.assertOnlySentinelAuth(t)
}

func TestOpenCodeReal_Cancel(t *testing.T) {
	requireOpenCodeOnPath(t)
	f := fakeOpenAIServer(t)
	_, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	r := opencodeRealRunner(t, wm, opencodeIsolation(t, f.URL()))

	baseCtx, baseCancel := openCodeRealCtx(t)
	defer baseCancel()
	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, opencodeRealProvider, "SLOW tell me a long story", nil, PermissionSettings{}, "", "", events)
	}()

	// The fake backend streams for ~10s; cancel as soon as the first
	// assistant text arrives.
	timeout := time.After(60 * time.Second)
waitForText:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("events closed before the first assistant text arrived")
			}
			if e.Type == EventTypeText {
				break waitForText
			}
		case <-timeout:
			t.Fatal("timed out waiting for the first assistant text")
		}
	}
	cancel()
	select {
	case <-errCh: // nil or context.Canceled, both acceptable
	case <-time.After(5 * time.Second):
		t.Fatal("RunPrompt() did not return within 5s of cancellation")
	}
	for range events { // drain the remainder so the goroutine winds down
	}
	if len(f.chatCalls()) == 0 {
		t.Fatal("fake backend never saw the prompt request")
	}
	f.assertOnlySentinelAuth(t)
}

func TestOpenCodeReal_ResumeLoadsSession(t *testing.T) {
	requireOpenCodeOnPath(t)
	f := fakeOpenAIServer(t)
	_, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	// One isolation set for both runs: OpenCode persists sessions under
	// XDG_DATA_HOME, so the second run can only resume if the dirs match.
	env := opencodeIsolation(t, f.URL())
	r := opencodeRealRunner(t, wm, env)

	ctx, cancel := openCodeRealCtx(t)
	defer cancel()
	got1, err := runOpenCodeRealPrompt(ctx, r, task, "hello first", nil)
	if err != nil {
		t.Fatalf("RunPrompt() (first run) error = %v (events: %+v)", err, got1)
	}
	first, ok := r.sessionStore.Get(task.ID)
	if !ok || first.SessionID == "" || first.Provider != opencodeRealProvider {
		t.Fatalf("session handle after first run = %+v (found=%v)", first, ok)
	}

	got2, err := runOpenCodeRealPrompt(ctx, r, task, "SECOND run, what did I say before?", nil)
	if err != nil {
		t.Fatalf("RunPrompt() (second run) error = %v", err)
	}
	for _, e := range got2 {
		if e.Type == EventTypeSessionNote {
			t.Fatalf("second run degraded to a fresh session: %s", e.Text)
		}
	}
	second, _ := r.sessionStore.Get(task.ID)
	if second.SessionID != first.SessionID {
		t.Fatalf("second run session id = %q, want the resumed %q", second.SessionID, first.SessionID)
	}

	// The resumed conversation must carry the first turn's history: the
	// second model call contains an assistant message (the first turn's
	// "FAKE reply done." reply) before the new user turn.
	chats := f.chatCalls()
	if len(chats) < 2 {
		t.Fatalf("fake backend served %d chat completions, want >= 2", len(chats))
	}
	last := chats[len(chats)-1]
	if !last.hasAssistant {
		t.Fatal("second model call carried no assistant history: the session was not resumed with its prior turns")
	}
	if !strings.Contains(last.lastUser, "SECOND") {
		t.Fatalf("second model call's last user message = %q, want the second prompt", last.lastUser)
	}
	f.assertOnlySentinelAuth(t)
}

func modelInfoHasID(models []ModelInfo, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

func modeInfoHasID(modes []ModeInfo, id string) bool {
	for _, m := range modes {
		if m.ID == id {
			return true
		}
	}
	return false
}
