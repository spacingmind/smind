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

// A file created inside a RestrictDir directory is private to its owner
// without any call of its own: on Windows it inherits the directory's two
// entries instead of the parent's broader ACL.
func TestRestrictDir_ChildrenStayPrivate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := IsPrivateTree(root); err != nil || ok {
		t.Fatalf("IsPrivateTree before RestrictDir = %v, %v; want false", ok, err)
	}
	if err := RestrictDir(root); err != nil {
		t.Fatalf("RestrictDir() error = %v", err)
	}
	sub := filepath.Join(root, "data", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "opencode.db")
	if err := os.WriteFile(file, []byte("prompt text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := IsPrivateTree(root); err != nil || !ok {
		t.Fatalf("IsPrivateTree after RestrictDir with children = %v, %v; want true", ok, err)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "prompt text" {
		t.Fatalf("the owner can no longer read its own child: %q, %v", got, err)
	}
}

// One child that is not private makes the whole tree not private.
func TestIsPrivateTree_OneBroadChildFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RestrictDir(root); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "leaky")
	if err := os.WriteFile(child, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtimeIsWindows() {
		// Re-grant the Everyone group on this one file; Unix keeps 0700 on the root, which is the only gate there.
		if err := grantEveryone(child); err != nil {
			t.Fatal(err)
		}
		if ok, err := IsPrivateTree(root); err != nil || ok {
			t.Fatalf("IsPrivateTree with an Everyone-readable child = %v, %v; want false", ok, err)
		}
		return
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := IsPrivateTree(root); err != nil || ok {
		t.Fatalf("IsPrivateTree with a 0755 root = %v, %v; want false", ok, err)
	}
}
