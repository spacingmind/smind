package secretdeny

import (
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
