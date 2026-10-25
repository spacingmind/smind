package workspace

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func TestWorkspaceCommitMessageStyle_ValidatesLength(t *testing.T) {
	repo := newDetectRepo(t, "main", false)
	dbPath := filepath.Join(t.TempDir(), "smind.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	m := New(s)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly 1000 runes (multi-byte) is fine; 1001 is rejected.
	ok := strings.Repeat("é", 1000)
	if _, err := m.SetCommitMessageStyle(w.ID, ok); err != nil {
		t.Fatalf("1000 runes: %v", err)
	}
	_, err = m.SetCommitMessageStyle(w.ID, ok+"x")
	if err == nil || err.Error() != "commit message style is too long (max 1000 characters)" {
		t.Fatalf("1001 runes error = %v", err)
	}
	if got := m.CommitMessageStyle(w.ID); got != ok {
		t.Fatalf("a rejected style changed the stored one (len %d)", len(got))
	}

	for _, bad := range []string{"a\x00b", "a\x1bb", "a\rb", "a\x7fb"} {
		if _, err := m.SetCommitMessageStyle(w.ID, bad); err == nil {
			t.Fatalf("SetCommitMessageStyle(%q) error = nil, want rejection", bad)
		}
	}

	// Newlines and tabs are allowed; the text is trimmed.
	ws, err := m.SetCommitMessageStyle(w.ID, "  \n imperative\n\tmood \n ")
	if err != nil {
		t.Fatal(err)
	}
	if want := "imperative\n\tmood"; ws.CommitMessageStyle != want || m.CommitMessageStyle(w.ID) != want {
		t.Fatalf("style = %q / %q, want %q", ws.CommitMessageStyle, m.CommitMessageStyle(w.ID), want)
	}

	// Persisted across a reopen.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	m = New(s)
	if got := m.CommitMessageStyle(w.ID); got != "imperative\n\tmood" {
		t.Fatalf("after reopen style = %q", got)
	}
	list, err := m.ListWorkspaces()
	if err != nil || len(list) != 1 || list[0].CommitMessageStyle != "imperative\n\tmood" {
		t.Fatalf("ListWorkspaces = %+v, %v", list, err)
	}

	// Empty (or whitespace-only) clears.
	if _, err := m.SetCommitMessageStyle(w.ID, "  "); err != nil {
		t.Fatal(err)
	}
	if got := m.CommitMessageStyle(w.ID); got != "" {
		t.Fatalf("after clear style = %q", got)
	}
	if _, err := m.SetCommitMessageStyle(9999, "x"); err == nil {
		t.Fatal("unknown workspace: error = nil")
	}
}
