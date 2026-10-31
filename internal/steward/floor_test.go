package steward

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStewardSeatFloor_ReadInScratchAllowedElseDeny(t *testing.T) {
	t.Parallel()

	scratch := t.TempDir()
	inside := filepath.Join(scratch, "diff.txt")
	if err := os.WriteFile(inside, []byte("diff"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tool := range SeatBuiltinTools {
		for _, p := range []string{inside, "diff.txt"} {
			allow, reason := SeatFloor(SeatRequest{Tool: tool, Paths: []string{p}, ScratchDir: scratch})
			if !allow || reason != "inside the scratch tree" {
				t.Errorf("SeatFloor(%s, %s) = %v, %q; want allow, \"inside the scratch tree\"", tool, p, allow, reason)
			}
		}
	}

	for _, tool := range []string{"Bash", "Edit", "Write", "WebFetch", ""} {
		allow, reason := SeatFloor(SeatRequest{Tool: tool, Paths: []string{inside}, ScratchDir: scratch})
		if allow {
			t.Errorf("SeatFloor(tool %q) = allow (%q), want deny", tool, reason)
		}
	}

	if allow, _ := SeatFloor(SeatRequest{Tool: "Read", ScratchDir: scratch}); allow {
		t.Error("SeatFloor(no paths) = allow, want deny")
	}
	if allow, _ := SeatFloor(SeatRequest{Tool: "Read", Paths: []string{"../outside.txt"}, ScratchDir: scratch}); allow {
		t.Error("SeatFloor(../ escape) = allow, want deny")
	}
	if allow, _ := SeatFloor(SeatRequest{Tool: "Read", Paths: []string{scratch + "x"}, ScratchDir: scratch}); allow {
		t.Error("SeatFloor(sibling dir sharing the prefix) = allow, want deny")
	}
	if allow, _ := SeatFloor(SeatRequest{Tool: "Read", Paths: []string{inside}, ScratchDir: ""}); allow {
		t.Error("SeatFloor(blank ScratchDir) = allow, want deny")
	}

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if allow, _ := SeatFloor(SeatRequest{Tool: "Read", Paths: []string{outsideFile}, ScratchDir: scratch}); allow {
		t.Error("SeatFloor(absolute path outside) = allow, want deny")
	}

	link := filepath.Join(scratch, "link")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	if allow, _ := SeatFloor(SeatRequest{Tool: "Read", Paths: []string{link}, ScratchDir: scratch}); allow {
		t.Error("SeatFloor(symlink inside scratch pointing outside) = allow, want deny")
	}
}
