package taskrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func decodeAskSettings(t *testing.T, raw string) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("settings %q: %v", raw, err)
	}
	return s
}

// TestClaudeGitAskSettingsPerMode: every mode but bypass gets exactly the
// always-ask rules under permissions.ask, and nothing else.
func TestClaudeGitAskSettingsPerMode(t *testing.T) {
	t.Parallel()
	want := ClaudeAlwaysAskRules()
	for _, mode := range []string{ClaudeModeAuto, ClaudeModeAcceptEdits, ClaudeModeDefault, ClaudeModePlan} {
		s := decodeAskSettings(t, claudeGitAskSettings(mode))
		if len(s) != 1 {
			t.Fatalf("%s: top-level keys = %v, want only permissions", mode, s)
		}
		perms, _ := s["permissions"].(map[string]any)
		if len(perms) != 1 {
			t.Fatalf("%s: permissions keys = %v, want only ask (no allow/deny)", mode, perms)
		}
		var got []string
		for _, v := range perms["ask"].([]any) {
			got = append(got, v.(string))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: ask = %v, want %v", mode, got, want)
		}
	}
	if got := claudeGitAskSettings(ClaudeModeBypass); got != "" {
		t.Fatalf("bypass settings = %q, want none", got)
	}
}

// TestClaudeAskRulesCoverEveryAlwaysAskClass is the drift test: each sample
// command is an always-ask class for the matcher, its rule ID has a CLI
// pattern entry, and every entry is exercised by a sample.
func TestClaudeAskRulesCoverEveryAlwaysAskClass(t *testing.T) {
	t.Parallel()
	samples := map[string]string{
		"git-push":             "git push origin main",
		"gh-pr-create":         "gh pr create --title x",
		"gh-pr-merge":          "gh pr merge 1",
		"git-reset-hard":       "git reset --hard HEAD",
		"git-branch-delete":    "git branch -D topic",
		"git-clean":            "git clean -fd",
		"git-checkout-discard": "git checkout -- f",
		"git-restore":          "git restore .",
		"git-stash-drop":       "git stash drop",
		"git-stash-clear":      "git stash clear",
		"git-commit-amend":     "git commit --amend",
	}
	inTable := map[string]bool{}
	for _, r := range claudeAskRules {
		inTable[r.ID] = true
	}
	for id, cmd := range samples {
		r := ClassifyGitCommand(cmd)
		if r == nil || r.Class != GitClassAlwaysAsk || r.ID != id {
			t.Fatalf("%q classifies as %+v, want always-ask %s", cmd, r, id)
		}
		if !inTable[id] {
			t.Fatalf("always-ask class %s has no claudeAskRules entry", id)
		}
	}
	for id := range inTable {
		if _, ok := samples[id]; !ok {
			t.Fatalf("claudeAskRules entry %s is not an always-ask class", id)
		}
	}
	rules := strings.Join(ClaudeAlwaysAskRules(), "\n")
	for _, p := range []string{"Bash(git push:*)", "Bash(*git * push*)", "Bash(*gh pr create*)", "Bash(*git reset*--hard*)", "Bash(*git commit*--amend*)"} {
		if !strings.Contains(rules, p) {
			t.Fatalf("rules missing %s:\n%s", p, rules)
		}
	}
}

// TestRunClaude_GitAskSettingsReachTheCLI (fakeclaude): the launch argv carries
// --settings with the rules in auto/acceptEdits, none in bypass, and the MCP
// config flags of the same launch are untouched.
func TestRunClaude_GitAskSettingsReachTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode string
		want bool
	}{
		{ClaudeModeAuto, true},
		{ClaudeModeAcceptEdits, true},
		{"", true},
		{ClaudeModeBypass, false},
	} {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			t.Parallel()
			r, st, task := newClaudeMcpEnv(t)
			registerUserSmind(t, st)
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(policyCtx(&McpPolicy{}), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{Mode: tc.mode}, "", "", events)
			}()
			drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(string(data), "\n")
			v, ok := flagValue(args, "--settings")
			if ok != tc.want {
				t.Fatalf("--settings present = %v, want %v (args %q)", ok, tc.want, args)
			}
			if ok {
				if v != claudeGitAskSettings(tc.mode) {
					t.Fatalf("--settings = %q, want %q", v, claudeGitAskSettings(tc.mode))
				}
				if strings.Count(string(data), "--settings") != 1 {
					t.Fatalf("--settings passed more than once: %q", args)
				}
			}
			// The MCP launch plumbing is preserved next to --settings.
			if !hasFlag(args, "--mcp-config") || !hasFlag(args, "--strict-mcp-config") {
				t.Fatalf("mcp flags lost: %q", args)
			}
		})
	}
}
