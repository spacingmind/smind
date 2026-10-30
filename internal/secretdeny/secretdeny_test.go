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

func TestSecretdeny_ProviderEnvNames(t *testing.T) {
	t.Parallel()
	rules := EnvRules("/h/smind", []string{"ANTHROPIC_AUTH_TOKEN"})
	for _, want := range []string{
		"Bash(env)",
		"Bash(printenv*)",
		"Bash(set)",
		"Bash(export -p*)",
		"Bash(declare -x*)",
		"Bash(echo $ANTHROPIC_AUTH_TOKEN*)",
		"Bash(echo ${ANTHROPIC_AUTH_TOKEN}*)",
		"Read(//proc/*/environ)",
		"Read(//h/smind/providers/**)",
	} {
		if !contains(rules, want) {
			t.Errorf("EnvRules missing %q: %v", want, rules)
		}
	}
	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		if seen[r] {
			t.Errorf("EnvRules has a duplicate: %q", r)
		}
		seen[r] = true
	}
	if len(seen) != len(rules) {
		t.Errorf("EnvRules = %d entries, %d unique", len(rules), len(seen))
	}

	if got := EnvRules("/h/smind", nil); len(got) != 0 {
		t.Errorf("EnvRules with no names = %v, want empty", got)
	}

	bad := EnvRules("/h/smind", []string{"A B"})
	if !contains(bad, "Bash(env)") || !contains(bad, "Bash(printenv*)") {
		t.Errorf("EnvRules with a bad name lost the generic rules: %v", bad)
	}
	for _, r := range bad {
		if strings.Contains(r, "A B") {
			t.Errorf("EnvRules produced a per-name rule for %q: %q", "A B", r)
		}
	}

	both := EnvRules("/h/smind", []string{"ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY"})
	for _, want := range []string{
		"Bash(echo $ANTHROPIC_AUTH_TOKEN*)",
		"Bash(printenv OPENAI_API_KEY*)",
	} {
		if !contains(both, want) {
			t.Errorf("EnvRules with two names missing %q", want)
		}
	}
	seenBoth := map[string]bool{}
	for _, r := range both {
		if seenBoth[r] {
			t.Errorf("EnvRules with two names has a duplicate: %q", r)
		}
		seenBoth[r] = true
	}
	if len(seenBoth) != len(both) {
		t.Errorf("EnvRules with two names = %d entries, %d unique", len(both), len(seenBoth))
	}
}

func contains(rules []string, want string) bool {
	for _, r := range rules {
		if r == want {
			return true
		}
	}
	return false
}

func TestSecretdeny_EnvRulesAreDeny(t *testing.T) {
	t.Parallel()
	for _, rules := range [][]string{
		EnvRules("/h/smind", []string{"ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY"}),
		EnvRules("/h/smind", []string{"A B", "1BAD", "GOOD_NAME"}),
		EnvRules("C:/Users/a/.smind", []string{"ANTHROPIC_AUTH_TOKEN"}),
	} {
		for _, r := range rules {
			if !strings.HasPrefix(r, "Bash(") && !strings.HasPrefix(r, "Read(") {
				t.Errorf("rule %q is neither a Bash nor a Read deny", r)
			}
		}
	}
}
