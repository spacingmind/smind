package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// newIsolatedTask is newTestTask with the worktree's .git link removed, so
// the cwd guard (ErrIsolationCwd) accepts it.
func newIsolatedTask(t *testing.T, scenario string) (*workspace.Manager, store.Task) {
	t.Helper()
	wm, task := newTestTask(t, scenario)
	if err := os.RemoveAll(filepath.Join(*task.WorktreePath, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}
	return wm, task
}

// runIsolated runs RunPromptIsolated to completion and returns the events
// and the error.
func runIsolated(t *testing.T, r *Runner, task store.Task, provider Provider, iso IsolationOptions) ([]Event, error) {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPromptIsolated(context.Background(), task.ID, task.ID, provider, "hi", testModelFor(provider), iso, events)
	}()
	got := drainEvents(events)
	return got, <-errCh
}

func readArgs(t *testing.T, task store.Task) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	return strings.Split(string(data), "\n")
}

func assertIsolatedFlags(t *testing.T, args []string, systemPrompt string) {
	t.Helper()
	if v, ok := flagValue(args, "--tools"); !ok || v != "" {
		t.Errorf("--tools = %q (present %v), want present and empty; args %q", v, ok, args)
	}
	for _, f := range []string{"--strict-mcp-config", "--setting-sources", "--disable-slash-commands"} {
		if _, ok := flagValue(args, f); !ok {
			t.Errorf("%s missing from args %q", f, args)
		}
	}
	if v, _ := flagValue(args, "--setting-sources"); v != "" {
		t.Errorf("--setting-sources = %q, want empty", v)
	}
	if v, _ := flagValue(args, "--max-turns"); v != "1" {
		t.Errorf("--max-turns = %q, want 1", v)
	}
	if v, _ := flagValue(args, "--permission-mode"); v != ClaudeModePlan {
		t.Errorf("--permission-mode = %q, want plan", v)
	}
	if v, _ := flagValue(args, "--system-prompt"); v != systemPrompt {
		t.Errorf("--system-prompt = %q, want %q", v, systemPrompt)
	}
	for _, f := range []string{"--mcp-config", "--add-dir", "--allowedTools", "--disallowedTools", "--allow-dangerously-skip-permissions", "--resume", "--plugin-dir"} {
		if v, ok := flagValue(args, f); ok {
			t.Errorf("%s = %q present, want absent; args %q", f, v, args)
		}
	}
}

func TestReflector_HasNoTools(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)

	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "you reflect"})
	if err != nil {
		t.Fatalf("RunPromptIsolated() error = %v", err)
	}
	if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("want a final Done event, got %+v", got)
	}
	args := readArgs(t, task)
	t.Logf("emitted args: %q", args)
	assertIsolatedFlags(t, args, "you reflect")
}

func TestReflector_NoMcpNoAddDir(t *testing.T) {
	t.Parallel()
	r, st, task := newClaudeMcpEnv(t)
	if err := os.RemoveAll(filepath.Join(*task.WorktreePath, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}
	if _, err := mcpservers.New(st).Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Args: `["-y"]`, Enabled: true}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	// Sanity: a normal run on this fixture does carry --mcp-config.
	if _, ok := flagValue(runClaudeMcp(t, r, task), "--mcp-config"); !ok {
		t.Fatal("fixture broken: normal run has no --mcp-config")
	}
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p", MaxTurns: 0}); err != nil {
		t.Fatalf("RunPromptIsolated() error = %v", err)
	}
	assertIsolatedFlags(t, readArgs(t, task), "p")
}

func TestRunPromptIsolated_DisablesClaudeAiConnectors(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env-claudeai-mcp"))
	if err != nil || string(data) != "false" {
		t.Fatalf("ENABLE_CLAUDEAI_MCP_SERVERS = %q (err %v), want false", data, err)
	}
}

