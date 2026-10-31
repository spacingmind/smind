//go:build windows

package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// unsafeForms are Windows path spellings that must be refused before any
// filesystem access. The server address is unroutable on purpose: if a
// form reached realpath it would try the network instead of failing fast.
var unsafeForms = []struct{ name, path string }{
	{"UNC", `\\10.255.255.1\share\f.txt`},
	{"UNC forward slashes", `//10.255.255.1/share/f.txt`},
	{"verbatim UNC", `\\?\UNC\10.255.255.1\share\f.txt`},
	{"device namespace", `\\.\pipe\smind-test`},
	{"drive-relative", `C:f.txt`},
}

// TestResolveInRoot_RefusesUnsafeFormsAtEveryEntryPoint: the guard lives in
// resolveInRoot, so List, Read, Write and Resolve (the old file.* methods and
// file refs) refuse a UNC/device/drive-relative path exactly as Stat and
// ReadRange do, with InvalidParamsError and without touching the network.
func TestResolveInRoot_RefusesUnsafeFormsAtEveryEntryPoint(t *testing.T) {
	root := newTestRoot(t)
	entries := map[string]func(path string) error{
		"List":  func(p string) error { _, err := List(root, p); return err },
		"Read":  func(p string) error { _, _, err := Read(root, p); return err },
		"Write": func(p string) error { _, err := Write(root, p, "x", nil); return err },
		"Resolve": func(p string) error {
			_, err := Resolve(root, p)
			return err
		},
		"Stat":      func(p string) error { _, err := Stat(root, p); return err },
		"ReadRange": func(p string) error { _, err := ReadRange(root, p, 0, 1, testLimits, nil); return err },
	}
	for entry, call := range entries {
		for _, f := range unsafeForms {
			done := make(chan error, 1)
			go func() { done <- call(f.path) }()
			select {
			case err := <-done:
				if !asInvalid(err) {
					t.Errorf("%s(%s %q): err = %v, want the guard's InvalidParamsError", entry, f.name, f.path, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s(%s %q): still running after 5s (reached the network?)", entry, f.name, f.path)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "f.txt")); err == nil {
		t.Error("Write with a drive-relative path created a file under the root")
	}
}

// TestResolveInRoot_LocalVerbatimStillWorks: \\?\C:\... names a local path and
// is not refused; containment still applies to it.
func TestResolveInRoot_LocalVerbatimStillWorks(t *testing.T) {
	root := newTestRoot(t)
	verbatim := `\\?\` + filepath.Join(root, "hello.txt")
	got, _, err := Read(root, verbatim)
	if err != nil || got != "hello\n" {
		t.Errorf("Read(%q) = %q, %v, want the file", verbatim, got, err)
	}
	if _, _, err := Read(root, `\\?\C:\Windows\win.ini`); err == nil {
		t.Error("verbatim path outside the root: want refusal")
	}
}
