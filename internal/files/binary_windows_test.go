//go:build windows

package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFileReadRange_WindowsPathForms (S17): separators, \\?\ verbatim,
// UNC / device / drive-relative refusal, case-insensitive extensions.
func TestFileReadRange_WindowsPathForms(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, `sub\Doc.PDF`, []byte("%PDF-1.4 body"))

	ok := []struct{ name, path string }{
		{"backslash", `sub\Doc.PDF`},
		{"forward slash", "sub/Doc.PDF"},
		{"mixed", `sub/.\Doc.PDF`},
		{"verbatim absolute", `\\?\` + filepath.Join(root, "sub", "Doc.PDF")},
		{"absolute", filepath.Join(root, "sub", "Doc.PDF")},
		{"other case on disk name", `sub\doc.pdf`},
	}
	for _, c := range ok {
		r, err := ReadRange(root, c.path, 0, 100, testLimits, nil)
		if err != nil || string(r.Data) != "%PDF-1.4 body" {
			t.Errorf("%s (%q): data=%q err=%v, want the file", c.name, c.path, r.Data, err)
		}
	}
	st, err := Stat(root, `sub\Doc.PDF`)
	if err != nil || st.Ext != ".pdf" || st.Kind != KindPDF || st.Mismatch {
		t.Errorf("upper-case .PDF: %+v err=%v, want ext .pdf kind pdf no mismatch", st, err)
	}

	refused := []struct {
		name, path string
		guard      bool // refused by the path-form guard (invalid_params), before any filesystem access
	}{
		{"UNC", `\\server\share\x.pdf`, true},
		{"UNC forward slashes", `//server/share/x.pdf`, true},
		{"verbatim UNC", `\\?\UNC\server\share\x.pdf`, true},
		{"device namespace", `\\.\C:\Windows\win.ini`, true},
		{"drive-relative", `C:x.pdf`, true},
		{"escape with backslashes", `..\outside.pdf`, false},
		{"escape verbatim other path", `\\?\C:\Windows\win.ini`, false},
		{"reserved device name", `NUL`, false},
		{"reserved device name in subdir", `sub\CON`, false},
	}
	for _, c := range refused {
		done := make(chan error, 1)
		go func() { _, err := ReadRange(root, c.path, 0, 10, testLimits, nil); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s (%q): want refusal", c.name, c.path)
			} else if c.guard && !asInvalid(err) {
				t.Errorf("%s (%q): err = %v, want the path-form guard's InvalidParamsError", c.name, c.path, err)
			} else if strings.Contains(err.Error(), root) {
				t.Errorf("%s: error leaks an absolute path: %v", c.name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s (%q): blocked", c.name, c.path)
		}
	}
}

func TestIsUNCOrDevicePath(t *testing.T) {
	for p, want := range map[string]bool{
		`\\server\share`:       true,
		`//server/share`:       true,
		`\\?\UNC\server\share`: true,
		`\\?\unc\server\share`: true,
		`\\.\pipe\x`:           true,
		`\\?\C:\work\x`:        false,
		`C:\work\x`:            false,
		`sub\x`:                false,
		`\\?\UNCLE\x`:          false,
		``:                     false,
	} {
		if got := isUNCOrDevicePath(p); got != want {
			t.Errorf("isUNCOrDevicePath(%q) = %v, want %v", p, got, want)
		}
	}
}
