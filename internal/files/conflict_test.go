package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setMtime forces a file's mtime to a distinct value, simulating an
// out-of-band rewrite (an agent) between a client's Read and its
// conditional Write -- a plain write can land within the same filesystem
// timestamp tick, which would make the mtimes compare equal.
func setMtime(t *testing.T, root, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(filepath.Join(root, path), mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// TestRead_ReturnsMtime proves Read hands back the file's current mtime --
// the value a conditional Write echoes as expectedMtime.
func TestRead_ReturnsMtime(t *testing.T) {
	root := newTestRoot(t)

	want := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	setMtime(t, root, "hello.txt", want)

	_, mtime, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !mtime.Equal(want) {
		t.Fatalf("Read() mtime = %v, want %v", mtime, want)
	}
}

// TestRead_MissingFile_TypedError proves a missing file surfaces as
// *MissingError (machine-readable), not just a wrapped OS error.
func TestRead_MissingFile_TypedError(t *testing.T) {
	root := newTestRoot(t)

	_, _, err := Read(root, "gone.txt")
	if err == nil {
		t.Fatal("Read(gone.txt) error = nil, want an error")
	}
	me, ok := err.(*MissingError)
	if !ok {
		t.Fatalf("Read(gone.txt) error = %T (%v), want *MissingError", err, err)
	}
	if me.RPCCode() != "not_found" {
		t.Fatalf("MissingError.RPCCode() = %q, want %q", me.RPCCode(), "not_found")
	}
}

// TestWrite_ExpectedMtimeMatch_Succeeds proves the conditional write's
// happy path: same mtime as the Read that produced it, write lands and
// returns the new mtime for chaining into the next conditional write.
func TestWrite_ExpectedMtimeMatch_Succeeds(t *testing.T) {
	root := newTestRoot(t)

	_, mtime, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	newMtime, err := Write(root, "hello.txt", "replaced", &mtime)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !newMtime.After(mtime) && !newMtime.Equal(mtime) {
		t.Fatalf("Write() mtime = %v, want >= read-time %v", newMtime, mtime)
	}
	content, _, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() after Write() error = %v", err)
	}
	if content != "replaced" {
		t.Fatalf("content = %q, want %q", content, "replaced")
	}

	// The returned mtime chains: a second conditional write with it succeeds.
	if _, err := Write(root, "hello.txt", "replaced again", &newMtime); err != nil {
		t.Fatalf("Write(chained expectedMtime) error = %v", err)
	}
}

// TestWrite_StaleExpectedMtime_Conflict_Unchanged proves the rejected-if-
// changed core: the file's mtime moved since the Read (an agent rewrote
// it), the conditional write is refused, nothing is written, and the error
// is the typed *ConflictError with the "conflict" RPC code.
func TestWrite_StaleExpectedMtime_Conflict_Unchanged(t *testing.T) {
	root := newTestRoot(t)

	_, mtime, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	setMtime(t, root, "hello.txt", mtime.Add(time.Second))

	_, err = Write(root, "hello.txt", "clobbered", &mtime)
	if err == nil {
		t.Fatal("Write(stale expectedMtime) error = nil, want a conflict")
	}
	ce, ok := err.(*ConflictError)
	if !ok {
		t.Fatalf("Write(stale expectedMtime) error = %T (%v), want *ConflictError", err, err)
	}
	if ce.Deleted {
		t.Fatalf("ConflictError.Deleted = true, want false (changed, not deleted)")
	}
	if ce.RPCCode() != "conflict" {
		t.Fatalf("ConflictError.RPCCode() = %q, want %q", ce.RPCCode(), "conflict")
	}

	// No write happened: disk still holds the agent's content.
	content, _, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() after refused Write() error = %v", err)
	}
	if content != "hello\n" {
		t.Fatalf("content = %q, want unchanged %q (a conflicting write must not land)", content, "hello\n")
	}
}

// TestWrite_DeletedFile_WithExpectedMtime_Conflict proves the deleted-on-
// disk case: the client holds an mtime from a Read that succeeded, the
// file is now gone, the conditional write is refused with the deleted
// flavor of the conflict error.
func TestWrite_DeletedFile_WithExpectedMtime_Conflict(t *testing.T) {
	root := newTestRoot(t)

	_, mtime, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "hello.txt")); err != nil {
		t.Fatalf("remove hello.txt: %v", err)
	}

	_, err = Write(root, "hello.txt", "resurrected", &mtime)
	if err == nil {
		t.Fatal("Write(expectedMtime on deleted file) error = nil, want a conflict")
	}
	ce, ok := err.(*ConflictError)
	if !ok {
		t.Fatalf("error = %T (%v), want *ConflictError", err, err)
	}
	if !ce.Deleted {
		t.Fatal("ConflictError.Deleted = false, want true (file deleted on disk)")
	}
	if ce.RPCCode() != "conflict_deleted" {
		t.Fatalf("ConflictError.RPCCode() = %q, want %q", ce.RPCCode(), "conflict_deleted")
	}
	if _, err := os.Stat(filepath.Join(root, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("a conflicting write against a deleted file must not recreate it")
	}
}

// TestWrite_NilExpectedMtime_LastWriteWins proves the omitted-expectedMtime
// path is exactly today's unconditional behavior, including over a file
// whose mtime moved (the Overwrite action's force write) and to a file
// that no longer exists (recreating it).
func TestWrite_NilExpectedMtime_LastWriteWins(t *testing.T) {
	root := newTestRoot(t)

	_, mtime, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	setMtime(t, root, "hello.txt", mtime.Add(time.Second))
	if _, err := Write(root, "hello.txt", "forced", nil); err != nil {
		t.Fatalf("Write(nil expectedMtime over changed file) error = %v", err)
	}
	content, _, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() after forced Write() error = %v", err)
	}
	if content != "forced" {
		t.Fatalf("content = %q, want %q", content, "forced")
	}

	if err := os.Remove(filepath.Join(root, "hello.txt")); err != nil {
		t.Fatalf("remove hello.txt: %v", err)
	}
	if _, err := Write(root, "hello.txt", "recreated", nil); err != nil {
		t.Fatalf("Write(nil expectedMtime recreating deleted file) error = %v", err)
	}
}
