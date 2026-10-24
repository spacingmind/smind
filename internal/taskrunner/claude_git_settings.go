package taskrunner

import "encoding/json"

// claudeGitAskSettings returns the inline --settings JSON that makes the
// Claude CLI itself ask before every always-ask git/gh operation, or "" for
// bypassPermissions (the Human's own per-run choice, ADR-0019). The rules
// ride only on this run's command line: no settings file is written. The CLI
// applies `ask` rules in auto and acceptEdits too, where it would otherwise
// run `git push` silently (auto) or by its own allow logic, and routes the ask
// through --permission-prompt-tool to the permission decider, which annotates
// the card (gitrules.go).
//
// Limit: --settings is fixed when the CLI starts. A live switch into
// bypassPermissions (SetPermissionMode) keeps asking for these operations
// until the next turn respawns the CLI (the CLI applies ask rules in bypass
// too), and a live switch out of bypass has no rules until the next turn; the
// decider still annotates whatever the CLI does escalate.
func claudeGitAskSettings(mode string) string {
	if mode == ClaudeModeBypass {
		return ""
	}
	b, err := json.Marshal(map[string]any{
		"permissions": map[string]any{"ask": ClaudeAlwaysAskRules()},
	})
	if err != nil {
		return ""
	}
	return string(b)
}
