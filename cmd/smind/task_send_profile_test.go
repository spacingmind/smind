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

// seatThenStop starts a seat run with the profile and waits until it is
// stopped, so the chat is free for the follow-up under test.
func seatThenStop(t *testing.T, client *wsclient.Client, id, profID string) int64 {
	t.Helper()
	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "seat", "--profile", profID, "--detach"})
	if code != 0 {
		t.Fatalf("seat send exit = %d; stderr = %q", code, stderr)
	}
	runID := strings.TrimSpace(stdout)
	waitForRunStatus(t, client, runID, "running")
	stopRun(t, client, runID)
	waitForRunStatus(t, client, runID, "stopped")
	want, _ := strconv.ParseInt(profID, 10, 64)
	return want
}

func TestTaskSend_ProfileNoneSendsZeroAndKeepsOldArity(t *testing.T) {
	client, id := hangingTask(t)
	profID := seedGLMProfile(t, client, map[string]any{})
	seatThenStop(t, client, id, profID)

	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "plain", "again", "--profile", "none", "--mode", "plan", "--detach"})
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
	if run.ProfileID != 0 || run.Provider != "glm" || run.PermissionMode != "plan" || run.Prompt != "plain again" {
		t.Fatalf("run = profile %d provider %q mode %q prompt %q", run.ProfileID, run.Provider, run.PermissionMode, run.Prompt)
	}
}

func TestTaskSend_NoProfileFlagOmitsTheKey(t *testing.T) {
	client, id := hangingTask(t)
	profID := seedGLMProfile(t, client, map[string]any{})
	want := seatThenStop(t, client, id, profID)

	// No profileId on the wire: the daemon inherits the seat's profile.
	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "follow", "up", "--detach"})
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
	if run.ProfileID != want {
		t.Fatalf("follow-up ProfileID = %d, want %d (inherited)", run.ProfileID, want)
	}
}

func TestTaskSend_ProfileInvalidValueRejected(t *testing.T) {
	client, id := hangingTask(t)
	for _, v := range []string{"abc", "0", "-3"} {
		code, _, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi", "--profile", v, "--detach"})
		if code != 2 || !strings.Contains(stderr, "invalid --profile value") || !strings.Contains(stderr, "none") {
			t.Errorf("--profile %s: code %d stderr %q", v, code, stderr)
		}
	}
	if rows := listRuns(t, client); len(rows) != 0 {
		t.Errorf("runs started despite rejection: %+v", rows)
	}
}
