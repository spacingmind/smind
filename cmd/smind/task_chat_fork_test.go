package main

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

// Tests for `task chat fork` (ADR-0057), against the same fake-agent-backed
// daemon the task_chat_test.go helpers set up. The fake GLM agent is an ACP
// harness, so a native fork of its chat is always refused with
// fork_unavailable -- which is exactly the path the refusal test needs.

// chatWithForkForTest is a chat.list row including the additive `fork`
// object a forked chat carries (see internal/wsapi's chatView).
type chatWithForkForTest struct {
	ID    int64
	Title string
	Fork  *struct {
		ParentChatID int64  `json:"parentChatId"`
		Mode         string `json:"mode"`
		Lossy        bool   `json:"lossy"`
	} `json:"fork"`
}

// listChatsWithForkForTask fetches taskId's chats (archived included) as
// chatWithForkForTest rows, so a test can assert on the fork object.
func listChatsWithForkForTask(t *testing.T, client *wsclient.Client, taskID int64) []chatWithForkForTest {
	t.Helper()
	var chats []chatWithForkForTest
	if err := client.Call(context.Background(), "chat.list", map[string]any{"taskId": taskID, "includeArchived": true}, &chats); err != nil {
		t.Fatalf("chat.list: %v", err)
	}
	return chats
}

// onlyRunInChat returns the id of chatID's single run, so a test can name
// the finished turn it forks at.
func onlyRunInChat(t *testing.T, client *wsclient.Client, chatID int64) string {
	t.Helper()
	var runs []struct {
		ID string
	}
	if err := client.Call(context.Background(), "run.list", map[string]any{"chatId": chatID}, &runs); err != nil {
		t.Fatalf("run.list(chatId=%d): %v", chatID, err)
	}
	if len(runs) != 1 {
		t.Fatalf("run.list(chatId=%d) = %d runs, want 1", chatID, len(runs))
	}
	return runs[0].ID
}

// forkChat runs `task chat fork <chatId> --at <runId>` (plus extra args)
// with stdout and stderr captured separately.
func forkChat(t *testing.T, chatID int64, runID string, extra ...string) (int, string, string) {
	t.Helper()
	args := []string{"task", "chat", "fork", strconv.FormatInt(chatID, 10), "--at", runID}
	args = append(args, extra...)
	code := -1
	var out, errOut string
	errOut = captureStderr(t, func() {
		out = captureStdout(t, func() { code = run(args) })
	})
	return code, out, errOut
}

// finishedRunOnDefaultChat sets up a task whose default chat carries one
// finished fake-agent run, returning the chat and that run's id.
func finishedRunOnDefaultChat(t *testing.T) (*wsclient.Client, int64, int64, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)
	chat := listChatsForTask(t, client, taskID)[0]
	if code := runTaskSend(t, taskID, 0); code != 0 {
		t.Fatalf("run(task send) = %d, want 0", code)
	}
	return client, taskID, chat.ID, onlyRunInChat(t, client, chat.ID)
}

// TestTaskChatForkSeeded proves `--seeded` creates the lossy child chat:
// one line of three tab-separated fields (new id, "Fork of ..." title,
// mode seeded) and one more chat in chat.list carrying a lossy fork
// object.
func TestTaskChatForkSeeded(t *testing.T) {
	client, taskID, chatID, runID := finishedRunOnDefaultChat(t)
	before := len(listChatsForTask(t, client, taskID))

	code, out, _ := forkChat(t, chatID, runID, "--seeded")
	if code != 0 {
		t.Fatalf("run(task chat fork --seeded) = %d, want 0", code)
	}
	fields := strings.Split(strings.TrimRight(out, "\n"), "\t")
	if len(fields) != 3 {
		t.Fatalf("stdout = %q, want three tab-separated fields", out)
	}
	if !strings.HasPrefix(fields[1], "Fork of") {
		t.Fatalf("stdout title field = %q, want it to start with %q", fields[1], "Fork of")
	}
	if fields[2] != "seeded" {
		t.Fatalf("stdout mode field = %q, want %q", fields[2], "seeded")
	}
	newID, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		t.Fatalf("stdout id field = %q: %v", fields[0], err)
	}

	chats := listChatsWithForkForTask(t, client, taskID)
	if len(chats) != before+1 {
		t.Fatalf("chats after fork = %d, want %d", len(chats), before+1)
	}
	for _, c := range chats {
		if c.ID != newID {
			continue
		}
		if c.Fork == nil {
			t.Fatalf("forked chat %d has no fork object: %+v", c.ID, c)
		}
		if !c.Fork.Lossy || c.Fork.Mode != "seeded" || c.Fork.ParentChatID != chatID {
			t.Fatalf("fork of chat %d = %+v, want a lossy seeded fork of %d", c.ID, c.Fork, chatID)
		}
		return
	}
	t.Fatalf("chat.list after fork has no chat %d: %+v", newID, chats)
}

// TestTaskChatForkNativeRefusedSuggestsSeeded proves a native fork of an
// ACP chat is refused the documented way: exit 1, the "cannot be forked
// here" line, the --seeded suggestion, and no chat created.
func TestTaskChatForkNativeRefusedSuggestsSeeded(t *testing.T) {
	client, taskID, chatID, runID := finishedRunOnDefaultChat(t)
	before := len(listChatsForTask(t, client, taskID))

	code, _, errOut := forkChat(t, chatID, runID)
	if code != 1 {
		t.Fatalf("run(task chat fork native) = %d, want 1 (stderr = %q)", code, errOut)
	}
	if want := "cannot be forked here"; !strings.Contains(errOut, want) {
		t.Fatalf("stderr = %q, want it to contain %q", errOut, want)
	}
	if want := "--seeded"; !strings.Contains(errOut, want) {
		t.Fatalf("stderr = %q, want it to contain %q", errOut, want)
	}
	if got := len(listChatsForTask(t, client, taskID)); got != before {
		t.Fatalf("chats after a refused fork = %d, want %d (nothing created)", got, before)
	}
}

// TestTaskChatForkUsageErrors pins the argument contract: no args, a
// missing --at, a bad chat id, an unknown flag and a repeated --at each
// exit 2 with the usage line.
func TestTaskChatForkUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"1"},
		{"x", "--at", "r"},
		{"1", "--at"},
		{"1", "--at", "r", "--bogus"},
		{"1", "--at", "r", "--at", "s"},
	} {
		full := append([]string{"task", "chat", "fork"}, args...)
		code := -1
		errOut := captureStderr(t, func() {
			captureStdout(t, func() { code = run(full) })
		})
		if code != 2 {
			t.Errorf("run(%v) = %d, want 2", full, code)
		}
		if !strings.Contains(errOut, cmdTaskChatForkUsage) {
			t.Errorf("run(%v) stderr = %q, want the usage line %q", full, errOut, cmdTaskChatForkUsage)
		}
	}
}
