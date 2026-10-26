package sandbox

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("settings are not valid JSON: %v", err)
	}
	return m
}

func fsOf(m map[string]any) map[string]any {
	return m["sandbox"].(map[string]any)["filesystem"].(map[string]any)
}

func TestSandboxSettings_Table(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe sandbox is POSIX-only (there is no measured sandbox on Windows, see launcher_windows.go); its settings take /-rooted paths")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realArm := filepath.Join(root, "real-arm")
	if err := os.Mkdir(realArm, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link-arm")
	if err := os.Symlink(realArm, link); err != nil {
		t.Fatal(err)
	}
	const proj = "/nonexistent-p0/proj"

	cases := []struct {
		name      string
		arm, proj string
		wantArm   string // "" = error expected
	}{
		{"good", "/nonexistent-p0/arm", proj, "/nonexistent-p0/arm"},
		{"nonexistent absolute tail", filepath.Join(root, "not", "yet", "arm"), proj, filepath.Join(root, "not", "yet", "arm")},
		{"symlinked arm dir", link, proj, realArm},
		{"no projection", "/nonexistent-p0/arm", "", "/nonexistent-p0/arm"},
		{"relative arm", "rel/arm", proj, ""},
		{"dotdot in arm", "/nonexistent-p0/../arm", proj, ""},
		{"glob in arm", "/nonexistent-p0/a*m", proj, ""},
		{"control char in arm", "/nonexistent-p0/a\x01m", proj, ""},
		{"newline in arm", "/nonexistent-p0/a\nm", proj, ""},
		{"empty arm", "", proj, ""},
		{"quote in arm", "/nonexistent-p0/a\"m", proj, ""},
		{"dollar in arm", "/nonexistent-p0/a$m", proj, ""},
		{"semicolon in arm", "/nonexistent-p0/a;m", proj, ""},
		{"tilde in arm", "/nonexistent-p0/~m", proj, ""},
		{"ampersand in arm", "/nonexistent-p0/a&m", proj, ""},
		{"angle in arm", "/nonexistent-p0/a<m", proj, ""},
		{"pipe in arm", "/nonexistent-p0/a|m", proj, ""},
		{"backtick in arm", "/nonexistent-p0/a`m", proj, ""},
		{"ampersand in projection", "/nonexistent-p0/arm", "/nonexistent-p0/p&roj", ""},
		{"relative projection", "/nonexistent-p0/arm", "rel/proj", ""},
		{"glob in projection", "/nonexistent-p0/arm", "/nonexistent-p0/pr?oj", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := Settings(tc.arm, tc.proj)
			if tc.wantArm == "" {
				if !errors.Is(err, ErrBadPath) || raw != "" {
					t.Fatalf("want ErrBadPath and no settings, got err=%v raw=%s", err, raw)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, link) {
				t.Errorf("symlink path leaked into settings: %s", raw)
			}
			m := decode(t, raw)
			sb := m["sandbox"].(map[string]any)
			for k, want := range map[string]any{"enabled": true, "failIfUnavailable": true, "allowUnsandboxedCommands": false, "autoAllowBashIfSandboxed": true} {
				if sb[k] != want {
					t.Errorf("sandbox.%s = %v, want %v", k, sb[k], want)
				}
			}
			if _, ok := sb["excludedCommands"]; ok {
				t.Error("sandbox.excludedCommands must not be set")
			}
			if !reflect.DeepEqual(sb["network"], map[string]any{"allowedDomains": []any{}, "strictAllowlist": true, "allowLocalBinding": false}) {
				t.Errorf("network = %v", sb["network"])
			}
			fs := fsOf(m)
			if !reflect.DeepEqual(fs["denyRead"], []any{"/"}) {
				t.Errorf("denyRead = %v, want exactly [/]", fs["denyRead"])
			}
			if !reflect.DeepEqual(fs["allowWrite"], []any{tc.wantArm}) {
				t.Errorf("allowWrite = %v, want [%s]", fs["allowWrite"], tc.wantArm)
			}
			allow := map[string]bool{}
			for _, p := range fs["allowRead"].([]any) {
				allow[p.(string)] = true
			}
			wantAllow := append([]string{tc.wantArm}, SystemSymlinkNodes...)
			for _, p := range SystemReadPaths() {
				c, _ := canonPath(p)
				wantAllow = append(wantAllow, c)
			}
			if tc.proj != "" {
				wantAllow = append(wantAllow, tc.proj)
			}
			for _, p := range wantAllow {
				if !allow[p] {
					t.Errorf("allowRead lacks %s: %v", p, fs["allowRead"])
				}
			}
			if allow["/"] || len(allow) != len(wantAllow) {
				t.Errorf("allowRead has unexpected entries: %v", fs["allowRead"])
			}
			if tc.proj == "" {
				if _, ok := fs["denyWrite"]; ok {
					t.Error("denyWrite emitted without a projection")
				}
				if _, ok := m["permissions"]; ok {
					t.Error("permissions emitted without a projection")
				}
				return
			}
			if !reflect.DeepEqual(fs["denyWrite"], []any{tc.proj}) {
				t.Errorf("denyWrite = %v", fs["denyWrite"])
			}
			wantDeny := []any{"Edit(//nonexistent-p0/proj/**)", "Write(//nonexistent-p0/proj/**)", "NotebookEdit(//nonexistent-p0/proj/**)"}
			if got := m["permissions"].(map[string]any)["deny"]; !reflect.DeepEqual(got, wantDeny) {
				t.Errorf("permissions.deny = %v", got)
			}
		})
	}
}

func TestSandboxSettings_SystemPathsIncludeClaudeTmp(t *testing.T) {
	var found bool
	for _, p := range SystemReadPaths() {
		if strings.HasPrefix(p, "/private/tmp/claude-") {
			found = true
		}
	}
	if !found {
		t.Error("the /private/tmp/claude-<uid> entry is missing")
	}
}

func TestSandboxSettings_HomebrewIsNarrowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe sandbox is POSIX-only (there is no measured sandbox on Windows, see launcher_windows.go); its settings take /-rooted paths")
	}
	paths := SystemReadPaths()
	have := map[string]bool{}
	for _, p := range paths {
		have[p] = true
	}
	if have["/opt/homebrew"] {
		t.Error("the whole /opt/homebrew is readable")
	}
	for _, sub := range []string{"bin", "Cellar", "opt", "lib", "share", "include", "Library"} {
		if !have["/opt/homebrew/"+sub] {
			t.Errorf("/opt/homebrew/%s missing", sub)
		}
	}
	raw, err := Settings("/nonexistent-p0/arm", "")
	if err != nil {
		t.Fatal(err)
	}
	allow := fsOf(decode(t, raw))["allowRead"].([]any)
	for _, a := range allow {
		if s, _ := a.(string); s == "/opt/homebrew" {
			t.Error("allowRead holds the whole /opt/homebrew")
		}
	}
}
