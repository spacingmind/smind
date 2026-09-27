package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/wsclient"
)

// Tests for the ADR-0016 P4 CLI surface: `task send --chat` and the
// `task chat` subcommands. They run against the same fake-agent-backed
// daemon the config-option tests use (newConfigOptionTestEnv).

// chatForTest decodes a chat.* RPC result off the wire -- store.Chat has
// no json tags, so its Go field names are the wire keys (see
// internal/wsapi/chat_test.go's runStatusForTest for the same convention).
type chatForTest struct {
	ID         int64
	TaskID     int64
	Title      string
	Provider   *string
	ArchivedAt *time.Time
}

// dialTestClient opens a wsclient to the test daemon, the same way
// startTestRun does.
func dialTestClient(t *testing.T, srvURL string) *wsclient.Client {
	t.Helper()
	token, err := auth.LoadOrCreateToken(os.Getenv("SMIND_HOME"))
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client, err := wsclient.Dial(context.Background(), "127.0.0.1:"+u.Port(), token)
	if err != nil {
		t.Fatalf("wsclient.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newTestRepoTask creates a git repo with one commit, registers it as a
// workspace, and creates one task in it via the daemon's own RPCs -- the
// same setup startTestRun performs, minus the run, so chat tests get a
// taskID to operate on.
func newTestRepoTask(t *testing.T, client *wsclient.Client) int64 {
	t.Helper()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"git", "-C", repoDir, "init"},
		{"git", "-C", repoDir, "config", "user.email", "test@example.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test"},
		{"git", "-C", repoDir, "add", "README.md"},
		{"git", "-C", repoDir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", args, err, out)
		}
	}

	var ws struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "workspace.create", map[string]any{"path": repoDir, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	var task struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "task.create", map[string]any{"workspaceId": ws.ID, "title": "T"}, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	return task.ID
}

// listChatsForTest fetches taskId's chats (archived included) over the
// wire, so tests can locate a task's default chat or assert on CLI-driven
// mutations without re-deriving them.
func listChatsForTask(t *testing.T, client *wsclient.Client, taskID int64) []chatForTest {
	t.Helper()
	var chats []chatForTest
	if err := client.Call(context.Background(), "chat.list", map[string]any{"taskId": taskID, "includeArchived": true}, &chats); err != nil {
		t.Fatalf("chat.list: %v", err)
	}
	return chats
}

// runTaskSend runs `smind task send <taskId> glm hi` (with --chat when
// chatID is nonzero) with stdout and stderr captured, returning the exit
// code. The fake agent's default (no scenario file) script finishes on
// its own, so the send streams to completion and returns.
func runTaskSend(t *testing.T, taskID, chatID int64) int {
	t.Helper()
	args := []string{"task", "send", strconv.FormatInt(taskID, 10), "glm", "hi"}
	if chatID != 0 {
		args = append(args, "--chat", strconv.FormatInt(chatID, 10))
	}
	code := -1
	captureStdout(t, func() { captureStderr(t, func() { code = run(args) }) })
	return code
}

// countRunsInChat returns how many runs the registry lists for chatID.
func countRunsInChat(t *testing.T, client *wsclient.Client, chatID int64) int {
	t.Helper()
	var runs []map[string]any
	if err := client.Call(context.Background(), "run.list", map[string]any{"chatId": chatID}, &runs); err != nil {
		t.Fatalf("run.list(chatId=%d): %v", chatID, err)
	}
	return len(runs)
}

// TestTaskSend_ChatFlagRoutesRunToThatChat proves `task send --chat` lands
// the run on the named chat (not the default one) and that omitting the
// flag keeps landing runs on the default chat -- the CLI side of the
// P1.6 compatibility path.
func TestTaskSend_ChatFlagRoutesRunToThatChat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)

	var second chatForTest
	if err := client.Call(context.Background(), "chat.create", map[string]any{"taskId": taskID, "title": "Second"}, &second); err != nil {
		t.Fatalf("chat.create: %v", err)
	}
	defaultChat := listChatsForTask(t, client, taskID)[0]
	if defaultChat.ID == second.ID {
		t.Fatal("chat.list[0] is the second chat; expected the default chat first")
	}

	if code := runTaskSend(t, taskID, second.ID); code != 0 {
		t.Fatalf("run(task send --chat) = %d, want 0", code)
	}
	if got := countRunsInChat(t, client, second.ID); got != 1 {
		t.Fatalf("run.list(chatId=%d) = %d runs, want 1 (the --chat send)", second.ID, got)
	}
	if got := countRunsInChat(t, client, defaultChat.ID); got != 0 {
		t.Fatalf("run.list(chatId=%d) = %d runs, want 0 (default chat untouched)", defaultChat.ID, got)
	}
}

// TestTaskSend_OmittedChatLandsOnDefaultChat pins the no-flag half of the
// compatibility path: a send with no --chat lands its run on the task's
// default chat.
func TestTaskSend_OmittedChatLandsOnDefaultChat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)
	defaultChat := listChatsForTask(t, client, taskID)[0]

	if code := runTaskSend(t, taskID, 0); code != 0 {
		t.Fatalf("run(task send) = %d, want 0", code)
	}
	if got := countRunsInChat(t, client, defaultChat.ID); got != 1 {
		t.Fatalf("run.list(chatId=%d) = %d runs, want 1 (the default-chat send)", defaultChat.ID, got)
	}
}
