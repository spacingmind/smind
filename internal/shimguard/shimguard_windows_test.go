//go:build windows

package shimguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shimOnPath puts a trivial .cmd shim named name on PATH and returns nothing:
// Validate only resolves and inspects it, it never runs it.
func shimOnPath(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name+".cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestValidate_RefusesMetacharactersOnBatchShims(t *testing.T) {
	shimOnPath(t, "fakeshim")
	for _, arg := range []string{"a&b", "a|b", "a<b", "a>b", "a^b", "%PATH%", "a\nb", "a\rb"} {
		err := Validate("acp", []string{"fakeshim", "ok", arg})
		if err == nil {
			t.Errorf("Validate(arg %q) = nil, want a refusal", arg)
			continue
		}
		if !strings.Contains(err.Error(), "acp:") || !strings.Contains(err.Error(), "unsafe argument") {
			t.Errorf("Validate(arg %q) error = %v, want it namespaced and naming the argument", arg, err)
		}
	}
}

func TestValidate_AllowsSafeArgsAndNonShims(t *testing.T) {
	shimOnPath(t, "fakeshim")
	for _, arg := range []string{"plain", "with space", `a"b`, "a!b", "", `C:\dir\`, "ünïcødé"} {
		if err := Validate("acp", []string{"fakeshim", arg}); err != nil {
			t.Errorf("Validate(arg %q) = %v, want nil", arg, err)
		}
	}
	// A real .exe target is never refused, whatever the argument.
	if err := Validate("acp", []string{"cmd.exe", "/c", "a&b"}); err != nil {
		t.Errorf("Validate(cmd.exe, a&b) = %v, want nil (.exe quoting is correct)", err)
	}
	// An unresolvable command is the spawn's error to report, not ours.
	if err := Validate("acp", []string{"definitely-not-a-command-xyz", "a&b"}); err != nil {
		t.Errorf("Validate(unresolvable) = %v, want nil", err)
	}
	if err := Validate("acp", nil); err != nil {
		t.Errorf("Validate(nil) = %v, want nil", err)
	}
}