func TestRunPromptIsolated_CwdGuard(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"CLAUDE.md", ".claude", ".mcp.json", "AGENTS.md", ".git"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wm, task := newIsolatedTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)
			path := filepath.Join(*task.WorktreePath, name)
			if name == ".claude" || name == ".git" {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
			if !errors.Is(err, ErrIsolationCwd) {
				t.Fatalf("error = %v, want ErrIsolationCwd", err)
			}
			if len(got) != 0 {
				t.Fatalf("events = %+v, want none", got)
			}
			if _, statErr := os.Stat(filepath.Join(*task.WorktreePath, "args")); !os.IsNotExist(statErr) {
				t.Fatal("a subprocess was started")
			}
		})
	}
}

func TestRunPromptIsolated_CwdGuardRejectsMissingDir(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	if err := os.RemoveAll(*task.WorktreePath); err != nil {
		t.Fatal(err)
	}
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); !errors.Is(err, ErrIsolationCwd) {
		t.Fatalf("error = %v, want ErrIsolationCwd", err)
	}
}

func TestRunPromptIsolated_TimeoutFails(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "hang")
	r := claudeNativeRunner(t, wm)
	start := time.Now()
	_, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p", Timeout: 200 * time.Millisecond})
	if !errors.Is(err, ErrIsolatedRunFailed) || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v, want ErrIsolatedRunFailed wrapping timeout", err)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestToolNameForError(t *testing.T) {
	t.Parallel()
	if got := safeToolName("mcp__x.y:z-1"); got != "mcp__x.y:z-1" {
		t.Fatalf("got %q", got)
	}
	if got := safeToolName("a b\n\"é"); got != "a_b___" {
		t.Fatalf("got %q", got)
	}
	if got := safeToolName(strings.Repeat("a", 100)); len(got) != 64 {
		t.Fatalf("len = %d, want 64", len(got))
	}
}

func TestRunPromptIsolated_ClosesClientBeforeDone(t *testing.T) {
	t.Parallel()
	// A consumer that never reads the final Done event must not keep the
	// CLI alive: the run returns once ctx ends, and the client was closed
	// before the Done send was attempted.
	wm, task := newIsolatedTask(t, "tool-attempt")
	r := claudeNativeRunner(t, wm)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := make(chan Event) // never drained after the first read
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPromptIsolated(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", testClaudeModel, IsolationOptions{SystemPrompt: "p"}, events)
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrIsolationViolated) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunPromptIsolated blocked on a slow consumer")
	}
}

func TestReflector_MaxTurnsOverride(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p", MaxTurns: 3}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if v, _ := flagValue(readArgs(t, task), "--max-turns"); v != "3" {
		t.Fatalf("--max-turns = %q, want 3", v)
	}
}

func TestReflector_UnchangedRunPromptFlags(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	args := readArgs(t, task)
	for _, f := range []string{"--tools", "--strict-mcp-config", "--setting-sources", "--max-turns", "--disable-slash-commands"} {
		if v, ok := flagValue(args, f); ok {
			t.Errorf("normal RunPrompt leaked %s=%q; args %q", f, v, args)
		}
	}
	if data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env-claudeai-mcp")); err != nil || string(data) != "" {
		t.Errorf("normal RunPrompt ENABLE_CLAUDEAI_MCP_SERVERS = %q (err %v), want unset", data, err)
	}
	if _, ok := flagValue(args, "--allow-dangerously-skip-permissions"); !ok {
		t.Errorf("normal RunPrompt lost --allow-dangerously-skip-permissions; args %q", args)
	}
}

