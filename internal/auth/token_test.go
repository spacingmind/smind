package auth

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/securefile"
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

	// Owner-only: 0600 on Unix, a protected owner+SYSTEM DACL on Windows
	// (ADR-0028 D3) -- the parity plan's TestTokenFile_OwnerOnlyACL_Windows.
	private, err := securefile.IsPrivate(TokenPath(dir))
	if err != nil {
		t.Fatalf("IsPrivate(%s) error = %v", TokenPath(dir), err)
	}
	if !private {
		t.Errorf("token file %s is readable by others, want owner-only", TokenPath(dir))
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
