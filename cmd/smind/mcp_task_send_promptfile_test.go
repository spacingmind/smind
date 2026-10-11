package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/testutil"
)

func TestMCPTools_TaskSendPromptFile(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)
	wt := taskWorktree(t, client, taskID)

	if err := os.MkdirAll(filepath.Join(wt, "briefs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "briefs", "a.md"), []byte("do the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readPromptFile(wt, "briefs/a.md"); err != nil || got != "do the thing\n" {
		t.Fatalf("readPromptFile = %q, %v", got, err)
	}

	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{
		"taskId": taskID, "provider": "glm", "promptFile": "briefs/a.md",
	}, &sent); isErr || sent.RunID == "" {
		t.Fatalf("task_send promptFile = %+v isErr=%v, want a runId", sent, isErr)
	}
	waitForRunStatus(t, client, sent.RunID, "done")

	// prompt and promptFile are mutually exclusive.
	if msg := taskSendErr(t, env, map[string]any{"taskId": taskID, "provider": "glm", "prompt": "x", "promptFile": "briefs/a.md"}); !strings.Contains(msg, "not both") {
		t.Fatalf("both = %q", msg)
	}
}

func TestMCPTools_TaskSendPromptFileRejectsEscape(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)
	wt := taskWorktree(t, client, taskID)

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Link(t, secret, filepath.Join(wt, "link.txt"))
	testutil.Link(t, outside, filepath.Join(wt, "linkdir"))
	if err := os.WriteFile(filepath.Join(wt, "big.txt"), []byte(strings.Repeat("a", mcpPromptFileMaxBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(wt, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct{ file, want string }{
		"absolute":     {secret, "relative"},
		"dotdot":       {"../" + filepath.Base(outside) + "/secret.txt", ".."},
		"dotdot-mid":   {"adir/../../x", ".."},
		"symlink":      {"link.txt", "outside"},
		"symlink-dir":  {"linkdir/secret.txt", "outside"},
		"oversize":     {"big.txt", "limit"},
		"directory":    {"adir", "regular"},
		"missing file": {"nope.md", "resolve"},
	}
	for name, c := range cases {
		msg := taskSendErr(t, env, map[string]any{"taskId": taskID, "provider": "glm", "promptFile": c.file})
		if !strings.Contains(msg, "promptFile") || !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want promptFile error mentioning %q", name, msg, c.want)
		}
	}
}
