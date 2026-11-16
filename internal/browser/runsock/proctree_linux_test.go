//go:build linux

package runsock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProcfsTreeFixture(t *testing.T) {
	dir := t.TempDir()
	writeStat := func(pid, line string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, pid, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeStat("4300", "4300 (init) S 1 4300 4300 0 -1 0 0 0 0 0 0 0 0 0 0 0 1 0 111 0 0 0 0 0")
	writeStat("4321", "4321 (chrome) S 4300 4321 4321 0 -1 4194624 12345 0 12 3 45 67 8 9 10 0 1 20 987654321 123456789 0 0 0 0 0")

	tree := newProcfsTree(dir)
	info, err := tree.Info(4321)
	if err != nil {
		t.Fatalf("Info(4321) error = %v, want nil", err)
	}
	if info.PPID != 4300 || info.Start != 987654321 {
		t.Errorf("Info(4321) = %+v, want {PPID:4300 Start:987654321}", info)
	}

	if _, err := tree.Info(4322); !errors.Is(err, ErrNoProcess) {
		t.Errorf("Info(missing pid) error = %v, want ErrNoProcess", err)
	}
	if _, err := tree.Info(0); !errors.Is(err, ErrNoProcess) {
		t.Errorf("Info(0) error = %v, want ErrNoProcess", err)
	}
}
