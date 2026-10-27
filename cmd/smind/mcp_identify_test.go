package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spacingmind/smind/internal/wsapi"
)

func buildSmindForTest(t *testing.T) string { return buildSmind(t) }

// identifyEnv starts a daemon with one running "hang" run and returns its
// URL, the run's id and its task id.
func identifyEnv(t *testing.T) (srvURL, runID string, taskID int64) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL = newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	runID = startTestRun(t, srvURL, "glm", "hang")
	client := dialTestClient(t, srvURL)
	var list []struct {
		ID     string
		TaskID int64
	}
	if err := client.Call(context.Background(), "run.list", map[string]any{}, &list); err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID == runID {
			return srvURL, runID, r.TaskID
		}
	}
	t.Fatalf("run %s not in run.list", runID)
	return
}

func taskWorkspace(t *testing.T, srvURL string, taskID int64) int64 {
	t.Helper()
	var task taskForTest
	if err := dialTestClient(t, srvURL).Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil {
		t.Fatal(err)
	}
	return task.WorkspaceID
}

func connectServe(t *testing.T, bin string, env ...string) *mcp.ClientSession {
	t.Helper()
	cmd := exec.Command(bin, "mcp", "serve")
	cmd.Env = append(os.Environ(), env...)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(context.Background(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestMCPServe_IdentifiesWhenRunIdEnvSet(t *testing.T) {
	srvURL, runID, seatTask := identifyEnv(t)
	wsID := taskWorkspace(t, srvURL, seatTask)
	bin := buildSmindForTest(t)

	// Identified: tools work, and the daemon defaults the parent to the
	// run's own task.
	cs := connectServe(t, bin, "SMIND_RUN_ID="+runID)
	var created struct {
		Task taskForTest `json:"task"`
	}
	if isErr := callMCPTool(t, cs, "task_new", map[string]any{"workspaceId": wsID, "title": "Child"}, &created); isErr {
		t.Fatal("task_new returned an error result")
	}
	if created.Task.ParentTaskID == nil || *created.Task.ParentTaskID != seatTask {
		t.Fatalf("task = %+v, want parent = the run's task #%d", created.Task, seatTask)
	}

	// A bogus id: serve fails with the identify error, before any MCP.
	cmd := exec.Command(bin, "mcp", "serve")
	cmd.Env = append(os.Environ(), "SMIND_RUN_ID=no-such-run")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("serve with a bogus run id exited 0: %s", out)
	}
	if !strings.Contains(string(out), "mcp serve: identify as run no-such-run:") || !strings.Contains(string(out), "unknown run") {
		t.Fatalf("output = %q, want the identify error", out)
	}
}

func TestMCPServe_NoEnvNoIdentify(t *testing.T) {
	srvURL, _, seatTask := identifyEnv(t)
	wsID := taskWorkspace(t, srvURL, seatTask)
	cs := connectServe(t, buildSmindForTest(t), "SMIND_RUN_ID=")
	var created struct {
		Task taskForTest `json:"task"`
	}
	if isErr := callMCPTool(t, cs, "task_new", map[string]any{"workspaceId": wsID, "title": "Plain"}, &created); isErr {
		t.Fatal("task_new returned an error result")
	}
	if created.Task.ParentTaskID != nil {
		t.Fatalf("task = %+v, want no parent (unidentified)", created.Task)
	}
}

// TestMCPCalledMethodsAreRunScopedAllowed keeps wsapi.RunScopedMethods in
// step with the MCP tools: every wsapi method named in a non-test mcp*.go
// source must be allowed for an identified connection, so a future tool
// cannot be silently blocked.
func TestMCPCalledMethodsAreRunScopedAllowed(t *testing.T) {
	files, err := filepath.Glob("mcp*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	re := regexp.MustCompile(`"((?:task|run|chat|workspace|provider|profile|account|usage|queue|session)\.[A-Za-z]+)"`)
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen++
			if !slices.Contains(wsapi.RunScopedMethods, m[1]) {
				t.Errorf("%s calls %q, which is not in wsapi.RunScopedMethods", f, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("scanned no method literals; the regexp is stale")
	}
}
