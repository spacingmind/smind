package wsapi

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestServer_FileRead_ReturnsMtime proves file.read's terminal result
// carries the file's mtime (RFC 3339), the value file.write's
// expectedMtime echoes back.
func TestServer_FileRead_ReturnsMtime(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	want := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	if err := os.Chtimes(filepath.Join(*task.WorktreePath, "README.md"), want, want); err != nil {
		t.Fatalf("chtimes README.md: %v", err)
	}

	sendRequest(t, ws, "read", "file.read", map[string]any{"taskId": task.ID, "path": "README.md"})
	resp := readEnvelopeFor(t, ws, "read", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("file.read error = %v", resp.Error.Message)
	}
	var result struct {
		Content string   `json:"content"`
		Mtime   DateTime `json:"mtime"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode file.read result: %v", err)
	}
	if result.Content != "hello\n" {
		t.Fatalf("content = %q, want %q", result.Content, "hello\n")
	}
	if got := time.Time(result.Mtime); !got.Equal(want) {
		t.Fatalf("mtime = %v, want %v", got, want)
	}
}

// TestServer_FileWrite_StaleExpectedMtime_Conflict proves the conditional
// write's rejected-if-changed path end to end over the wire: expectedMtime
// that no longer matches yields an error carrying code "conflict" (not just
// a message), and the file on disk is left untouched.
func TestServer_FileWrite_StaleExpectedMtime_Conflict(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	mtime := time.Time(readMtime(t, ws, task.ID, "README.md"))
	// Simulate the agent's rewrite out of band: new mtime, new content.
	newTime := mtime.Add(time.Second)
	if err := os.Chtimes(filepath.Join(*task.WorktreePath, "README.md"), newTime, newTime); err != nil {
		t.Fatalf("chtimes README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "README.md"), []byte("agent version\n"), 0o644); err != nil {
		t.Fatalf("rewrite README.md: %v", err)
	}

	sendRequest(t, ws, "write", "file.write", map[string]any{
		"taskId": task.ID, "path": "README.md", "content": "human version\n",
		"expectedMtime": time.Time(mtime).Format(time.RFC3339Nano),
	})
	resp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("file.write(stale expectedMtime) error = nil, want a conflict")
	}
	if resp.Error.Code != "conflict" {
		t.Fatalf("error code = %q, want %q (message was %q)", resp.Error.Code, "conflict", resp.Error.Message)
	}

	disk, err := os.ReadFile(filepath.Join(*task.WorktreePath, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(disk) != "agent version\n" {
		t.Fatalf("disk content = %q, want the agent's %q (a conflicting write must not land)", disk, "agent version\n")
	}
}

// TestServer_FileWrite_MatchingExpectedMtime_Succeeds proves the
// conditional write's happy path over the wire, including that the result
// carries the new mtime for the editor to chain into its next save.
func TestServer_FileWrite_MatchingExpectedMtime_Succeeds(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	mtime := time.Time(readMtime(t, ws, task.ID, "README.md"))

	sendRequest(t, ws, "write", "file.write", map[string]any{
		"taskId": task.ID, "path": "README.md", "content": "saved cleanly\n",
		"expectedMtime": time.Time(mtime).Format(time.RFC3339Nano),
	})
	resp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("file.write(matching expectedMtime) error = %v", resp.Error.Message)
	}
	var result fileWriteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode file.write result: %v", err)
	}
	if time.Time(result.Mtime).IsZero() {
		t.Fatal("file.write result mtime = zero, want the post-write mtime")
	}
}

// TestServer_FileWrite_DeletedFile_ExpectedMtime_Conflict proves the
// deleted-on-disk case: expectedMtime set, file gone -> code
// "conflict_deleted", nothing written (the file must not be resurrected
// behind the client's back).
func TestServer_FileWrite_DeletedFile_ExpectedMtime_Conflict(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	mtime := time.Time(readMtime(t, ws, task.ID, "README.md"))
	if err := os.Remove(filepath.Join(*task.WorktreePath, "README.md")); err != nil {
		t.Fatalf("remove README.md: %v", err)
	}

	sendRequest(t, ws, "write", "file.write", map[string]any{
		"taskId": task.ID, "path": "README.md", "content": "resurrect\n",
		"expectedMtime": time.Time(mtime).Format(time.RFC3339Nano),
	})
	resp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("file.write(expectedMtime on deleted file) error = nil, want a conflict")
	}
	if resp.Error.Code != "conflict_deleted" {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, "conflict_deleted")
	}
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "README.md")); !os.IsNotExist(err) {
		t.Fatal("a conflicting write against a deleted file must not recreate it")
	}
}

// TestServer_FileWrite_NoExpectedMtime_Overwrites proves the omitted-
// expectedMtime wire shape is today's unconditional last-write-wins
// (params simply omit the field), including over a changed file -- the
// Overwrite action's force save.
func TestServer_FileWrite_NoExpectedMtime_Overwrites(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	mtime := time.Time(readMtime(t, ws, task.ID, "README.md"))
	newTime := mtime.Add(time.Second)
	if err := os.Chtimes(filepath.Join(*task.WorktreePath, "README.md"), newTime, newTime); err != nil {
		t.Fatalf("chtimes README.md: %v", err)
	}

	sendRequest(t, ws, "write", "file.write", map[string]any{
		"taskId": task.ID, "path": "README.md", "content": "forced save\n",
	})
	resp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("file.write(no expectedMtime) error = %v", resp.Error.Message)
	}

	disk, err := os.ReadFile(filepath.Join(*task.WorktreePath, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(disk) != "forced save\n" {
		t.Fatalf("disk content = %q, want %q", disk, "forced save\n")
	}
}

// readMtime issues a file.read over ws and returns its result's mtime --
// the shared prefix of every conditional-write test here.
func readMtime(t *testing.T, ws *websocket.Conn, taskID int64, path string) DateTime {
	t.Helper()
	sendRequest(t, ws, "read", "file.read", map[string]any{"taskId": taskID, "path": path})
	resp := readEnvelopeFor(t, ws, "read", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("file.read error = %v", resp.Error.Message)
	}
	var result fileReadResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode file.read result: %v", err)
	}
	return result.Mtime
}
