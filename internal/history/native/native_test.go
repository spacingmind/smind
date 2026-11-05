package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	claudeSession = "11111111-2222-4333-8444-555555555555"
	codexThread   = "01a1aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"
	targetPrompt  = "Fix the failing test"
)

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

type env struct {
	root string // CLAUDE_CONFIG_DIR / CODEX_HOME
	ref  Ref
	rb   *Rebuilder
	file string // the session file
}

func rebuilder(root string, cfg Config) *Rebuilder {
	cfg.Roots = func(provider, harness string) []string { return []string{root} }
	return New(cfg)
}

func newClaude(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "projects", claudeProjectDir("/work/repo"), claudeSession+".jsonl")
	copyFile(t, "testdata/claude/session.jsonl", file)
	ref := Ref{Provider: "claude-native", Harness: HarnessClaudeCode, SessionID: claudeSession, ConfigDir: root, Cwd: "/work/repo", TranscriptPath: file,
		PromptSha256: sha(targetPrompt), TurnWindow: TurnWindow{StartedAt: at("2026-11-05T09:10:00Z"), FinishedAt: at("2026-11-05T09:10:10Z")}}
	return &env{root: root, ref: ref, rb: rebuilder(root, Config{}), file: file}
}

func newCodex(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	name := "rollout-2026-11-05T09-00-00-" + codexThread + ".jsonl"
	file := filepath.Join(root, "sessions", "2026", "11", "05", name)
	copyFile(t, filepath.Join("testdata/codex", name), file)
	ref := Ref{Provider: "codex-native", Harness: HarnessCodex, SessionID: codexThread, CodexHome: root, Cwd: "/work/repo", TranscriptPath: file,
		PromptSha256: sha(targetPrompt), TurnWindow: TurnWindow{StartedAt: at("2026-11-05T09:10:00Z"), FinishedAt: at("2026-11-05T09:10:10Z")}}
	return &env{root: root, ref: ref, rb: rebuilder(root, Config{}), file: file}
}

func (e *env) rebuild() Result {
	return e.rb.Rebuild(context.Background(), Request{Ref: e.ref, Requester: RequesterHuman, ExpectedToolCalls: -1})
}

// liveClaudeEvents is what the live path produced for the fixture's target turn:
// the SDK messages the CLI streamed, run through taskrunner's own mapping, then
// the delivery elider.
func liveClaudeEvents() []taskrunner.Event {
	heredoc := "cat > notes.txt <<'EOF'\nsecret-token-123\nEOF\necho written"
	msgs := []claudecode.Message{
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ThinkingBlock{Thinking: "need the failing test first"}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "I will run the tests. "}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B1", Name: "Bash", Input: map[string]any{"command": heredoc, "description": "write notes"}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B1", Content: json.RawMessage(`"written"`)}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B2", Name: "Read", Input: map[string]any{"file_path": "/work/repo/main.go"}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B2", Content: json.RawMessage(`[{"type":"text","text":"package main"}]`)}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B3", Name: "Bash", Input: map[string]any{"command": "go test ./..."}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B3", Content: json.RawMessage(`"FAIL: boom"`), IsError: true}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "Done: fixed."}}},
	}
	var out []taskrunner.Event
	for _, m := range msgs {
		out = append(out, taskrunner.ClaudeMessageEvents(m)...)
	}
	return out
}

