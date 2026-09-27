package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// runCaptured runs fn with both stdout and stderr captured, returning the
// exit code and the combined output.
func runCaptured(t *testing.T, args []string) (int, string) {
	t.Helper()
	code := -1
	out := captureStdout(t, func() { captureStderr(t, func() { code = run(args) }) })
	return code, out
}

// TestTaskChat_LsNewRenameArchive drives the four `task chat`
// subcommands end to end against the daemon: new creates, ls lists (and
// --all includes what plain ls hides), rename retitles, archive archives
// (and the daemon's running-run refusal is surfaced as a non-zero exit).
func TestTaskChat_LsNewRenameArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)
	taskArg := strconv.FormatInt(taskID, 10)

	// new: default chat plus the created one.
	code, out := runCaptured(t, []string{"task", "chat", "ls", taskArg})
	if code != 0 {
		t.Fatalf("run(task chat ls) = %d, want 0", code)
	}
	// tabwriter pads columns with spaces, so assert on a
	// whitespace-collapsed rendering of each row.
	collapsed := collapseSpaces(out)
	if want := "ID TITLE PROVIDER ARCHIVED"; !strings.Contains(collapsed, want) {
		t.Fatalf("task chat ls output = %q, want header %q", out, want)
	}
	if want := "Chat - no"; !strings.Contains(collapsed, want) {
		t.Fatalf("task chat ls output = %q, want the default chat row (unbound provider, unarchived)", out)
	}

	code, out = runCaptured(t, []string{"task", "chat", "new", taskArg, "Side", "chat"})
	if code != 0 {
		t.Fatalf("run(task chat new) = %d, want 0", code)
	}
	created := listChatsForTask(t, client, taskID)
	if len(created) != 2 {
		t.Fatalf("chats after new = %+v, want 2", created)
	}
	var second chatForTest
	for _, c := range created {
		if c.Title == "Side chat" {
			second = c
		}
	}
	if second.ID == 0 {
		t.Fatalf("chats after new = %+v, want one titled %q", created, "Side chat")
	}
	if !strings.Contains(out, "Side chat") {
		t.Fatalf("task chat new output = %q, want it to echo the created chat", out)
	}
	chatArg := strconv.FormatInt(second.ID, 10)

	// rename.
	code, _ = runCaptured(t, []string{"task", "chat", "rename", chatArg, "Renamed"})
	if code != 0 {
		t.Fatalf("run(task chat rename) = %d, want 0", code)
	}
	for _, c := range listChatsForTask(t, client, taskID) {
		if c.ID == second.ID && c.Title != "Renamed" {
			t.Fatalf("chat %d title = %q, want %q", c.ID, c.Title, "Renamed")
		}
	}

	// archive hides the chat from plain ls; --all keeps it, marked.
	code, _ = runCaptured(t, []string{"task", "chat", "archive", chatArg})
	if code != 0 {
		t.Fatalf("run(task chat archive) = %d, want 0", code)
	}
	code, out = runCaptured(t, []string{"task", "chat", "ls", taskArg})
	if code != 0 {
		t.Fatalf("run(task chat ls) = %d, want 0", code)
	}
	if strings.Contains(out, "Renamed") {
		t.Fatalf("task chat ls after archive = %q, want the archived chat hidden", out)
	}
	code, out = runCaptured(t, []string{"task", "chat", "ls", taskArg, "--all"})
	if code != 0 {
		t.Fatalf("run(task chat ls --all) = %d, want 0", code)
	}
	if !strings.Contains(collapseSpaces(out), "Renamed") {
		t.Fatalf("task chat ls --all = %q, want the archived chat listed", out)
	}
}

