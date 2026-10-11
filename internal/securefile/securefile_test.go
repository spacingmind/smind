package securefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile_IsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := WriteFile(path, []byte("s3cret")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	private, err := IsPrivate(path)
	if err != nil {
		t.Fatalf("IsPrivate() error = %v", err)
	}
	if !private {
		t.Fatal("IsPrivate() = false right after WriteFile, want true")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "s3cret" {
		t.Fatalf("ReadFile() = %q, %v; want the written content readable by its owner", got, err)
	}
}

// A file created the ordinary way (inherited ACL on Windows, 0644 on Unix)
// is not private until Restrict runs -- proving IsPrivate actually checks
// something and Restrict actually changes it.
func TestRestrict_TightensExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keypair.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if private, err := IsPrivate(path); err != nil || private {
		t.Fatalf("IsPrivate() before Restrict = %v, %v; want false", private, err)
	}
	if err := Restrict(path); err != nil {
		t.Fatalf("Restrict() error = %v", err)
	}
	if private, err := IsPrivate(path); err != nil || !private {
		t.Fatalf("IsPrivate() after Restrict = %v, %v; want true", private, err)
	}
}

// The owner keeps full access: rewriting an already-restricted file works.
func TestWriteFile_RewritesRestrictedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	for _, content := range []string{"first", "second"} {
		if err := WriteFile(path, []byte(content)); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", content, err)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}
}
