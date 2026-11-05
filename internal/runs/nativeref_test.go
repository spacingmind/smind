package runs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestClaudeProjectDirMatchesRealLayout pins the directory naming against a
// transcript directory that exists on a real machine (checked 2026-11-05):
// every character that is not an ASCII letter or digit becomes "-", so the
// "/." of ".smind" is "--".
func TestClaudeProjectDirMatchesRealLayout(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/lounguyen/.smind/worktrees/4/lead-skeleton-storage-impl-dmn4gqgg7zd4": "-Users-lounguyen--smind-worktrees-4-lead-skeleton-storage-impl-dmn4gqgg7zd4",
		"/private/tmp/smind-dogfood/home/.smind/worktrees/1/manual-dmczzesp31w0":      "-private-tmp-smind-dogfood-home--smind-worktrees-1-manual-dmczzesp31w0",
		"/tmp/a b/c_d":     "-tmp-a-b-c-d",
		`C:\Users\me\repo`: "C--Users-me-repo",
	} {
		if got := claudeProjectDir(in); got != want {
			t.Errorf("claudeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaudeTranscriptAndCodexRollout(t *testing.T) {
	cfg := t.TempDir()
	if got, want := claudeTranscript(cfg, "/a/.b", "sid"), filepath.Join(cfg, "projects", "-a--b", "sid.jsonl"); got != want {
		t.Fatalf("claudeTranscript = %q, want %q", got, want)
	}

	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "11", "05")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(day, "rollout-2026-11-05T01-09-53-thread-1.jsonl")
	os.WriteFile(live, []byte("{}\n"), 0o600)
	if got := codexRollout(home, "thread-1"); got != live {
		t.Fatalf("live rollout = %q, want %q", got, live)
	}
	arch := filepath.Join(home, "archived_sessions")
	os.MkdirAll(arch, 0o755)
	archived := filepath.Join(arch, "rollout-2026-11-04T01-00-00-thread-2.jsonl")
	os.WriteFile(archived, []byte("{}\n"), 0o600)
	if got := codexRollout(home, "thread-2"); got != archived {
		t.Fatalf("archived rollout = %q, want %q", got, archived)
	}
	for _, bad := range []string{"", "*", "../x", "a/b", "thread-[12]", `a\b`} {
		if got := codexRollout(home, bad); got != "" {
			t.Errorf("codexRollout(%q) = %q, want none (no glob or path characters)", bad, got)
		}
	}
	if got := codexRollout(home, "unknown"); got != "" {
		t.Fatalf("unknown thread = %q", got)
	}
}

func TestTranscriptVersion(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "c.jsonl")
	os.WriteFile(claude, []byte("{\"type\":\"queue-operation\"}\n{\"type\":\"user\",\"version\":\"2.1.310\"}\n"), 0o600)
	codex := filepath.Join(dir, "x.jsonl")
	os.WriteFile(codex, []byte("{\"type\":\"session_meta\",\"payload\":{\"cli_version\":\"0.166.0\"}}\n"), 0o600)
	if got := transcriptVersion(claude); got != "2.1.310" {
		t.Errorf("claude version = %q", got)
	}
	if got := transcriptVersion(codex); got != "0.166.0" {
		t.Errorf("codex version = %q", got)
	}
	if got := transcriptVersion(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing file version = %q", got)
	}
}

// TestFinishCapturesNativeRef: a real run on the fake agent ends with a
// native_ref that names its provider session and turn, and holds no content.
func TestFinishCapturesNativeRef(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	sessions := taskrunner.NewMemorySessionStore()
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}), taskrunner.WithSessionStore(sessions))
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "my prompt", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 10*time.Second)
	sk, err := st.GetRunSkeleton(runID)
	if err != nil || sk.NativeRef == "" {
		t.Fatalf("no native_ref after finish: %+v, %v", sk, err)
	}
	var ref nativeRef
	if err := json.Unmarshal([]byte(sk.NativeRef), &ref); err != nil {
		t.Fatal(err)
	}
	if ref.Provider != string(taskrunner.ProviderGLM) || ref.SessionID == "" || ref.Harness != "acp" || ref.Cwd != *task.WorktreePath {
		t.Fatalf("native_ref = %+v", ref)
	}
	if ref.PromptSha256 != sha256Hex("my prompt") || ref.TurnWindow.StartedAt.IsZero() || !ref.TurnWindow.FinishedAt.After(ref.TurnWindow.StartedAt) {
		t.Fatalf("native_ref turn = %+v", ref)
	}
	if ref.TranscriptPath != "" || ref.ConfigDir != "" || ref.CodexHome != "" {
		t.Fatalf("an ACP run has no file to point at: %+v", ref)
	}
}

// TestNativeRefClaudeFindsTranscript: with a claude handle and a transcript on
// disk, native_ref carries the path, the config dir used and the version.
func TestNativeRefClaudeFindsTranscript(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	e := newHistEnv(t)
	cwd := *e.task.WorktreePath
	tr := claudeTranscript(cfg, cwd, "sess-1")
	os.MkdirAll(filepath.Dir(tr), 0o755)
	os.WriteFile(tr, []byte("{\"type\":\"user\",\"version\":\"2.9.9\"}\n"), 0o600)

	sessions := taskrunner.NewMemorySessionStore()
	sessions.Set(7, taskrunner.SessionHandle{Provider: taskrunner.ProviderClaudeNative, SessionID: "sess-1"})
	runner := taskrunner.New(nil, taskrunner.WithSessionStore(sessions))
	reg := newTestRegistry(t, e.st)
	started := time.Now().Add(-time.Minute).UTC()
	if _, err := e.st.CreateRun(store.Run{ID: "r", TaskID: e.task.ID, Provider: "claude-native", Prompt: "p", Status: "done", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	r := &run{id: "r", taskID: e.task.ID, chatID: 7, provider: taskrunner.ProviderClaudeNative, prompt: "p", startedAt: started, runner: runner}
	reg.captureNativeRef(r, time.Now())
	sk, _ := e.st.GetRunSkeleton("r")
	var ref nativeRef
	if err := json.Unmarshal([]byte(sk.NativeRef), &ref); err != nil {
		t.Fatalf("native_ref %q: %v", sk.NativeRef, err)
	}
	if ref.TranscriptPath != tr || ref.ConfigDir != cfg || ref.ProviderVersion != "2.9.9" || ref.SessionID != "sess-1" {
		t.Fatalf("native_ref = %+v, want transcript %s", ref, tr)
	}

	// a handle of another provider is never reported
	sessions.Set(8, taskrunner.SessionHandle{Provider: taskrunner.ProviderGLM, SessionID: "other"})
	r2 := &run{id: "r2", taskID: e.task.ID, chatID: 8, provider: taskrunner.ProviderClaudeNative, runner: runner}
	if _, ok := runner.NativeSession(r2.chatID, r2.provider); ok {
		t.Fatal("a handle bound to another provider was reported")
	}
}
