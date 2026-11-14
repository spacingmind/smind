package crash

import (
	"path/filepath"
	"testing"
)

// crash-main-panic-recorded-then-repanics: the main goroutine's helper
// records the panic and then panics again, so the process still ends.
func TestCrashRecordAndRepanic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	setDefault(t, testConfig(dir))
	var repanicked any
	func() {
		defer func() { repanicked = recover() }()
		defer RecordAndRepanic("main")
		panic("fatal")
	}()
	if repanicked != "fatal" {
		t.Fatalf("re-panic value = %v, want the original value", repanicked)
	}
	if files := crashFiles(t, dir); len(files) != 1 {
		t.Fatalf("reports = %d, want 1", len(files))
	}
	// no panic: nothing recorded, nothing re-raised
	func() {
		defer RecordAndRepanic("main")
	}()
	if files := crashFiles(t, dir); len(files) != 1 {
		t.Fatalf("a clean return recorded a report: %d", len(files))
	}
}