func TestRunPromptIsolated_RefusesAcpProviders(t *testing.T) {
	t.Parallel()
	for _, p := range []Provider{ProviderGLM, ProviderKimi, ProviderCodexNative} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			wm, task := newIsolatedTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)
			got, err := runIsolated(t, r, task, p, IsolationOptions{SystemPrompt: "p"})
			if !errors.Is(err, ErrIsolationUnsupported) {
				t.Fatalf("error = %v, want ErrIsolationUnsupported", err)
			}
			if len(got) != 0 {
				t.Fatalf("events = %+v, want none (channel closed)", got)
			}
			if _, statErr := os.Stat(filepath.Join(*task.WorktreePath, "args")); !os.IsNotExist(statErr) {
				t.Fatalf("a subprocess was started (args file exists, stat err %v)", statErr)
			}
		})
	}
}

func TestRunPromptIsolated_RequiresSystemPromptAndTask(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{}); err == nil {
		t.Fatal("empty SystemPrompt: want error")
	}
	if _, statErr := os.Stat(filepath.Join(*task.WorktreePath, "args")); !os.IsNotExist(statErr) {
		t.Fatal("subprocess started despite invalid options")
	}
	task.ID = 99999
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); err == nil {
		t.Fatal("unknown task: want error")
	}
}

func TestRunPromptIsolated_DoesNotTouchSessionStore(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)

	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if h, ok := r.sessionStore.Get(task.ID); ok {
		t.Fatalf("session handle stored after isolated run: %+v", h)
	}

	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "stale-sess"})
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if v, ok := flagValue(readArgs(t, task), "--resume"); ok {
		t.Fatalf("--resume = %q passed to isolated run", v)
	}
	if h, _ := r.sessionStore.Get(task.ID); h.SessionID != "stale-sess" {
		t.Fatalf("stored handle overwritten: %+v", h)
	}
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if len(r.claudeClients) != 0 {
		t.Fatalf("claudeClients = %d entries, want none tracked", len(r.claudeClients))
	}
}

func TestRunPromptIsolated_ToolAttemptAborts(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "tool-attempt")
	r := claudeNativeRunner(t, wm)

	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if !errors.Is(err, ErrIsolationViolated) {
		t.Fatalf("error = %v, want ErrIsolationViolated", err)
	}
	if !strings.Contains(err.Error(), "Bash") {
		t.Fatalf("error %q does not name the tool", err)
	}
	if len(got) == 0 {
		t.Fatal("no events")
	}
	last := got[len(got)-1]
	if last.Type != EventTypeDone || last.StopReason != "isolation_violated" {
		t.Fatalf("last event = %+v, want Done/isolation_violated", last)
	}
	for _, e := range got[:len(got)-1] {
		if e.Type == EventTypeToolCall || e.Type == EventTypeText {
			t.Fatalf("event after/at the violation leaked: %+v (all %+v)", e, got)
		}
	}
}

func TestRunPromptIsolated_PermissionRequestDenied(t *testing.T) {
	t.Parallel()
	t.Run("denies every permission request", func(t *testing.T) {
		t.Parallel()
		wm, task := newIsolatedTask(t, "permission")
		r := claudeNativeRunner(t, wm)
		got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if txt := FinalText(got); txt != "chose:deny" {
			t.Fatalf("final text = %q, want chose:deny", txt)
		}
	})
}

func TestRunPromptIsolated_ReturnsFinalAssistantText(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "reply")
	r := claudeNativeRunner(t, wm)
	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if txt := FinalText(got); txt != "hello from claude" {
		t.Fatalf("FinalText = %q", txt)
	}
}

