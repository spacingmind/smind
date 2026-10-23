package main

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/workspace"
)

func TestRenderArchivePreview(t *testing.T) {
	tests := []struct {
		name         string
		p            workspace.ArchivePreview
		deleteBranch bool
		want         string
	}{
		{
			name: "all risks, branch kept",
			p: workspace.ArchivePreview{
				Branch: workspace.PreviewBranch{Name: "feat"}, Merged: workspace.PreviewMerged{State: "unmerged"},
				Risks:            workspace.PreviewRisks{Uncommitted: workspace.PreviewUncommitted{Files: 2, Added: 5, Deleted: 1}, CommitsAheadOfOrigin: 1},
				IgnoredFilesLost: true, HasTeardown: true,
			},
			want: "  Uncommitted changes (5 added, 1 deleted lines) - saved to the branch first\n" +
				"! 1 commit not pushed\n" +
				"! Branch feat is not merged anywhere\n" +
				"! Files git ignores (node_modules, .env) are not kept\n" +
				"  The workspace's teardown script runs first\n" +
				"  The branch is kept\n",
		},
		{
			name: "never pushed, plural",
			p: workspace.ArchivePreview{
				Branch: workspace.PreviewBranch{Name: "feat"}, Merged: workspace.PreviewMerged{State: "unknown"},
				Risks: workspace.PreviewRisks{CommitsAheadOfBase: 3},
			},
			want: "! 3 commits only on this machine (the branch was never pushed)\n  The branch is kept\n",
		},
		{
			name: "merged via pr, will be deleted",
			p: workspace.ArchivePreview{
				Branch: workspace.PreviewBranch{Name: "feat", Owned: true}, Base: workspace.PreviewBase{Ref: "origin/develop"},
				Merged: workspace.PreviewMerged{State: "merged", Via: "pr"}, CanDeleteBranch: true,
			},
			deleteBranch: true,
			want: "  Branch feat is merged into origin/develop (its pull request was merged)\n" +
				"  Branch feat will be deleted\n",
		},
		{
			name: "deletion blocked with reason",
			p: workspace.ArchivePreview{
				Branch: workspace.PreviewBranch{Name: "mine"}, DeleteBlockedReason: "This is your own branch; smind never deletes it.",
			},
			deleteBranch: true,
			want:         "! Branch deletion is blocked without --force: This is your own branch; smind never deletes it.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderArchivePreview(&buf, tc.p, tc.deleteBranch)
			if buf.String() != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), tc.want)
			}
		})
	}
}

func TestTaskArchive_Usage(t *testing.T) {
	for _, args := range [][]string{
		{"task", "archive"},
		{"task", "archive", "abc"},
		{"task", "archive", "0"},
		{"task", "archive", "1", "2"},
		{"task", "archive", "1", "--nope"},
	} {
		if code, _ := runCaptured(t, args); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}

func TestTaskArchive_Daemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	client := dialTestClient(t, srvURL)

	archived := func(id int64) bool {
		var task struct {
			ArchivedAt *string `json:"archivedAt"`
		}
		if err := client.Call(context.Background(), "task.get", map[string]any{"id": id}, &task); err != nil {
			t.Fatalf("task.get: %v", err)
		}
		return task.ArchivedAt != nil
	}

	t.Run("refusal printed verbatim, nothing archived", func(t *testing.T) {
		id := newTestRepoTask(t, client)
		code := -1
		var stderr string
		out := captureStdout(t, func() {
			stderr = captureStderr(t, func() {
				code = run([]string{"task", "archive", strconv.FormatInt(id, 10), "--delete-branch"})
			})
		})
		if want := "task archive: The branch is not merged anywhere yet."; !strings.Contains(stderr, want) {
			t.Errorf("stderr %q missing the daemon's refusal %q", stderr, want)
		}
		if code != 1 {
			t.Fatalf("code = %d, want 1; out %q", code, out)
		}
		if !strings.Contains(out, "! Branch deletion is blocked without --force:") {
			t.Errorf("preview block line missing: %q", out)
		}
		if archived(id) {
			t.Fatal("task was archived despite the refusal")
		}
	})

	t.Run("force passes through and archives", func(t *testing.T) {
		id := newTestRepoTask(t, client)
		code, out := runCaptured(t, []string{"task", "archive", strconv.FormatInt(id, 10), "--delete-branch", "--force"})
		if code != 0 || !strings.Contains(out, "archived task") {
			t.Fatalf("code = %d, out %q", code, out)
		}
		if !archived(id) {
			t.Fatal("task not archived")
		}
	})

	t.Run("plain archive prints risks and archives", func(t *testing.T) {
		id := newTestRepoTask(t, client)
		code, out := runCaptured(t, []string{"task", "archive", strconv.FormatInt(id, 10)})
		if code != 0 {
			t.Fatalf("code = %d, out %q", code, out)
		}
		for _, want := range []string{"Files git ignores", "The branch is kept", "archived task"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q: %q", want, out)
			}
		}
		if !archived(id) {
			t.Fatal("task not archived")
		}
		if code, _ := runCaptured(t, []string{"task", "archive", strconv.FormatInt(id, 10)}); code != 1 {
			t.Fatalf("second archive code = %d, want 1", code)
		}
	})
}
