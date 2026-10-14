//go:build windows

package selfupdate

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestHelperSleepProcess is the body of the "running smind.exe" below; it
// does nothing in a normal test run.
func TestHelperSleepProcess(t *testing.T) {
	if os.Getenv("SMIND_SLEEP_HELPER") != "1" {
		t.Skip("helper process")
	}
	time.Sleep(time.Minute)
}

// TestUpdateWindowsRenameAsideAndCleanup replaces a smind.exe that is really
// running: Windows refuses to delete or overwrite it but allows the rename
// that Swap relies on, keeps the running image as smind.exe.old, and only
// CleanupStale can delete it once the process is gone.
func TestUpdateWindowsRenameAsideAndCleanup(t *testing.T) {
	if !RenameAside {
		t.Fatal("RenameAside must be on for Windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "smind.exe")
	copyTestBinary(t, exe)

	child := exec.Command(exe, "-test.run=^TestHelperSleepProcess$")
	child.Env = append(os.Environ(), "SMIND_SLEEP_HELPER=1")
	if err := child.Start(); err != nil {
		t.Fatalf("start the running smind.exe: %v", err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	time.Sleep(300 * time.Millisecond) // let the image map

	if err := os.Remove(exe); err == nil {
		t.Fatal("expected Windows to refuse deleting a running exe; the rename-aside premise is wrong")
	}

	staged := filepath.Join(dir, ".smind.new.exe")
	if err := os.WriteFile(staged, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Swap(exe, staged, true); err != nil {
		t.Fatalf("Swap over a running exe: %v", err)
	}
	if got := read(t, exe); got != "new binary" {
		t.Fatalf("exe = %q after Swap", got)
	}
	Discard(exe) // the running image cannot be deleted yet
	if _, err := os.Stat(OldPath(exe)); err != nil {
		t.Fatalf("the running exe's .old must survive Discard while it runs: %v", err)
	}

	_ = child.Process.Kill()
	_ = child.Wait()
	CleanupStale(exe)
	if _, err := os.Stat(OldPath(exe)); !os.IsNotExist(err) {
		t.Fatalf(".old should be gone after the process exited and CleanupStale ran, stat err = %v", err)
	}
}

func copyTestBinary(t *testing.T, dst string) {
	t.Helper()
	in, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
