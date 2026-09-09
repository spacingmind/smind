package wsapi

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestServer_CommitFlow drives the full review-and-commit flow over a real
// WS connection: files list, per-file diff, stage, staged-state
// observability, unstage round-trip, commit of only the staged set, and
// the branch tip proving the unstaged file stayed out.
func TestServer_CommitFlow(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	wt := *task.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// task.files: both changed files, neither staged.
	sendRequest(t, ws, "1", "task.files", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.files error = %v", resp.Error.Message)
	}
	var filesResult struct {
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
			Staged bool   `json:"staged"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp.Result, &filesResult); err != nil {
		t.Fatalf("decode task.files result: %v", err)
	}
	if len(filesResult.Files) != 2 {
		t.Fatalf("task.files = %+v, want 2 entries", filesResult.Files)
	}
	for _, f := range filesResult.Files {
		if f.Staged {
			t.Fatalf("task.files = %+v, want nothing staged initially", filesResult.Files)
		}
		if f.Path == "README.md" && f.Status != "modified" {
			t.Fatalf("README.md status = %q, want modified", f.Status)
		}
		if f.Path == "notes.txt" && f.Status != "added" {
			t.Fatalf("notes.txt status = %q, want added", f.Status)
		}
	}

	// task.fileDiff: one file's slice; unchanged path is empty.
	sendRequest(t, ws, "2", "task.fileDiff", map[string]any{"taskId": task.ID, "path": "notes.txt"})
	resp = readEnvelopeFor(t, ws, "2", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.fileDiff error = %v", resp.Error.Message)
	}
	var fileDiff struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(resp.Result, &fileDiff); err != nil {
		t.Fatalf("decode task.fileDiff result: %v", err)
	}
	if !strings.Contains(fileDiff.Diff, "brand new") {
		t.Fatalf("task.fileDiff(notes.txt) = %q, want the added content", fileDiff.Diff)
	}
	sendRequest(t, ws, "3", "task.fileDiff", map[string]any{"taskId": task.ID, "path": "nope.txt"})
	resp = readEnvelopeFor(t, ws, "3", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.fileDiff(nope) error = %v", resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Result, &fileDiff); err != nil {
		t.Fatalf("decode task.fileDiff result: %v", err)
	}
	if fileDiff.Diff != "" {
		t.Fatalf("task.fileDiff(nope.txt) = %q, want empty", fileDiff.Diff)
	}

	// task.stage one file; task.files then shows it staged.
	sendRequest(t, ws, "4", "task.stage", map[string]any{"taskId": task.ID, "path": "README.md", "staged": true})
	resp = readEnvelopeFor(t, ws, "4", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.stage error = %v", resp.Error.Message)
	}
	sendRequest(t, ws, "5", "task.files", map[string]any{"taskId": task.ID})
	resp = readEnvelopeFor(t, ws, "5", 5*time.Second)
	if err := json.Unmarshal(resp.Result, &filesResult); err != nil {
		t.Fatalf("decode task.files result: %v", err)
	}
	stagedCount := 0
	for _, f := range filesResult.Files {
		if f.Staged {
			stagedCount++
			if f.Path != "README.md" {
				t.Fatalf("staged file = %q, want README.md", f.Path)
			}
		}
	}
	if stagedCount != 1 {
		t.Fatalf("staged count = %d, want 1", stagedCount)
	}

	// Unstage round-trip: README.md goes back to unstaged.
	sendRequest(t, ws, "6", "task.stage", map[string]any{"taskId": task.ID, "path": "README.md", "staged": false})
	resp = readEnvelopeFor(t, ws, "6", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.stage(unstage) error = %v", resp.Error.Message)
	}
	sendRequest(t, ws, "7", "task.files", map[string]any{"taskId": task.ID})
	resp = readEnvelopeFor(t, ws, "7", 5*time.Second)
	if err := json.Unmarshal(resp.Result, &filesResult); err != nil {
		t.Fatalf("decode task.files result: %v", err)
	}
	for _, f := range filesResult.Files {
		if f.Staged {
			t.Fatalf("task.files = %+v after unstage, want nothing staged", filesResult.Files)
		}
	}

	// Nothing staged: clean error, no stderr leak.
	sendRequest(t, ws, "8", "task.commit", map[string]any{"taskId": task.ID, "message": "m", "author": "human"})
	resp = readEnvelopeFor(t, ws, "8", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("task.commit with nothing staged: error = nil, want an error")
	}
	if !strings.Contains(resp.Error.Message, "nothing staged") {
		t.Fatalf("task.commit error = %q, want a clean nothing-staged message", resp.Error.Message)
	}

	// Stage README.md only and commit: the branch tip records exactly it.
	sendRequest(t, ws, "9", "task.stage", map[string]any{"taskId": task.ID, "path": "README.md", "staged": true})
	readEnvelopeFor(t, ws, "9", 5*time.Second)
	sendRequest(t, ws, "10", "task.commit", map[string]any{"taskId": task.ID, "message": "only readme", "author": "human"})
	resp = readEnvelopeFor(t, ws, "10", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.commit error = %v", resp.Error.Message)
	}
	var commit struct {
		Commit  string `json:"commit"`
		Subject string `json:"subject"`
		Files   int    `json:"files"`
	}
	if err := json.Unmarshal(resp.Result, &commit); err != nil {
		t.Fatalf("decode task.commit result: %v", err)
	}
	if commit.Subject != "only readme" || commit.Files != 1 {
		t.Fatalf("task.commit result = %+v, want subject %q and 1 file", commit, "only readme")
	}
	tip := runGitTOut(t, wt, "rev-parse", "HEAD")
	if commit.Commit != strings.TrimSpace(tip) {
		t.Fatalf("commit sha = %q, want branch tip %q", commit.Commit, tip)
	}
	inCommit := runGitTOut(t, wt, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	if strings.TrimSpace(inCommit) != "README.md" {
		t.Fatalf("commit records %q, want only README.md", inCommit)
	}
}

// TestServer_TaskCommit_AgentTrailers proves an agent-authored task.commit
// lands the Smind-Agent/Smind-Task trailers byte-exact on the branch.
func TestServer_TaskCommit_AgentTrailers(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	wt := *task.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.stage", map[string]any{"taskId": task.ID, "path": "notes.txt", "staged": true})
	readEnvelopeFor(t, ws, "1", 5*time.Second)
	sendRequest(t, ws, "2", "task.commit", map[string]any{
		"taskId": task.ID, "message": "agent work", "author": "agent", "agent": "glm",
	})
	resp := readEnvelopeFor(t, ws, "2", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.commit error = %v", resp.Error.Message)
	}

	body := runGitTOut(t, wt, "log", "-1", "--format=%B")
	for _, want := range []string{
		"agent work", "Smind-Agent: glm", "Smind-Task: " + strconv.FormatInt(task.ID, 10),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("commit body = %q, want it to contain %q", body, want)
		}
	}
}

func runGitTOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
	return stdout.String()
}
