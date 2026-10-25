package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/commitdraft"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"

	"context"
)

type stubDrafter struct {
	calls atomic.Int32
	res   wsapi.CommitDraft
	err   error
}

func (s *stubDrafter) DraftCommitMessage(context.Context, int64) (wsapi.CommitDraft, error) {
	s.calls.Add(1)
	return s.res, s.err
}

func TestTaskCommitMessageCLI_PrintsAndDoesNotCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	wm := workspace.New(st)
	api, err := wsapi.New(wm, accounts.New(st), taskrunner.New(wm), st, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	stub := &stubDrafter{res: wsapi.CommitDraft{Message: "feat(cli): drafted subject\n\nbody line", Model: "claude-haiku-5-5", Truncated: true, Omitted: []string{".env"}}}
	api.SetCommitDrafter(stub)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	writeTestConfig(t, home, srv.URL)

	client := dialTestClient(t, srv.URL)
	taskID := newTestRepoTask(t, client)
	task, err := st.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	wt := *task.WorktreePath
	gitOut := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", wt}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut("add", "README.md")
	head, status := gitOut("rev-parse", "HEAD"), gitOut("status", "--porcelain=v2")

	// stdout carries only the message; the notes go to stderr.
	var code int
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { code = run([]string{"task", "commit-message", strconv.FormatInt(taskID, 10)}) })
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if out != "feat(cli): drafted subject\n\nbody line\n" {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(stderr, "truncated") || !strings.Contains(stderr, ".env") {
		t.Fatalf("stderr = %q, want truncated and .env notes", stderr)
	}
	if got := gitOut("rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD moved: %s -> %s", head, got)
	}
	if got := gitOut("status", "--porcelain=v2"); got != status {
		t.Fatalf("status changed:\n%s\n%s", status, got)
	}
	if n := stub.calls.Load(); n != 1 {
		t.Fatalf("drafter calls = %d, want 1", n)
	}

	// A typed failure exits non-zero with its text and prints nothing to stdout.
	stub.err = commitdraft.ErrNothingStaged
	out = captureStdout(t, func() {
		stderr = captureStderr(t, func() { code = run([]string{"task", "commit-message", strconv.FormatInt(taskID, 10)}) })
	})
	if code == 0 || out != "" || !strings.Contains(stderr, "nothing is staged") {
		t.Fatalf("failure: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	// Bad usage.
	if c, _ := runCaptured(t, []string{"task", "commit-message"}); c != 2 {
		t.Fatalf("no args exit = %d, want 2", c)
	}
}
