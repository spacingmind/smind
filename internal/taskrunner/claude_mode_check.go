package taskrunner

import (
	"encoding/json"
	"fmt"
)

// claudeModeRank orders Claude Code's permission modes from the most to the
// least restrictive. A mode outside the table (a future CLI value) has no
// rank: a mismatch against it is still reported, never as "more permissive".
var claudeModeRank = map[string]int{
	ClaudeModePlan:        0,
	ClaudeModeDefault:     1,
	ClaudeModeAcceptEdits: 2,
	ClaudeModeAuto:        3,
	ClaudeModeBypass:      4,
}

// claudeModeWords are the user-facing names of the modes (the catalog's own
// labels, kept static so the check never depends on which modes this machine
// offers).
var claudeModeWords = map[string]string{
	ClaudeModePlan:        "Plan Mode (plan)",
	ClaudeModeDefault:     "Manual, asks before each tool (default)",
	ClaudeModeAcceptEdits: "Accept File Edits (acceptEdits)",
	ClaudeModeAuto:        "Auto mode (auto)",
	ClaudeModeBypass:      "Bypass (bypassPermissions)",
}

// claudeModeCause names the settings key behind a refused mode, never its
// value or the file it came from.
var claudeModeCause = map[string]string{
	ClaudeModeAuto:   "Check permissions.disableAutoMode in your Claude settings, and that your plan and model support auto mode.",
	ClaudeModeBypass: "Check permissions.disableBypassPermissionsMode in your Claude settings.",
}

func claudeModeWord(mode string) string {
	if w, ok := claudeModeWords[mode]; ok {
		return w
	}
	return fmt.Sprintf("%q", mode)
}

// claudeModeMismatchNote compares the mode a run asked the CLI for with the
// one the CLI reports on its system/init frame (`permissionMode`) and returns
// the one session note to show, or "" when there is nothing to say: the modes
// match, the field is missing (an older CLI) or is not a non-empty string. It
// only reads: smind never changes the mode on the CLI's behalf (ADR-0019), and
// the note carries mode names and a settings key, no file content or path.
func claudeModeMismatchNote(requested string, initRaw json.RawMessage) string {
	var w struct {
		PermissionMode json.RawMessage `json:"permissionMode"`
	}
	if json.Unmarshal(initRaw, &w) != nil || len(w.PermissionMode) == 0 {
		return ""
	}
	var reported string
	if json.Unmarshal(w.PermissionMode, &reported) != nil || reported == "" || requested == "" || reported == requested {
		return ""
	}
	rr, rok := claudeModeRank[requested]
	pr, pok := claudeModeRank[reported]
	if rok && pok && pr > rr {
		return fmt.Sprintf("Warning: Claude started in %s although %s was requested, so it may run tools without asking where you expected a prompt. smind did not change the mode.",
			claudeModeWord(reported), claudeModeWord(requested))
	}
	note := fmt.Sprintf("Claude started in %s instead of the requested %s.", claudeModeWord(reported), claudeModeWord(requested))
	if rok && pok && pr < rr {
		if requested == ClaudeModeAuto || requested == ClaudeModeBypass {
			note += " It will ask before tool calls that you expected to run on their own."
		}
		if cause := claudeModeCause[requested]; cause != "" {
			note += " " + cause
		}
	}
	return note + " The mode shown for this run is the one requested."
}
