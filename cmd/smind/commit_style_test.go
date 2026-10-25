package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

type promptRecorder struct {
	mu      sync.Mutex
	prompts []string
}

func (r *promptRecorder) Run(_ context.Context, req executor.Request) (executor.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, req.UserPrompt)
	return executor.Result{FinalText: "feat: drafted"}, nil
}

func TestDraftStyleFromWorkspace_ReachesPrompt(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(t, repo, args...)
	}
	if out, err := exec.Command("sh", "-c", "echo base > "+filepath.Join(repo, "a.txt")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "chore: initial")

	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	wm := workspace.New(st)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatal(err)
	}
	wt := *task.WorktreePath
	if out, err := exec.Command("sh", "-c", "echo changed >> "+filepath.Join(wt, "a.txt")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	gitRun(t, wt, "add", "a.txt")

	rec := &promptRecorder{}
	drafter := newCommitDrafter(wm, rec)

	if _, err := drafter.DraftCommitMessage(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.prompts[0], "<style_guidance>") {
		t.Fatalf("no style set but prompt has style guidance:\n%s", rec.prompts[0])
	}

	if _, err := wm.SetCommitMessageStyle(ws.ID, "STYLE-SENTINEL use gitmoji"); err != nil {
		t.Fatal(err)
	}
	// A fresh drafter: the service rate-limits repeat drafts per workspace.
	drafter = newCommitDrafter(wm, rec)
	if _, err := drafter.DraftCommitMessage(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	if got := rec.prompts[len(rec.prompts)-1]; !strings.Contains(got, "<style_guidance>\nSTYLE-SENTINEL use gitmoji\n</style_guidance>") {
		t.Fatalf("prompt lacks the workspace style:\n%s", got)
	}
}
