package taskrunner

import (
	"encoding/json"

	"github.com/spacingmind/smind/internal/secretdeny"
)

// claudeSettings returns the inline --settings JSON for a claude-native run:
// permissions.deny for smind's own secrets under home (every mode: the CLI
// enforces deny rules in bypassPermissions too), and, except in
// bypassPermissions (the Human's own per-run choice, ADR-0019),
// permissions.ask making the Claude CLI itself ask before every always-ask
// git/gh operation. The rules
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
func claudeSettings(mode, home string) string {
	perms := map[string]any{"deny": secretdeny.Rules(home)}
	if mode != ClaudeModeBypass {
		perms["ask"] = ClaudeAlwaysAskRules()
	}
	b, err := json.Marshal(map[string]any{"permissions": perms})
	if err != nil {
		return ""
	}
	return string(b)
}
