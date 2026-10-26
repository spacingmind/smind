package secretdeny

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRules_TokenAndWorktrees(t *testing.T) {
	t.Parallel()
	rules := Rules("/h/smind")
	has := func(w string) bool {
		for _, r := range rules {
			if r == w {
				return true
			}
		}
		return false
	}
	if !has("Read(//h/smind/token)") || !has("Bash(cat /h/smind/token*)") {
		t.Fatalf("token not denied: %v", rules)
	}
	for _, r := range rules {
		if strings.Contains(r, "worktrees") {
			t.Fatalf("rule %q touches worktrees", r)
		}
	}
}

func TestCLIPath_WindowsDriveBecomesPOSIX(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"C:/Users/a/.smind":                 "/c/Users/a/.smind",
		filepath.FromSlash("D:/gotmp/home"): "/d/gotmp/home",
		"/h/smind":                          "/h/smind",
	} {
		if got := cliPath(in); got != want {
			t.Errorf("cliPath(%q) = %q, want %q", in, got, want)
		}
	}
	if !strings.Contains(strings.Join(Rules("C:/Users/a/.smind"), "\n"), "Read(//c/Users/a/.smind/token)") {
		t.Error("a Windows home is not spelled with its drive as a directory")
	}
}
