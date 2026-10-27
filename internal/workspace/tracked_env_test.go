package workspace

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestTrackedEnvFiles(t *testing.T) {
	dir := newTestRepo(t)
	for _, n := range []string{".env", "app/.env.local", ".env.example", "env.txt"} {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("K=v\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitT(t, dir, "add", ".env", "app/.env.local", ".env.example", "env.txt")
	got, err := TrackedEnvFiles(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{".env", ".env.example", "app/.env.local"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if _, err := TrackedEnvFiles(context.Background(), t.TempDir()); err == nil {
		t.Fatal("non-repo dir: want an error")
	}
}
