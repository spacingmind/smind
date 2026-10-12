package claudecli

import (
	"errors"
	"strings"
	"testing"
)

// fakeFS is a map-backed filesystem for Resolve.
type fakeFS map[string]string // path -> content ("" for plain files)

func (f fakeFS) exists(p string) bool { _, ok := f[p]; return ok }

func (f fakeFS) read(p string) ([]byte, error) {
	if c, ok := f[p]; ok {
		return []byte(c), nil
	}
	return nil, errors.New("not found")
}

func lookup(path string) func(string) (string, error) {
	return func(string) (string, error) {
		if path == "" {
			return "", errors.New("not found")
		}
		return path, nil
	}
}

// The shim npm really writes for @anthropic-ai/claude-code (read from a
// machine with the npm install).
const npmShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\"%dp0%\\node_modules\\@anthropic-ai\\claude-code\\bin\\claude.exe\"   %*\r\n"

func TestResolve_ExeOnPath(t *testing.T) {
	got, err := Resolve(lookup(`C:\Users\me\.local\bin\claude.exe`), fakeFS{}.read, fakeFS{}.exists, `C:\Users\me`)
	if err != nil || got != `C:\Users\me\.local\bin\claude.exe` {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

func TestResolve_NpmShimParsedToRealExe(t *testing.T) {
	shim := `C:\Users\me\AppData\Roaming\npm\claude.cmd`
	exe := `C:\Users\me\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\bin\claude.exe`
	fs := fakeFS{shim: npmShim, exe: ""}
	got, err := Resolve(lookup(shim), fs.read, fs.exists, `C:\Users\me`)
	if err != nil || got != exe {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, exe)
	}
}

func TestResolve_ShimPathWithSpaces(t *testing.T) {
	shim := `C:\Program Files\nodejs\global\claude.cmd`
	exe := `C:\Program Files\nodejs\global\node_modules\@anthropic-ai\claude-code\bin\claude.exe`
	fs := fakeFS{shim: npmShim, exe: ""}
	got, err := Resolve(lookup(shim), fs.read, fs.exists, "")
	if err != nil || got != exe {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, exe)
	}
}

func TestResolve_UnparseableShimFallsBackToNpmLayout(t *testing.T) {
	shim := `C:\npm\claude.cmd`
	exe := `C:\npm\node_modules\@anthropic-ai\claude-code\bin\claude.exe`
	// An older shim that runs node on cli.js: nothing to parse.
	fs := fakeFS{shim: "@echo off\r\nnode \"%~dp0\\node_modules\\@anthropic-ai\\claude-code\\cli.js\" %*\r\n", exe: ""}
	got, err := Resolve(lookup(shim), fs.read, fs.exists, "")
	if err != nil || got != exe {
		t.Fatalf("Resolve = %q, %v; want the layout fallback %q", got, err, exe)
	}
}

func TestResolve_UnreadableShimStillProbesLayout(t *testing.T) {
	shim := `C:\npm\claude.bat`
	exe := `C:\npm\node_modules\@anthropic-ai\claude-code\bin\claude.exe`
	fs := fakeFS{exe: ""} // shim itself unreadable
	got, err := Resolve(lookup(shim), fs.read, fs.exists, "")
	if err != nil || got != exe {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, exe)
	}
}

func TestResolve_NativeInstallerWhenNotOnPath(t *testing.T) {
	exe := `C:\Users\me\.local\bin\claude.exe`
	fs := fakeFS{exe: ""}
	got, err := Resolve(lookup(""), fs.read, fs.exists, `C:\Users\me`)
	if err != nil || got != exe {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, exe)
	}
}

func TestResolve_NeverReturnsAShim(t *testing.T) {
	// A shim with nothing behind it must be an error, not "run the .cmd".
	shim := `C:\npm\claude.cmd`
	got, err := Resolve(lookup(shim), fakeFS{shim: npmShim}.read, fakeFS{shim: npmShim}.exists, "")
	if err == nil {
		t.Fatalf("Resolve = %q, want an error", got)
	}
	for _, want := range []string{"native claude.exe", shim, "install Claude Code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestResolve_NotFoundAnywhereIsActionable(t *testing.T) {
	_, err := Resolve(lookup(""), fakeFS{}.read, fakeFS{}.exists, `C:\Users\me`)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{`C:\Users\me\.local\bin\claude.exe`, "npm install -g @anthropic-ai/claude-code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestResolve_RelativeExeOnPathIsRejected(t *testing.T) {
	if _, err := Resolve(lookup(`.\claude.exe`), fakeFS{}.read, fakeFS{}.exists, ""); err == nil {
		t.Fatal("a relative path must not be returned")
	}
}

func TestValidateTarget(t *testing.T) {
	for _, ok := range []string{`C:\a\claude.exe`, `c:/a/CLAUDE.EXE`, `\\server\share\claude.exe`} {
		if err := ValidateTarget(ok); err != nil {
			t.Errorf("ValidateTarget(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{``, `claude.exe`, `.\claude.exe`, `C:\Windows\System32\cmd.exe`, `C:\a\claude.cmd`, `C:\a\claude.exe.bat`, `C:\a\notclaude.exe`, `\claude.exe`} {
		if err := ValidateTarget(bad); !errors.Is(err, ErrNotAClaudeExe) {
			t.Errorf("ValidateTarget(%q) = %v, want ErrNotAClaudeExe", bad, err)
		}
	}
}

func TestWithoutEnv(t *testing.T) {
	in := []string{"A=1", "smind_claude_launch=C:\\x\\claude.exe", "SMIND_CLAUDE_LAUNCH_OTHER=keep", "B=2"}
	got := WithoutEnv(in, LaunchEnv)
	want := []string{"A=1", "SMIND_CLAUDE_LAUNCH_OTHER=keep", "B=2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("WithoutEnv = %v, want %v", got, want)
	}
	if len(in) != 4 {
		t.Fatalf("input was modified: %v", in)
	}
}