// TestTaskChat_UnknownSubcommandAndBadArgs pins the usage/exit-code
// contract of the chat subcommand tree.
func TestTaskChat_UnknownSubcommandAndBadArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no subcommand", []string{"task", "chat"}},
		{"unknown subcommand", []string{"task", "chat", "bogus"}},
		{"ls without taskId", []string{"task", "chat", "ls"}},
		{"new without taskId", []string{"task", "chat", "new"}},
		{"rename without title", []string{"task", "chat", "rename", "1"}},
		{"rename without args", []string{"task", "chat", "rename"}},
		{"archive without chatId", []string{"task", "chat", "archive"}},
		{"ls bad taskId", []string{"task", "chat", "ls", "not-a-number"}},
		{"new bad taskId", []string{"task", "chat", "new", "not-a-number"}},
		{"rename bad chatId", []string{"task", "chat", "rename", "not-a-number", "x"}},
		{"archive bad chatId", []string{"task", "chat", "archive", "not-a-number"}},
	}
	for _, tc := range cases {
		code, _ := runCaptured(t, tc.args)
		if code != 2 {
			t.Errorf("%s: run(%v) = %d, want 2", tc.name, tc.args, code)
		}
	}
}

// TestTaskChat_ErrorSurfacesDaemonRejection proves an RPC failure (unknown
// chat id) reaches stderr with the command prefix and a non-zero exit.
func TestTaskChat_ErrorSurfacesDaemonRejection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	code := -1
	errOut := captureStderr(t, func() {
		captureStdout(t, func() { code = run([]string{"task", "chat", "rename", "999999", "x"}) })
	})
	if code == 0 {
		t.Fatalf("run(task chat rename unknown id) = 0, want non-zero")
	}
	if want := "task chat rename:"; !strings.Contains(errOut, want) {
		t.Fatalf("stderr = %q, want it to carry the %q prefix", errOut, want)
	}
}

// collapseSpaces turns any run of whitespace (tabwriter's column padding
// included) into single spaces, so output assertions don't depend on
// column widths.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestTaskRuns_ListsAndFiltersByChat proves `task runs <taskId>` lists
// the task's runs (all chats) and `--chat <id>` narrows to that chat --
// run.list's P1.4 filter surfaced in the CLI.
func TestTaskRuns_ListsAndFiltersByChat(t *testing.T) {
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
	// One run on each chat.
	if code := runTaskSend(t, taskID, 0); code != 0 {
		t.Fatalf("send to default chat = %d, want 0", code)
	}
	if code := runTaskSend(t, taskID, second.ID); code != 0 {
		t.Fatalf("send to second chat = %d, want 0", code)
	}

	taskArg := strconv.FormatInt(taskID, 10)
	code, out := runCaptured(t, []string{"task", "runs", taskArg})
	if code != 0 {
		t.Fatalf("run(task runs) = %d, want 0", code)
	}
	rows := collapseSpaces(out)
	if want := "ID CHAT PROVIDER STATUS"; !strings.Contains(rows, want) {
		t.Fatalf("task runs output = %q, want header %q", out, want)
	}
	// Run ids are bare hex; count data rows by provider mentions instead
	// (the header and both rows carry "glm").
	if got := strings.Count(rows, "glm"); got != 2 {
		t.Fatalf("task runs = %q, want 2 run rows (got %d glm mentions)", out, got)
	}

	secondArg := "--chat=" + strconv.FormatInt(second.ID, 10)
	code, out = runCaptured(t, []string{"task", "runs", taskArg, secondArg})
	if code != 0 {
		t.Fatalf("run(task runs --chat) = %d, want 0", code)
	}
	rows = collapseSpaces(out)
	if got := strings.Count(rows, "glm"); got != 1 {
		t.Fatalf("task runs --chat = %q, want 1 run row (got %d glm mentions)", out, got)
	}
	if !strings.Contains(rows, strconv.FormatInt(second.ID, 10)) {
		t.Fatalf("task runs --chat = %q, want the second chat's id in its row", out)
	}
}

// TestTaskRuns_BadArgs pins the usage/exit-code contract.
func TestTaskRuns_BadArgs(t *testing.T) {
	for _, args := range [][]string{
		{"task", "runs"},
		{"task", "runs", "not-a-number"},
		{"task", "runs", "1", "--chat", "not-a-number"},
	} {
		if code, _ := runCaptured(t, args); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}
