package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSkillCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	prev := osStdout
	osStdout = &out
	defer func() { osStdout = prev }()
	code := run(append([]string{"skill"}, args...))
	return code, out.String()
}

func TestSkillCLI_InstallLsUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv(skillHomeEnv, home)

	if code, out := runSkillCLI(t, "ls"); code != 0 || !strings.Contains(out, "missing") || strings.Contains(out, "installed") {
		t.Fatalf("ls before install: code %d\n%s", code, out)
	}
	if code, out := runSkillCLI(t, "install", "--target", "claude", "smind"); code != 0 || !strings.Contains(out, "installed") {
		t.Fatalf("install: code %d\n%s", code, out)
	}
	skillMD := filepath.Join(home, ".claude", "skills", "smind", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("SKILL.md not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("--target claude touched codex: %v", err)
	}

	// Edited file: reported as modified/skipped, exit 0, --force hint.
	os.WriteFile(skillMD, []byte("mine\n"), 0o644)
	code, out := runSkillCLI(t, "install", "--target=claude", "smind")
	if code != 0 || !strings.Contains(out, "modified, skipped") || !strings.Contains(out, "--force") {
		t.Fatalf("install over edit: code %d\n%s", code, out)
	}
	if code, out := runSkillCLI(t, "ls", "--target", "claude", "smind"); code != 0 || !strings.Contains(out, "modified") {
		t.Fatalf("ls after edit: code %d\n%s", code, out)
	}
	if code, _ := runSkillCLI(t, "install", "--target", "claude", "--force", "smind"); code != 0 {
		t.Fatal("force install failed")
	}
	if b, _ := os.ReadFile(skillMD); string(b) == "mine\n" {
		t.Fatal("--force did not overwrite")
	}

	if code, _ := runSkillCLI(t, "install"); code != 0 {
		t.Fatal("install all failed")
	}
	if code, _ := runSkillCLI(t, "uninstall"); code != 0 {
		t.Fatal("uninstall failed")
	}
	if _, err := os.Stat(skillMD); !os.IsNotExist(err) {
		t.Fatalf("SKILL.md still present after uninstall: %v", err)
	}

	if code, _ := runSkillCLI(t, "install", "nope"); code != 1 {
		t.Fatalf("unknown skill exit = %d, want 1", code)
	}
	if code, _ := runSkillCLI(t, "install", "--target", "bogus"); code != 1 {
		t.Fatalf("bad target exit = %d, want 1", code)
	}
	if code, _ := runSkillCLI(t); code != 2 {
		t.Fatalf("no subcommand exit = %d, want 2", code)
	}
}