func TestFinalText(t *testing.T) {
	t.Parallel()
	txt := func(s string) Event { return Event{Type: EventTypeText, Text: s} }
	for _, tc := range []struct {
		name   string
		events []Event
		want   string
	}{
		{"empty", nil, ""},
		{"single", []Event{txt("a")}, "a"},
		{"chunks then done", []Event{txt("a"), txt("b"), {Type: EventTypeDone}}, "ab"},
		{"last turn only", []Event{txt("old"), {Type: EventTypeToolCall}, txt("x"), txt("y"), {Type: EventTypeDone}}, "xy"},
		{"thinking separates", []Event{txt("old"), {Type: EventTypeThinking, Text: "t"}, txt("new")}, "new"},
		{"trailing non-text skipped", []Event{txt("a"), {Type: EventTypeUsage}, {Type: EventTypeDone}}, "a"},
		{"no text", []Event{{Type: EventTypeDone}}, ""},
		{"violated run yields nothing", []Event{txt("a"), {Type: EventTypeDone, StopReason: "isolation_violated"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FinalText(tc.events); got != tc.want {
				t.Fatalf("FinalText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunPromptIsolated_InitWithToolsAborts(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "init-with-tools")
	r := claudeNativeRunner(t, wm)
	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if !errors.Is(err, ErrIsolationViolated) {
		t.Fatalf("error = %v, want ErrIsolationViolated", err)
	}
	if !strings.Contains(err.Error(), "init reported tools") {
		t.Fatalf("error %q does not say init reported tools", err)
	}
	if last := got[len(got)-1]; last.Type != EventTypeDone || last.StopReason != "isolation_violated" {
		t.Fatalf("last event = %+v, want Done/isolation_violated", last)
	}
	if txt := FinalText(got); txt != "" {
		t.Fatalf("FinalText = %q, want empty after a violation", txt)
	}
}

func TestRunPromptIsolated_InitCleanProceeds(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "init-clean")
	r := claudeNativeRunner(t, wm)
	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if err != nil {
		t.Fatalf("error = %v (non-empty agents and empty collections must pass)", err)
	}
	if txt := FinalText(got); txt != "after init" {
		t.Fatalf("FinalText = %q", txt)
	}
}

func TestRunPromptIsolated_InitBuiltinPluginsProceed(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "init-builtin-plugins")
	r := claudeNativeRunner(t, wm)
	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if err != nil {
		t.Fatalf("error = %v (the CLI's compiled-in plugins are not an isolation leak)", err)
	}
	if txt := FinalText(got); txt != "after init" {
		t.Fatalf("FinalText = %q", txt)
	}
}

func TestRunPromptIsolated_InitUserPluginAborts(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "init-user-plugin")
	r := claudeNativeRunner(t, wm)
	_, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if !errors.Is(err, ErrIsolationViolated) || !strings.Contains(err.Error(), "init reported plugins") {
		t.Fatalf("error = %v, want ErrIsolationViolated naming plugins", err)
	}
}

func TestInitViolation_RealCLIShape(t *testing.T) {
	t.Parallel()
	// Trimmed from a live claude 2.1.293 init message (isolated flags).
	const real = `{"type":"system","subtype":"init","tools":[],"mcp_servers":[],"slash_commands":[],"skills":[],` +
		`"agents":["claude","Explore"],"plugins":[{"name":"cc-plugin-sec-default","path":"builtin","source":"cc-plugin-sec-default@builtin"},` +
		`{"name":"cc-plugin-telemetry","path":"builtin","source":"cc-plugin-telemetry@builtin"}]}`
	if got := initViolation(json.RawMessage(real)); got != "" {
		t.Fatalf("initViolation(real isolated init) = %q, want none", got)
	}
	leak := strings.Replace(real, `"skills":[]`, `"skills":["learned-x"]`, 1)
	if got := initViolation(json.RawMessage(leak)); got != "skills" {
		t.Fatalf("initViolation(skills leak) = %q, want skills", got)
	}
}

func TestInitViolation_PluginExemptionIsNarrow(t *testing.T) {
	t.Parallel()
	const builtin = `{"name":"cc-x","path":"builtin","source":"cc-x@builtin"}`
	for _, tc := range []struct {
		name string
		init string
		want string
	}{
		{"builtin only", `{"plugins":[` + builtin + `]}`, ""},
		{"user plugin first", `{"plugins":[{"name":"mine","path":"/p/mine","source":"mine@local"},` + builtin + `]}`, "plugins"},
		{"path builtin, foreign source", `{"plugins":[{"name":"mine","path":"builtin","source":"mine@local"}]}`, "plugins"},
		{"path builtin, no source", `{"plugins":[{"name":"mine","path":"builtin"}]}`, "plugins"},
		{"path builtin, no name", `{"plugins":[{"path":"builtin","source":"@builtin"}]}`, "plugins"},
		{"non-object entry", `{"plugins":["x"]}`, "plugins"},
		{"null entry", `{"plugins":[null]}`, "plugins"},
		{"builtin plus skill", `{"plugins":[` + builtin + `],"skills":["s"]}`, "skills"},
		{"builtin plus slash command", `{"plugins":[` + builtin + `],"slash_commands":["/x"]}`, "slash_commands"},
		{"builtin plus mcp server", `{"plugins":[` + builtin + `],"mcp_servers":[{"name":"m"}]}`, "mcp_servers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := initViolation(json.RawMessage(tc.init)); got != tc.want {
				t.Fatalf("initViolation = %q, want %q", got, tc.want)
			}
		})
	}
}

// usageEvents returns the EventTypeUsage events of got.
func usageEvents(got []Event) []Usage {
	var out []Usage
	for _, e := range got {
		if e.Type == EventTypeUsage && e.Usage != nil {
			out = append(out, *e.Usage)
		}
	}
	return out
}

func TestRunPromptIsolated_FailedRunsAreMetered(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		scenario string
		wantErr  error
	}{
		{"error result", "result-error-usage", ErrIsolatedRunFailed},
		{"violated run", "result-denials-usage", ErrIsolationViolated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newIsolatedTask(t, tc.scenario)
			r := claudeNativeRunner(t, wm)
			got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			us := usageEvents(got)
			if len(us) != 1 {
				t.Fatalf("usage events = %d, want exactly 1 (events %+v)", len(us), got)
			}
			u := us[0]
			if u.CostUSD == nil || *u.CostUSD != 0.0339 || u.Input == nil || *u.Input != 2 || u.Output == nil || *u.Output != 494 ||
				u.CacheWrite == nil || *u.CacheWrite != 7099 || u.CachedInput == nil || *u.CachedInput != 100 {
				t.Fatalf("usage = %+v, want cost 0.0339, in 2, out 494, cache write 7099, cache read 100", u)
			}
			if last := got[len(got)-1]; tc.wantErr == ErrIsolationViolated && (last.Type != EventTypeDone || last.StopReason != "isolation_violated") {
				t.Fatalf("last event = %+v, want Done/isolation_violated after the usage", last)
			}
		})
	}
}