// view is the client-visible content of an event: what a consumer compares.
func view(e taskrunner.Event) string {
	norm := func(r json.RawMessage) string {
		var v any
		if json.Unmarshal(r, &v) != nil {
			return string(r)
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	return fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s", e.Type, e.Text, e.ToolCallID, e.ToolName, e.ToolStatus, norm(e.ToolInput), norm(e.ToolResult))
}

// TestClaudeRebuildEquivalentToLive: the rebuilt events of the recorded session
// are the events the live path emitted for the same messages, after the same
// elider.
func TestClaudeRebuildEquivalentToLive(t *testing.T) {
	e := newClaude(t)
	res := e.rebuild()
	if !res.OK() {
		t.Fatalf("rebuild refused: %q (%s)", res.Reason, res.Detail)
	}
	el := taskrunner.NewToolCallElider()
	var want []string
	for _, ev := range liveClaudeEvents() {
		if ev.Type == taskrunner.EventTypeToolCall {
			ev.ToolInput, ev.ToolTitle, ev.ToolResult, _ = el.Elide(ev.ToolCallID, ev.ToolInput, ev.ToolTitle, ev.ToolResult)
		}
		want = append(want, view(ev))
	}
	var got []string
	for _, ev := range res.Events {
		got = append(got, view(ev))
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rebuilt events differ from live\n got %q\nwant %q", got, want)
	}
	// the sidechain record, the other turns and every unknown record type are absent
	for _, ev := range res.Events {
		if strings.Contains(view(ev), "sidechain") || strings.Contains(view(ev), "first continue") || strings.Contains(view(ev), "README") {
			t.Fatalf("an event from outside the turn: %s", view(ev))
		}
	}
}

// TestCodexRebuildEquivalentToLive: a Codex run's live events are the agent's
// message text (no tool cards), so the rebuilt text must equal it.
func TestCodexRebuildEquivalentToLive(t *testing.T) {
	e := newCodex(t)
	res := e.rebuild()
	if !res.OK() {
		t.Fatalf("rebuild refused: %q (%s)", res.Reason, res.Detail)
	}
	var text string
	for _, ev := range res.Events {
		if ev.Type != taskrunner.EventTypeText {
			t.Fatalf("a codex rebuild returned a %d event; the live path has text only", ev.Type)
		}
		text += ev.Text
	}
	if text != "I will run the tests. Done: fixed." {
		t.Fatalf("text = %q", text)
	}
}

func TestTurnMatching(t *testing.T) {
	for name, newEnv := range map[string]func(*testing.T) *env{"claude": newClaude, "codex": newCodex} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			check := func(label string, want Reason, mut func(*Ref)) {
				t.Helper()
				ref := e.ref
				mut(&ref)
				res := e.rb.Rebuild(context.Background(), Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: -1})
				if res.Reason != want {
					t.Errorf("%s: reason %q (%s), want %q", label, res.Reason, res.Detail, want)
				}
			}
			check("exact", ReasonNone, func(*Ref) {})
			check("wrong prompt hash", ReasonSliceNotFound, func(r *Ref) { r.PromptSha256 = sha("another prompt") })
			check("empty prompt hash", ReasonCannotRebuild, func(r *Ref) { r.PromptSha256 = "" })
			check("window an hour later", ReasonSliceNotFound, func(r *Ref) {
				r.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T10:10:00Z"), FinishedAt: at("2026-11-05T10:10:10Z")}
			})
			check("window ends 2 min before the turn: within tolerance", ReasonNone, func(r *Ref) {
				r.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T09:05:00Z"), FinishedAt: at("2026-11-05T09:08:00Z")}
			})
			check("window ends 2 min 1 s before the turn", ReasonSliceNotFound, func(r *Ref) {
				r.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T09:05:00Z"), FinishedAt: at("2026-11-05T09:07:59Z")}
			})
			check("zero window", ReasonSliceNotFound, func(r *Ref) { r.TurnWindow = TurnWindow{} })
			// a repeated prompt inside one window is ambiguous: smind does not guess
			check("two matching turns", ReasonSliceNotFound, func(r *Ref) {
				r.PromptSha256 = sha("continue")
				r.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T09:19:00Z"), FinishedAt: at("2026-11-05T09:26:00Z")}
			})
			// the same repeated prompt is told apart by the window
			check("repeated prompt, window of the first", ReasonNone, func(r *Ref) {
				r.PromptSha256 = sha("continue")
				r.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T09:20:00Z"), FinishedAt: at("2026-11-05T09:20:05Z")}
			})
			check("repeated prompt, window of the second", ReasonNone, func(r *Ref) {
				r.PromptSha256 = sha("continue")
				r.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T09:25:00Z"), FinishedAt: at("2026-11-05T09:25:05Z")}
			})
		})
	}
	// the window picks the right "continue"
	for _, tc := range []struct{ start, finish, want string }{{"09:20:00", "09:20:05", "first continue"}, {"09:25:00", "09:25:05", "second continue"}} {
		for name, newEnv := range map[string]func(*testing.T) *env{"claude": newClaude, "codex": newCodex} {
			e := newEnv(t)
			e.ref.PromptSha256 = sha("continue")
			e.ref.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T" + tc.start + "Z"), FinishedAt: at("2026-11-05T" + tc.finish + "Z")}
			res := e.rebuild()
			if !res.OK() || len(res.Events) == 0 || res.Events[len(res.Events)-1].Text != tc.want {
				t.Errorf("%s window %s: %q (%s), events %v, want %q", name, tc.start, res.Reason, res.Detail, res.Events, tc.want)
			}
		}
	}
}

