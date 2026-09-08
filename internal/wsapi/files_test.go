package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/files"
)

// TestServer_FileList_Read_Write_RoundTrip proves the wire shape of
// file.list/file.read/file.write round-trips correctly end to end over a
// real WebSocket connection: list the worktree root, read a real file,
// write a change, then read again to confirm it landed.
func TestServer_FileList_Read_Write_RoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// newTestTask's worktree starts with README.md (see newTestRepo) plus
	// the ".git" file every `git worktree add` checkout has (pointing back
	// at the real repo's gitdir) -- file.list has no special-casing for it,
	// it's just another entry.
	sendRequest(t, ws, "list", "file.list", map[string]any{"taskId": task.ID})
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if listResp.Error != nil {
		t.Fatalf("file.list error = %v", listResp.Error.Message)
	}
	var entries []files.Entry
	if err := json.Unmarshal(listResp.Result, &entries); err != nil {
		t.Fatalf("decode file.list result: %v", err)
	}
	readmeEntry, ok := findEntry(entries, "README.md")
	if !ok || readmeEntry.IsDir {
		t.Fatalf("file.list result = %+v, want a README.md file entry among them", entries)
	}

	sendRequest(t, ws, "read", "file.read", map[string]any{"taskId": task.ID, "path": "README.md"})
	readResp := readEnvelopeFor(t, ws, "read", 5*time.Second)
	if readResp.Error != nil {
		t.Fatalf("file.read error = %v", readResp.Error.Message)
	}
	var readResult fileReadResult
	if err := json.Unmarshal(readResp.Result, &readResult); err != nil {
		t.Fatalf("decode file.read result: %v", err)
	}
	if readResult.Content != "hello\n" {
		t.Fatalf("file.read content = %q, want %q", readResult.Content, "hello\n")
	}

	sendRequest(t, ws, "write", "file.write", map[string]any{
		"taskId": task.ID, "path": "README.md", "content": "updated content\n",
	})
	writeResp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if writeResp.Error != nil {
		t.Fatalf("file.write error = %v", writeResp.Error.Message)
	}

	sendRequest(t, ws, "read2", "file.read", map[string]any{"taskId": task.ID, "path": "README.md"})
	readResp2 := readEnvelopeFor(t, ws, "read2", 5*time.Second)
	if readResp2.Error != nil {
		t.Fatalf("file.read (after write) error = %v", readResp2.Error.Message)
	}
	var readResult2 fileReadResult
	if err := json.Unmarshal(readResp2.Result, &readResult2); err != nil {
		t.Fatalf("decode file.read (after write) result: %v", err)
	}
	if readResult2.Content != "updated content\n" {
		t.Fatalf("file.read (after write) content = %q, want %q", readResult2.Content, "updated content\n")
	}

	// Also prove file.write can create a brand-new file, and file.list
	// then sees both it and README.md.
	sendRequest(t, ws, "writenew", "file.write", map[string]any{
		"taskId": task.ID, "path": "new.txt", "content": "brand new",
	})
	writeNewResp := readEnvelopeFor(t, ws, "writenew", 5*time.Second)
	if writeNewResp.Error != nil {
		t.Fatalf("file.write (new file) error = %v", writeNewResp.Error.Message)
	}

	sendRequest(t, ws, "list2", "file.list", map[string]any{"taskId": task.ID})
	listResp2 := readEnvelopeFor(t, ws, "list2", 5*time.Second)
	var entries2 []files.Entry
	if err := json.Unmarshal(listResp2.Result, &entries2); err != nil {
		t.Fatalf("decode file.list (after write) result: %v", err)
	}
	if _, ok := findEntry(entries2, "README.md"); !ok {
		t.Fatalf("file.list (after write) = %+v, missing README.md", entries2)
	}
	if _, ok := findEntry(entries2, "new.txt"); !ok {
		t.Fatalf("file.list (after write) = %+v, missing new.txt", entries2)
	}
}

func findEntry(entries []files.Entry, name string) (files.Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return files.Entry{}, false
}

// TestServer_FileList_Subdirectory proves file.list's path param navigates
// into a subdirectory of the worktree, not just the root.
func TestServer_FileList_Subdirectory(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	if err := os.Mkdir(filepath.Join(*task.WorktreePath, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "sub", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write nested.txt: %v", err)
	}

	sendRequest(t, ws, "list", "file.list", map[string]any{"taskId": task.ID, "path": "sub"})
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if listResp.Error != nil {
		t.Fatalf("file.list error = %v", listResp.Error.Message)
	}
	var entries []files.Entry
	if err := json.Unmarshal(listResp.Result, &entries); err != nil {
		t.Fatalf("decode file.list result: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "nested.txt" {
		t.Fatalf("file.list(sub) result = %+v, want a single nested.txt entry", entries)
	}
}

// TestServer_File_PathTraversal_Rejected proves the wire-level handlers
// actually reject a sandbox-escaping path -- not just internal/files'
// own unit tests -- for all three methods.
func TestServer_File_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "list", "file.list", map[string]any{"taskId": task.ID, "path": "../../../../../../etc"})
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if listResp.Error == nil {
		t.Fatal("file.list(../../../../../../etc) error = nil, want a rejection")
	}

	sendRequest(t, ws, "read", "file.read", map[string]any{"taskId": task.ID, "path": "../../../../../../etc/passwd"})
	readResp := readEnvelopeFor(t, ws, "read", 5*time.Second)
	if readResp.Error == nil {
		t.Fatal("file.read(../../../../../../etc/passwd) error = nil, want a rejection")
	}

	sendRequest(t, ws, "write", "file.write", map[string]any{
		"taskId": task.ID, "path": "/etc/smind-should-not-exist", "content": "pwned",
	})
	writeResp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if writeResp.Error == nil {
		t.Fatal("file.write(/etc/smind-should-not-exist) error = nil, want a rejection")
	}
	if _, err := os.Stat("/etc/smind-should-not-exist"); err == nil {
		t.Fatal("file.write escaped the sandbox and actually created /etc/smind-should-not-exist")
	}
}

// TestServer_File_ArchivedTask_Errors proves file.list errors clearly
// (rather than panicking or silently succeeding on a stale root) once a
// task's worktree directory has actually been removed from disk by
// archiving it -- ArchiveTask leaves the DB row's WorktreePath as-is (see
// store.ArchiveTask) but runs `git worktree remove --force`, so the
// directory files.List would resolve against is simply gone.
func TestServer_File_ArchivedTask_Errors(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	if _, err := wm.ArchiveTask(task.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	sendRequest(t, ws, "list", "file.list", map[string]any{"taskId": task.ID})
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if listResp.Error == nil {
		t.Fatal("file.list on an archived task's worktree: error = nil, want an error")
	}
}
