package wsapi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/steward"
)

// The single review's seat (steward.twoAxis off) runs in the PR's head
// checkout, so a change could plant what a seat's CLI loads on its own. These
// tests plant it in the Lead's branch and read the seat's scratch tree.

// plantSingleSeatTree commits, in the Lead's worktree, files a change could
// plant: autoload files at any depth and in odd case, a symlinked autoload
// directory and a symlinked ordinary directory pointing outside the tree.
func (e *stewardHostEnv) plantSingleSeatTree(t *testing.T, outside string) string {
	t.Helper()
	wt := *e.seatTask.WorktreePath
	write := func(rel, body string) {
		full := filepath.Join(wt, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"CLAUDE.md", "claude.local.md", "AGENTS.md", "agents.md", ".mcp.json", ".claude/settings.json", ".codex/config.toml",
		"sub/CLAUDE.md", "Sub/Agents.MD", "sub/.claude/settings.json", "sub/.CLAUDE/x", "a/b/c/AGENTS.md", "GEMINI.md", ".cursorrules",
	} {
		write(rel, "planted")
	}
	write("main.go", "package x")
	write("docs/notes.md", "notes")
	for rel, target := range map[string]string{
		"nested-claude/.claude": outside, // a symlinked autoload directory
		"link":                  outside, // an ordinary directory symlinked outside: never descended into
	} {
		full := filepath.Join(wt, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, wt, "add", "-A", "-f", ".")
	gitIn(t, wt, "commit", "-qm", "plant")
	return gitIn(t, wt, "rev-parse", "HEAD")
}

func TestSingleSeat_ScratchTreeLosesPlantedAutoloads(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	outside := t.TempDir()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "settings.json", "keep.md"} {
		if err := os.WriteFile(filepath.Join(outside, name), []byte("OUTSIDE"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	head := e.plantSingleSeatTree(t, outside)
	r, err := e.start(t, head)
	if err != nil {
		t.Fatal(err)
	}
	row := e.waitSeatStarted(t, r.ReviewID)
	scratch := row.ScratchDir

	for _, gone := range []string{
		"CLAUDE.md", "claude.local.md", "AGENTS.md", "agents.md", ".mcp.json", ".claude", ".codex", "sub/CLAUDE.md", "Sub/Agents.MD",
		"sub/.claude", "sub/.CLAUDE", "a/b/c/AGENTS.md", "GEMINI.md", ".cursorrules", "nested-claude/.claude",
	} {
		if _, err := os.Lstat(filepath.Join(scratch, filepath.FromSlash(gone))); err == nil {
			t.Errorf("the single seat's tree still carries %s", gone)
		}
	}
	for _, kept := range []string{"main.go", "docs/notes.md", "scenario", "link"} {
		if _, err := os.Lstat(filepath.Join(scratch, filepath.FromSlash(kept))); err != nil {
			t.Errorf("the prune removed %s: %v", kept, err)
		}
	}
	// A symlink is removed or left, never followed: nothing outside was touched.
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "settings.json", "keep.md"} {
		if got, err := os.ReadFile(filepath.Join(outside, name)); err != nil || string(got) != "OUTSIDE" {
			t.Fatalf("outside/%s = %q, %v: the prune reached outside the scratch tree", name, got, err)
		}
	}
	e.endReview(t, r.ReviewID)
}

// TestSingleSeat_PruneFailureBlocksTheReview: fail closed. A prune that fails
// leaves a tree that may still carry planted autoloads, so no seat starts.
func TestSingleSeat_PruneFailureBlocksTheReview(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.host.prune = func(string, steward.PruneSpec) error { return errors.New("permission denied") }
	if _, err := e.start(t, e.head); err == nil {
		t.Fatal("a review started although its scratch tree could not be pruned")
	}
	rows, err := e.db.ListStewardReviews(e.seatTask.ID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("reviews = %+v, %v", rows, err)
	}
	if rows[0].Status != "blocked" || rows[0].BlockedReason != "crashed" || rows[0].RunID != "" || rows[0].TokenUsed {
		t.Fatalf("review = %+v, want blocked/crashed with no run", rows[0])
	}
}

// TestSingleSeat_PruneSpecIsTheAutoloadsOnly: the single seat has no standards
// copies, so its spec carries no exact paths, and it prunes what both passes do.
func TestSingleSeat_PruneSpecIsTheAutoloadsOnly(t *testing.T) {
	t.Parallel()
	spec := steward.SingleSeatPruneSpec()
	if len(spec.Paths) != 0 {
		t.Fatalf("single seat prune paths = %v, want none", spec.Paths)
	}
	want := steward.PruneSpecFor(steward.AxisSpec, nil)
	if len(spec.Names) != len(want.Names) {
		t.Fatalf("single seat names = %v, want the same as the spec pass's %v", spec.Names, want.Names)
	}
}