func TestOnlyAHumanRebuilds(t *testing.T) {
	e := newClaude(t)
	for name, who := range map[string]Requester{"agent": RequesterAgent, "zero value": 0} {
		res := e.rb.Rebuild(context.Background(), Request{Ref: e.ref, Requester: who, ExpectedToolCalls: -1})
		if res.Reason != ReasonHumanOnly || len(res.Events) != 0 {
			t.Errorf("%s: %q with %d events, want rebuild_not_requested and nothing", name, res.Reason, len(res.Events))
		}
	}
}

func TestUnsupportedHarnessAndMissingRef(t *testing.T) {
	e := newClaude(t)
	acp := e.ref
	acp.Harness = "acp"
	if res := e.rb.Rebuild(context.Background(), Request{Ref: acp, Requester: RequesterHuman}); res.Reason != ReasonCannotRebuild {
		t.Fatalf("acp: %q", res.Reason)
	}
	if res := e.rb.Rebuild(context.Background(), Request{Requester: RequesterHuman}); res.Reason != ReasonCannotRebuild {
		t.Fatalf("empty ref: %q", res.Reason)
	}
}

// Path safety: every refusal is native_session_missing, and nothing outside the
// allowed root is ever opened.
func TestPathSafety(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "other.jsonl")
	copyFile(t, "testdata/claude/session.jsonl", secret) // a perfectly valid transcript, but not under the root

	cases := map[string]func(t *testing.T, e *env){
		"session file is a symlink to a file outside the root": func(t *testing.T, e *env) {
			os.Remove(e.file)
			if err := os.Symlink(secret, e.file); err != nil {
				t.Skip("symlinks unavailable")
			}
		},
		"project directory is a symlink outside the root": func(t *testing.T, e *env) {
			dir := filepath.Dir(e.file)
			os.RemoveAll(dir)
			real := filepath.Join(outside, "proj")
			copyFile(t, "testdata/claude/session.jsonl", filepath.Join(real, claudeSession+".jsonl"))
			if err := os.Symlink(real, dir); err != nil {
				t.Skip("symlinks unavailable")
			}
		},
		"projects directory is a symlink outside the root": func(t *testing.T, e *env) {
			p := filepath.Join(e.root, "projects")
			os.RemoveAll(p)
			real := filepath.Join(outside, "projects")
			copyFile(t, "testdata/claude/session.jsonl", filepath.Join(real, claudeProjectDir("/work/repo"), claudeSession+".jsonl"))
			if err := os.Symlink(real, p); err != nil {
				t.Skip("symlinks unavailable")
			}
		},
		"session id with a traversal": func(t *testing.T, e *env) { e.ref.SessionID = "../../" + filepath.Base(outside) + "/other" },
		// a real transcript one directory up, still inside the root: containment alone would accept it
		"session id climbing to a transcript inside the root": func(t *testing.T, e *env) {
			copyFile(t, "testdata/claude/session.jsonl", filepath.Join(e.root, "projects", "other.jsonl"))
			e.ref.SessionID, e.ref.TranscriptPath = "../other", ""
		},
		"session id with a separator": func(t *testing.T, e *env) { e.ref.SessionID = "a/b" },
		"session id of dots":          func(t *testing.T, e *env) { e.ref.SessionID = ".." },
		"config dir outside the allowed roots, with a real transcript there": func(t *testing.T, e *env) {
			copyFile(t, "testdata/claude/session.jsonl", filepath.Join(outside, "projects", claudeProjectDir("/work/repo"), claudeSession+".jsonl"))
			e.ref.ConfigDir = outside
			e.ref.TranscriptPath = ""
		},
		"config dir relative": func(t *testing.T, e *env) { e.ref.ConfigDir, e.ref.TranscriptPath = "relative/dir", "" },
		"config dir traversal": func(t *testing.T, e *env) {
			e.ref.ConfigDir = e.root + "/../elsewhere"
			e.ref.TranscriptPath = ""
		},
		"recorded transcript path is another file": func(t *testing.T, e *env) { e.ref.TranscriptPath = secret },
		"session path is a directory": func(t *testing.T, e *env) {
			os.Remove(e.file)
			os.MkdirAll(e.file, 0o755)
		},
		"file over the size bound": func(t *testing.T, e *env) {
			f, err := os.OpenFile(e.file, os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := f.Truncate(MaxFileBytes + 1); err != nil {
				t.Skip("sparse file unavailable")
			}
		},
		"file gone": func(t *testing.T, e *env) { os.Remove(e.file) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newClaude(t)
			setup(t, e)
			res := e.rebuild()
			if res.Reason != ReasonSessionMissing || len(res.Events) != 0 {
				t.Fatalf("reason %q (%s) with %d events, want native_session_missing and nothing", res.Reason, res.Detail, len(res.Events))
			}
		})
	}

	// A root that is itself reached through a symlink is fine: both sides resolve.
	t.Run("allowed root behind a symlink", func(t *testing.T) {
		e := newClaude(t)
		link := filepath.Join(t.TempDir(), "cfg")
		if err := os.Symlink(e.root, link); err != nil {
			t.Skip("symlinks unavailable")
		}
		ref := e.ref
		ref.ConfigDir, ref.TranscriptPath = link, ""
		res := e.rb.Rebuild(context.Background(), Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: -1})
		if !res.OK() {
			t.Fatalf("a symlinked root was refused: %q", res.Reason)
		}
	})
}

