package contrast

import (
	"github.com/spacingmind/smind/internal/learning/loads"
	"github.com/spacingmind/smind/internal/learning/probe/signature"
	"github.com/spacingmind/smind/internal/learning/window"
)

// Loaded reports whether any event of the arm loads the skill, via
// loads.Match unchanged (a Skill tool call naming it, or a Read under
// <projectionDir>/.claude/skills/<name>/).
func Loaded(events []window.Event, skill string, projectionDir string) bool {
	names := map[string]bool{skill: true}
	for _, ev := range events {
		if len(loads.Match(ev.E, names, projectionDir)) > 0 {
			return true
		}
	}
	return false
}

// FailureRecurred reports whether the targeted failure occurs in events.
// nil when sig is nil (the evidence kind has no signature).
func FailureRecurred(events []window.Event, sig *signature.Signature) *bool {
	if sig == nil {
		return nil
	}
	r := len(signature.MatchSeqs(events, *sig, nil)) > 0
	return &r
}