func TestRunPromptIsolated_FailureWithoutUsageSendsNone(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"result-error", "tool-attempt", "init-with-tools"} {
		wm, task := newIsolatedTask(t, scenario)
		r := claudeNativeRunner(t, wm)
		got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
		if err == nil {
			t.Fatalf("%s: want an error", scenario)
		}
		if us := usageEvents(got); len(us) != 0 {
			t.Fatalf("%s: usage events = %+v, want none (nothing is estimated)", scenario, us)
		}
	}
}

func TestRunPromptIsolated_ResultErrorFails(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "result-error")
	r := claudeNativeRunner(t, wm)
	_, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if !errors.Is(err, ErrIsolatedRunFailed) {
		t.Fatalf("error = %v, want ErrIsolatedRunFailed", err)
	}
	if errors.Is(err, ErrIsolationViolated) {
		t.Fatalf("error = %v must not be a violation", err)
	}
}

func TestRunPromptIsolated_PermissionDenialsViolate(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "result-permission-denials")
	r := claudeNativeRunner(t, wm)
	got, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"})
	if !errors.Is(err, ErrIsolationViolated) {
		t.Fatalf("error = %v, want ErrIsolationViolated", err)
	}
	if last := got[len(got)-1]; last.StopReason != "isolation_violated" {
		t.Fatalf("last event = %+v, want isolation_violated", last)
	}
}
