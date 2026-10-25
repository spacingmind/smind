package taskrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

func decodeAskSettings(t *testing.T, raw string) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("settings %q: %v", raw, err)
	}
	return s
}

// TestClaudeSettings_AskRulesUnchanged: every mode but bypass gets exactly the
// always-ask rules under permissions.ask next to permissions.deny; bypass has
// no ask rules.
func TestClaudeSettings_AskRulesUnchanged(t *testing.T) {
	t.Parallel()
	want := ClaudeAlwaysAskRules()
	for _, mode := range []string{ClaudeModeAuto, ClaudeModeAcceptEdits, ClaudeModeDefault, ClaudeModePlan} {
		s := decodeAskSettings(t, claudeSettings(mode, t.TempDir()))
		if len(s) != 1 {
			t.Fatalf("%s: top-level keys = %v, want only permissions", mode, s)
		}
		perms, _ := s["permissions"].(map[string]any)
		if len(perms) != 2 || perms["deny"] == nil {
			t.Fatalf("%s: permissions keys = %v, want ask and deny (no allow)", mode, perms)
		}
		var got []string
		for _, v := range perms["ask"].([]any) {
			got = append(got, v.(string))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: ask = %v, want %v", mode, got, want)
		}
	}
	bypass := decodeAskSettings(t, claudeSettings(ClaudeModeBypass, t.TempDir()))["permissions"].(map[string]any)
	if bypass["ask"] != nil || bypass["deny"] == nil {
		t.Fatalf("bypass permissions = %v, want deny only", bypass)
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
		{ClaudeModeBypass, true}, // deny rules only
	} {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			t.Parallel()
			r, st, task := newClaudeMcpEnv(t)
			registerUserSmind(t, st)
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(policyCtx(&McpPolicy{}), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{Mode: tc.mode}, "", testClaudeModel, events)
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
				if v != claudeSettings(tc.mode, config.Dir()) {
					t.Fatalf("--settings = %q, want %q", v, claudeSettings(tc.mode, config.Dir()))
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

func denyList(t *testing.T, settings string) []string {
	t.Helper()
	perms := decodeAskSettings(t, settings)["permissions"].(map[string]any)
	var out []string
	for _, v := range perms["deny"].([]any) {
		out = append(out, v.(string))
	}
	return out
}

func hasRule(rules []string, want string) bool {
	for _, r := range rules {
		if r == want {
			return true
		}
	}
	return false
}

// TestClaudeSettings_DenyRulesForSmindSecrets: Read/Edit/Write for each secret
// of THAT home, the plugin data dirs, and the Bash readers.
func TestClaudeSettings_DenyRulesForSmindSecrets(t *testing.T) {
	t.Parallel()
	home := "/srv/smind-home"
	for _, mode := range []string{ClaudeModeAuto, ClaudeModeDefault, ClaudeModeBypass} {
		rules := denyList(t, claudeSettings(mode, home))
		for _, f := range []string{"token", "config.yaml", "relay-keypair.json", "smind.db", "smind.db-wal", "smind.db-shm"} {
			for _, tool := range []string{"Read", "Edit", "Write"} {
				if want := tool + "(//srv/smind-home/" + f + ")"; !hasRule(rules, want) {
					t.Fatalf("%s: missing %s in %v", mode, want, rules)
				}
			}
		}
		for _, want := range []string{
			"Read(//srv/smind-home/plugins/*/data/**)",
			"Bash(cat /srv/smind-home/token*)",
			"Bash(head /srv/smind-home/smind.db-wal*)",
			"Bash(base64 /srv/smind-home/config.yaml*)",
			"Bash(sqlite3 /srv/smind-home/smind.db*)",
		} {
			if !hasRule(rules, want) {
				t.Fatalf("%s: missing %s in %v", mode, want, rules)
			}
		}
	}
}

// TestClaudeSettings_DenyRules_DoNotCoverWorktrees: no rule is a prefix of, or
// a wildcard over, a path inside $SMIND_HOME/worktrees.
func TestClaudeSettings_DenyRules_DoNotCoverWorktrees(t *testing.T) {
	t.Parallel()
	home := "/srv/smind-home"
	wt := "/srv/smind-home/worktrees/1/task/src/main.go"
	for _, r := range denyList(t, claudeSettings(ClaudeModeAuto, home)) {
		if _, arg, ok := strings.Cut(strings.TrimSuffix(r, ")"), "("); ok {
			arg = strings.TrimPrefix(arg, "/") // Read(//abs) -> /abs
			if i := strings.LastIndex(arg, " "); i >= 0 {
				arg = arg[i+1:] // Bash(cat /abs*) -> /abs*
			}
			if strings.Contains(arg, "worktrees") {
				t.Fatalf("rule %s mentions worktrees", r)
			}
			if ok, _ := filepath.Match(arg, wt); ok {
				t.Fatalf("rule %s matches worktree path %s", r, wt)
			}
			if p := strings.TrimSuffix(arg, "**"); p != arg && strings.HasPrefix(wt, p) {
				t.Fatalf("rule %s covers worktree path %s", r, wt)
			}
			if p := strings.TrimSuffix(arg, "*"); p != arg && strings.HasPrefix(wt, p) {
				t.Fatalf("rule %s covers worktree path %s", r, wt)
			}
		}
	}
}

// TestClaudeSettings_DenyRules_FollowSmindHomeOverride: two homes, two
// disjoint rule sets; and RunPrompt resolves SMIND_HOME at launch.
func TestClaudeSettings_DenyRules_FollowSmindHomeOverride(t *testing.T) {
	a := denyList(t, claudeSettings(ClaudeModeAuto, "/srv/home-a"))
	b := denyList(t, claudeSettings(ClaudeModeAuto, "/srv/home-b"))
	if !hasRule(a, "Read(//srv/home-a/token)") || hasRule(a, "Read(//srv/home-b/token)") {
		t.Fatalf("home-a rules wrong: %v", a)
	}
	if !hasRule(b, "Read(//srv/home-b/token)") || hasRule(b, "Read(//srv/home-a/token)") {
		t.Fatalf("home-b rules wrong: %v", b)
	}
	t.Setenv("SMIND_HOME", "/srv/home-c")
	if !hasRule(denyList(t, claudeSettings(ClaudeModeAuto, config.Dir())), "Read(//srv/home-c/token)") {
		t.Fatal("config.Dir() override not reflected")
	}
}