func TestCodexPathSafety(t *testing.T) {
	outside := t.TempDir()
	e := newCodex(t)
	if res := e.rebuild(); !res.OK() {
		t.Fatalf("baseline: %q", res.Reason)
	}
	// archived rollout is found
	arch := filepath.Join(e.root, "archived_sessions", filepath.Base(e.file))
	if err := os.MkdirAll(filepath.Dir(arch), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(e.file, arch); err != nil {
		t.Fatal(err)
	}
	if res := e.rebuild(); !res.OK() {
		t.Fatalf("archived rollout not found: %q", res.Reason)
	}
	// symlink escape
	os.Remove(arch)
	target := filepath.Join(outside, filepath.Base(e.file))
	copyFile(t, "testdata/codex/"+filepath.Base(e.file), target)
	if err := os.Symlink(target, arch); err != nil {
		t.Skip("symlinks unavailable")
	}
	if res := e.rebuild(); res.Reason != ReasonSessionMissing {
		t.Fatalf("symlink escape: %q", res.Reason)
	}
	for _, id := range []string{"*", "../x", "a/b", "thread-[12]", ""} {
		ref := e.ref
		ref.SessionID = id
		if res := e.rb.Rebuild(context.Background(), Request{Ref: ref, Requester: RequesterHuman}); res.OK() {
			t.Errorf("thread id %q was accepted", id)
		}
	}
}

func TestBounds(t *testing.T) {
	t.Run("concurrency", func(t *testing.T) {
		e := newClaude(t)
		e.rb = rebuilder(e.root, Config{MaxConcurrent: 2, Timeout: 5 * time.Second})
		release := make(chan struct{})
		var entered sync.WaitGroup
		entered.Add(2)
		hook := func() { entered.Done(); <-release }
		e.rb.beforeParse.Store(&hook)
		var wg sync.WaitGroup
		results := make([]Result, 2)
		for i := range results {
			wg.Add(1)
			go func() { defer wg.Done(); results[i] = e.rebuild() }()
		}
		entered.Wait()
		if res := e.rebuild(); res.Reason != ReasonBusy {
			t.Fatalf("a third concurrent rebuild: %q, want rebuild_busy", res.Reason)
		}
		close(release)
		wg.Wait()
		for _, r := range results {
			if !r.OK() {
				t.Fatalf("a held rebuild failed: %q", r.Reason)
			}
		}
		e.rb.beforeParse.Store(nil)
		if res := e.rebuild(); !res.OK() {
			t.Fatalf("after release: %q", res.Reason)
		}
	})
	t.Run("timeout holds the slot until the parse ends", func(t *testing.T) {
		e := newClaude(t)
		e.rb = rebuilder(e.root, Config{MaxConcurrent: 1, Timeout: 50 * time.Millisecond})
		stop := make(chan struct{})
		hook := func() { <-stop }
		e.rb.beforeParse.Store(&hook)
		start := time.Now()
		res := e.rebuild()
		if res.Reason != ReasonFailed || time.Since(start) > 2*time.Second {
			t.Fatalf("timeout: %q after %v", res.Reason, time.Since(start))
		}
		if res := e.rebuild(); res.Reason != ReasonBusy {
			t.Fatalf("the timed-out parse must still hold its slot: %q", res.Reason)
		}
		close(stop)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			e.rb.beforeParse.Store(nil)
			if res := e.rebuild(); res.OK() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("the slot was never released")
	})
	t.Run("output over the cap", func(t *testing.T) {
		e := newClaude(t)
		e.rb = rebuilder(e.root, Config{MaxOutput: 20})
		if res := e.rebuild(); res.Reason != ReasonFailed || len(res.Events) != 0 {
			t.Fatalf("over the output bound: %q with %d events", res.Reason, len(res.Events))
		}
	})
	t.Run("codex output over the cap", func(t *testing.T) {
		e := newCodex(t)
		e.rb = rebuilder(e.root, Config{MaxOutput: 5})
		if res := e.rebuild(); res.Reason != ReasonFailed || len(res.Events) != 0 {
			t.Fatalf("over the output bound: %q with %d events", res.Reason, len(res.Events))
		}
	})
	t.Run("a record over the line cap", func(t *testing.T) {
		e := newClaude(t)
		f, _ := os.OpenFile(e.file, os.O_APPEND|os.O_WRONLY, 0o600)
		f.WriteString(`{"type":"attachment","x":"` + strings.Repeat("a", maxLineBytes+10) + `"}` + "\n")
		f.Close()
		if res := e.rebuild(); res.Reason != ReasonFailed {
			t.Fatalf("a %d-byte record: %q", maxLineBytes, res.Reason)
		}
	})
}

func TestFormatDrift(t *testing.T) {
	appendLines := func(e *env, lines ...string) {
		f, _ := os.OpenFile(e.file, os.O_APPEND|os.O_WRONLY, 0o600)
		defer f.Close()
		for _, l := range lines {
			f.WriteString(l + "\n")
		}
	}
	t.Run("unknown record and block types are skipped, not drift", func(t *testing.T) {
		e := newClaude(t)
		appendLines(e, `{"type":"brand-new-record","timestamp":"2026-11-05T09:10:05.000Z"}`)
		if res := e.rebuild(); !res.OK() {
			t.Fatalf("tolerant reader refused: %q (%s)", res.Reason, res.Detail)
		}
	})
	t.Run("tool results that do not pair", func(t *testing.T) {
		e := newClaude(t)
		b, _ := os.ReadFile(e.file)
		out := strings.ReplaceAll(string(b), `"tool_use_id":"toolu_B`, `"tool_use_id":"renamed_B`)
		os.WriteFile(e.file, []byte(out), 0o600)
		res := e.rebuild()
		if res.Reason != ReasonFormatDrift || len(res.Events) != 0 {
			t.Fatalf("unpaired results: %q (%s) with %d events, want format_drift and nothing", res.Reason, res.Detail, len(res.Events))
		}
	})
	t.Run("tool count far from the stored run", func(t *testing.T) {
		e := newClaude(t)
		res := e.rb.Rebuild(context.Background(), Request{Ref: e.ref, Requester: RequesterHuman, ExpectedToolCalls: 3})
		if !res.OK() {
			t.Fatalf("3 calls expected, 3 in the session: %q", res.Reason)
		}
		res = e.rb.Rebuild(context.Background(), Request{Ref: e.ref, Requester: RequesterHuman, ExpectedToolCalls: 9})
		if res.Reason != ReasonFormatDrift {
			t.Fatalf("9 calls expected, 3 in the session: %q", res.Reason)
		}
	})
	t.Run("records that do not decode", func(t *testing.T) {
		e := newClaude(t)
		var junk []string
		for i := 0; i < 40; i++ {
			junk = append(junk, "{not json")
		}
		appendLines(e, junk...)
		if res := e.rebuild(); res.Reason != ReasonFormatDrift {
			t.Fatalf("garbage: %q", res.Reason)
		}
	})
	t.Run("breaker opens after three failures and closes after an hour", func(t *testing.T) {
		e := newClaude(t)
		now := time.Now()
		e.rb = rebuilder(e.root, Config{Now: func() time.Time { return now }})
		bad := Request{Ref: e.ref, Requester: RequesterHuman, ExpectedToolCalls: 9}
		for i := 0; i < 3; i++ {
			if res := e.rb.Rebuild(context.Background(), bad); res.Reason != ReasonFormatDrift {
				t.Fatalf("failure %d: %q", i, res.Reason)
			}
		}
		// now even a good request is refused without reading anything
		os.Remove(e.file)
		if res := e.rebuild(); res.Reason != ReasonFormatDrift {
			t.Fatalf("open breaker: %q, want format_drift (not native_session_missing: the file must not even be looked at)", res.Reason)
		}
		now = now.Add(61 * time.Minute)
		copyFile(t, "testdata/claude/session.jsonl", e.file)
		if res := e.rebuild(); !res.OK() {
			t.Fatalf("after an hour: %q", res.Reason)
		}
	})
	t.Run("a success resets the count", func(t *testing.T) {
		e := newClaude(t)
		bad := Request{Ref: e.ref, Requester: RequesterHuman, ExpectedToolCalls: 9}
		for round := 0; round < 3; round++ {
			e.rb.Rebuild(context.Background(), bad)
			e.rb.Rebuild(context.Background(), bad)
			if res := e.rebuild(); !res.OK() {
				t.Fatalf("round %d: two failures then a success must not open the breaker: %q", round, res.Reason)
			}
		}
	})
}

// Redaction: nothing a live read hides is shown. The heredoc body is elided like
// a live tool call; provider secrets are scrubbed everywhere; the raw body stays
// reachable only through HeredocBody.
func TestElisionAndScrub(t *testing.T) {
	e := newClaude(t)
	res := e.rebuild()
	if !res.OK() {
		t.Fatal(res.Reason)
	}
	for _, ev := range res.Events {
		if v := view(ev); strings.Contains(v, "secret-token-123") {
			t.Fatalf("a heredoc body reached a rebuilt event: %s", v)
		}
	}
	body, ok := res.HeredocBody("toolu_B1", 0)
	if !ok || !strings.Contains(body, "secret-token-123") {
		t.Fatalf("HeredocBody = %q, %v", body, ok)
	}
	if _, ok := res.HeredocBody("toolu_B1", 1); ok {
		t.Fatal("a body index past the last was returned")
	}

	// a provider secret the session echoes in text, a tool input and a result
	b, _ := os.ReadFile(e.file)
	leaky := strings.NewReplacer(
		"I will run the tests. ", "I will run the tests with key sk-LIVE-1234. ",
		`"command":"go test ./..."`, `"command":"go test ./... --token sk-LIVE-1234"`,
		`"content":"FAIL: boom"`, `"content":"FAIL: boom sk-LIVE-1234"`,
	).Replace(string(b))
	os.WriteFile(e.file, []byte(leaky), 0o600)

	plain := e.rebuild()
	if !strings.Contains(fmt.Sprint(plain.Events), "sk-LIVE-1234") {
		t.Fatal("setup: the secret is not in the unscrubbed rebuild")
	}
	e.rb = rebuilder(e.root, Config{Scrub: func(s string) string { return strings.ReplaceAll(s, "sk-LIVE-1234", "[redacted]") }})
	scrubbed := e.rebuild()
	if !scrubbed.OK() {
		t.Fatal(scrubbed.Reason)
	}
	for _, ev := range scrubbed.Events {
		if strings.Contains(string(ev.ToolInput)+string(ev.ToolResult)+ev.Text+ev.ToolTitle, "sk-LIVE-1234") {
			t.Fatalf("a provider secret survived the scrub: %s", view(ev))
		}
	}
	if !strings.Contains(fmt.Sprint(scrubbed.Events), "[redacted]") {
		t.Fatal("the scrub did not run")
	}
}
