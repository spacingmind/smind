package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateToken_Creates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")

	token, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("LoadOrCreateToken() returned an empty token")
	}

	info, err := os.Stat(TokenPath(dir))
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", TokenPath(dir), err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 0600", perm)
	}
}

func TestLoadOrCreateToken_Reuses(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	second, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() second call error = %v", err)
	}
	if first != second {
		t.Errorf("token changed across calls: %q != %q, want identical", first, second)
	}
}
