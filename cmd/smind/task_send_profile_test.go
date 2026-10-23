package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// seedGLMProfile creates a glm profile through profile.create and returns its id.
func seedGLMProfile(t *testing.T, client *wsclient.Client, params map[string]any) string {
	t.Helper()
	params["name"], params["provider"] = "p-"+strconv.Itoa(len(params)), "glm"
	var p store.AgentProfile
	if err := client.Call(context.Background(), "profile.create", params, &p); err != nil {
		t.Fatalf("profile.create: %v", err)
	}
	return strconv.FormatInt(p.ID, 10)
}

// daemonStoreFor finds this test's daemon store: writeTestConfig put the
// daemon's port in $SMIND_HOME/config.yaml and testDaemonStores is keyed by
// the daemon's URL.
func daemonStoreFor(t *testing.T, _ *wsclient.Client) *store.Store {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("SMIND_HOME"), "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	port := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(string(b), "server:")), "port:"))
	v, ok := testDaemonStores.Load("http://127.0.0.1:" + port)
	if !ok {
		t.Fatalf("no test daemon store for port %q (config %q)", port, b)
	}
	return v.(*store.Store)
}

func TestTaskSend_WithProfileUsesProfileSettings(t *testing.T) {
	client, id := hangingTask(t)
	profID := seedGLMProfile(t, client, map[string]any{"permissionMode": "plan", "autoAccept": true, "model": "glm-test-model"})

	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "do the thing", "--profile", profID, "--detach"})
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %q", code, stderr)
	}
	runID := strings.TrimSpace(stdout)
	defer stopRun(t, client, runID)
	waitForRunStatus(t, client, runID, "running")

	run, err := daemonStoreFor(t, client).GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	wantID, _ := strconv.ParseInt(profID, 10, 64)
	if run.Provider != "glm" || run.PermissionMode != "plan" || !run.AutoAccept || run.Model != "glm-test-model" || run.ProfileID != wantID {
		t.Fatalf("run = provider %q mode %q autoAccept %v model %q profile %d; want glm/plan/true/glm-test-model/%d",
			run.Provider, run.PermissionMode, run.AutoAccept, run.Model, run.ProfileID, wantID)
	}
	if run.Prompt != "do the thing" {
		t.Errorf("prompt = %q", run.Prompt)
	}
}

func TestTaskSend_ProfileRejectsModeAndAutoAccept(t *testing.T) {
	client, id := hangingTask(t)
	profID := seedGLMProfile(t, client, map[string]any{})
	for _, extra := range [][]string{{"--mode", "plan"}, {"--auto-accept"}} {
		args := append([]string{"task", "send", id, "hi", "--profile", profID, "--detach"}, extra...)
		code, _, stderr := sendCaptured(t, args)
		if code != 2 || !strings.Contains(stderr, "cannot be combined with --profile") {
			t.Errorf("%v: code %d stderr %q", extra, code, stderr)
		}
	}
	if rows := listRuns(t, client); len(rows) != 0 {
		t.Errorf("runs started despite rejection: %+v", rows)
	}
}

func TestTaskSend_ProfileNotFound(t *testing.T) {
	client, id := hangingTask(t)
	code, _, stderr := sendCaptured(t, []string{"task", "send", id, "hi", "--profile", "9999", "--detach"})
	if code != 1 || !strings.Contains(stderr, "profile 9999") {
		t.Fatalf("code %d stderr %q, want exit 1 naming profile 9999", code, stderr)
	}
	if rows := listRuns(t, client); len(rows) != 0 {
		t.Errorf("runs started: %+v", rows)
	}
}

func TestTaskSend_WithoutProfileUnchanged(t *testing.T) {
	client, id := hangingTask(t)
	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hello", "there", "--mode", "plan", "--detach"})
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %q", code, stderr)
	}
	runID := strings.TrimSpace(stdout)
	defer stopRun(t, client, runID)
	waitForRunStatus(t, client, runID, "running")
	run, err := daemonStoreFor(t, client).GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Provider != "glm" || run.PermissionMode != "plan" || run.ProfileID != 0 || run.Prompt != "hello there" {
		t.Fatalf("run = %+v", run)
	}
	// Missing provider/prompt still prints the usage and exits 2.
	if code, _, stderr := sendCaptured(t, []string{"task", "send", id, "glm"}); code != 2 || !strings.Contains(stderr, "usage: smind task send") {
		t.Errorf("no prompt: code %d stderr %q", code, stderr)
	}
}
