package taskrunner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const testRoleText = "You are the REVIEWER seat. Never merge."

func runContract(t *testing.T, r *Runner, taskID int64, p Provider, prompt, instructions string) []Event {
	t.Helper()
	ctx := context.Background()
	if instructions != "" {
		ctx = WithRunContract(ctx, RunContract{Instructions: instructions})
	}
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, taskID, taskID, p, prompt, denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return got
}

func TestRunContractFrom_RoundTrip(t *testing.T) {
	t.Parallel()
	if c := runContractFrom(context.Background()); c.Instructions != "" {
		t.Fatalf("empty ctx has a contract: %+v", c)
	}
	if c := runContractFrom(WithRunContract(context.Background(), RunContract{Instructions: "x"})); c.Instructions != "x" {
		t.Fatalf("got %+v", c)
	}
}

func TestRunClaude_AppendsInstructions(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	runContract(t, r, task.ID, ProviderClaudeNative, "hi", testRoleText)
	if v, ok := flagValue(readArgs(t, task), "--append-system-prompt"); !ok || v != testRoleText {
		t.Fatalf("--append-system-prompt = %q (present %v), want %q", v, ok, testRoleText)
	}
	// The second run resumes the session and still carries the text.
	runContract(t, r, task.ID, ProviderClaudeNative, "again", testRoleText)
	args := readArgs(t, task)
	if _, ok := flagValue(args, "--resume"); !ok {
		t.Fatalf("second run did not resume: %q", args)
	}
	if v, ok := flagValue(args, "--append-system-prompt"); !ok || v != testRoleText {
		t.Fatalf("resumed --append-system-prompt = %q (present %v)", v, ok)
	}
}

func TestRunClaude_NoInstructionsNoOption(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	runContract(t, r, task.ID, ProviderClaudeNative, "hi", "")
	if _, ok := flagValue(readArgs(t, task), "--append-system-prompt"); ok {
		t.Fatal("--append-system-prompt present without instructions")
	}
}

func TestRunACP_PrefaceOnNewSession(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	r := glmRunnerWithCaps(wm, "loadSession")
	runContract(t, r, task.ID, ProviderGLM, "do the thing", testRoleText)
	got := promptBlocks(t, task)
	want := "[Role instructions for this session]\n" + testRoleText + "\n[End of role instructions]\n\ndo the thing"
	if len(got) != 1 || len(got[0]) != 1 || got[0][0] != want {
		t.Fatalf("prompt blocks = %q, want [%q]", got, want)
	}
}

func TestRunACP_NoPrefaceOnResumedSession(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	r := glmRunnerWithCaps(wm, "loadSession")
	runContract(t, r, task.ID, ProviderGLM, "first", testRoleText)
	runContract(t, r, task.ID, ProviderGLM, "second", testRoleText)
	got := promptBlocks(t, task)
	if len(got) != 2 || len(got[1]) != 1 || got[1][0] != "second" {
		t.Fatalf("prompt blocks = %q, want the resumed prompt to be just the user text", got)
	}
}

func TestRunACP_NoInstructionsNoPreface(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	r := glmRunnerWithCaps(wm, "loadSession")
	runContract(t, r, task.ID, ProviderGLM, "plain", "")
	got := promptBlocks(t, task)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0] != "plain" {
		t.Fatalf("prompt blocks = %q", got)
	}
}

// The preface is added inside RunPrompt: the caller's prompt (what the
// registry records as runs.prompt) is untouched and no event carries it.
func TestRun_PromptAndTranscriptStayUserText(t *testing.T) {
	t.Parallel()
	for _, p := range []Provider{ProviderGLM, ProviderClaudeNative, ProviderCodexNative} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			scenario := ""
			if p == ProviderClaudeNative {
				scenario = "echo-args"
			} else if p == ProviderCodexNative {
				scenario = "reply"
			}
			wm, task := newTestTask(t, scenario)
			var r *Runner
			switch p {
			case ProviderClaudeNative:
				r = claudeNativeRunner(t, wm)
			case ProviderCodexNative:
				r = codexRunner(wm)
			default:
				r = glmRunnerWithCaps(wm, "loadSession")
			}
			events := runContract(t, r, task.ID, p, "user text", testRoleText)
			for _, e := range events {
				if strings.Contains(e.Text, "Role instructions") || strings.Contains(e.Text, testRoleText) {
					t.Fatalf("event leaks the role text: %+v", e)
				}
			}
		})
	}
}

func TestRunCodex_DeveloperInstructions(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "reply")
	r := codexRunner(wm)
	path := filepath.Join(*task.WorktreePath, "thread-instructions")
	runContract(t, r, task.ID, ProviderCodexNative, "hi", testRoleText)
	if got := readLog(t, path); got != testRoleText {
		t.Fatalf("thread/start developerInstructions = %q, want %q", got, testRoleText)
	}
	runContract(t, r, task.ID, ProviderCodexNative, "again", testRoleText)
	if got := readLog(t, path); got != testRoleText {
		t.Fatalf("thread/resume developerInstructions = %q, want %q", got, testRoleText)
	}
}

func TestRunCodex_NoInstructionsNoField(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "reply")
	runContract(t, codexRunner(wm), task.ID, ProviderCodexNative, "hi", "")
	if got := readLog(t, filepath.Join(*task.WorktreePath, "thread-instructions")); got != "" {
		t.Fatalf("developerInstructions = %q without instructions", got)
	}
}
