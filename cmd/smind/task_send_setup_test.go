package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

// newSetupTask creates a repo workspace (with setupScript when non-empty)
// and a task in it, returning the client, the task id string and the id.
func newSetupTask(t *testing.T, setupScript string) (*wsclient.Client, string, int64) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	client := dialTestClient(t, srvURL)

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"},
		{"add", "README.md"}, {"commit", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repoDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	var ws struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "workspace.create", map[string]any{"path": repoDir, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	if setupScript != "" {
		if err := client.Call(context.Background(), "workspace.setScripts", map[string]any{"workspaceId": ws.ID, "setupScript": setupScript}, nil); err != nil {
			t.Fatalf("workspace.setScripts: %v", err)
		}
	}
	var task struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "task.create", map[string]any{"workspaceId": ws.ID, "title": "T"}, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	return client, strconv.FormatInt(task.ID, 10), task.ID
}

const slowSetup = "sleep 2\nexit 0\n"

func TestTaskSend_QueuesWhileSetupRuns(t *testing.T) {
	client, id, taskID := newSetupTask(t, slowSetup)
	code, _, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi"})
	if code != 0 {
		t.Fatalf("task send = %d; stderr = %q", code, stderr)
	}
	for _, want := range []string{"setup script is still running; queued", "queued as item"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, r := range listRuns(t, client) {
			if r.TaskID == taskID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("queued prompt was not delivered within 15s of setup finishing")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestTaskSend_ExplicitRejectKept(t *testing.T) {
	_, id, _ := newSetupTask(t, slowSetup)
	code, _, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi", "--when-busy", "reject"})
	if code != 1 {
		t.Fatalf("task send --when-busy reject = %d; stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "setup script is still running") {
		t.Fatalf("stderr = %q, want the setup-busy rejection", stderr)
	}
}

func TestTaskSend_NoSetupNoNote(t *testing.T) {
	_, id, _ := newSetupTask(t, "")
	code, _, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi"})
	if code != 0 {
		t.Fatalf("task send = %d; stderr = %q", code, stderr)
	}
	if strings.Contains(stderr, "setup script") {
		t.Fatalf("stderr = %q, want no setup note", stderr)
	}
}
