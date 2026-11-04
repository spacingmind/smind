package taskrunner

import (
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/providers"
)

// claudeLevels is the Claude Code native vocabulary.
var claudeLevels = []ThinkingLevel{ThinkingLevelOff, ThinkingLevelStandard, ThinkingLevelExtended}

// levelChoices returns the levels a run of kind on model may use. known is
// false when they are not discoverable yet (a Codex model list that has not
// been probed, or an older app-server without efforts): the caller then lets
// everything but Claude's own vocabulary through for the backend to judge.
func levelChoices(catalog ProviderInfo, kind providers.Runner, model string) (choices []string, known bool) {
	switch kind {
	case providers.RunnerClaude:
		for _, l := range claudeLevels {
			choices = append(choices, string(l))
		}
		return choices, true
	case providers.RunnerCodex:
		if model == "" {
			for _, m := range catalog.Models {
				if m.Default {
					model = m.ID
				}
			}
		}
		for _, m := range catalog.Models {
			if m.ID != model || len(m.Efforts) == 0 {
				continue
			}
			for _, e := range m.Efforts {
				choices = append(choices, e.ID)
			}
			return choices, true
		}
		return nil, false
	default:
		return nil, true
	}
}

// ResolveLevel applies the scope rule for a reasoning level: a level is valid
// only for the provider and model it was validated for. It reads the
// already-discovered catalog (never probes).
//
// An explicit request (a caller named the level for this send) that the run's
// provider/model does not support is an error naming the supported levels --
// never silently dropped. An inherited one (a profile pin, a queued or retried
// item, a failover successor, an old stored row) is never an error: it is
// dropped to the provider's own default and note says so, so a failover or a
// model change can not kill the run it was meant to rescue.
//
// Providers with no level (ACP agents) drop any level with a note. A Codex
// model whose efforts are unknown lets a level through unless it is one of
// Claude's own words, which no Codex model uses.
func ResolveLevel(catalog ProviderInfo, kind providers.Runner, model string, level ThinkingLevel, explicit bool) (ThinkingLevel, string, error) {
	if level == "" {
		return "", "", nil
	}
	choices, known := levelChoices(catalog, kind, model)
	supported := false
	switch {
	case known:
		for _, c := range choices {
			if c == string(level) {
				supported = true
			}
		}
	case kind == providers.RunnerCodex:
		supported = !isClaudeLevel(level)
	}
	if supported {
		return level, "", nil
	}
	name := model
	if name == "" {
		name = "the default model"
	}
	if kind != providers.RunnerClaude && kind != providers.RunnerCodex {
		return "", fmt.Sprintf("reasoning level %q ignored: %s runs take no reasoning level", level, catalog.ID), nil
	}
	if explicit {
		if len(choices) == 0 {
			return "", "", fmt.Errorf("invalid reasoning level %q for %s (%s): its reasoning levels are not discovered yet and Claude's off/standard/extended do not apply to Codex", level, name, catalog.ID)
		}
		return "", "", fmt.Errorf("invalid reasoning level %q for %s (%s) (supported: %s)", level, name, catalog.ID, strings.Join(choices, ", "))
	}
	return "", fmt.Sprintf("reasoning level %q is not supported by %s (%s); using the provider default", level, name, catalog.ID), nil
}

func isClaudeLevel(l ThinkingLevel) bool {
	for _, c := range claudeLevels {
		if c == l {
			return true
		}
	}
	return false
}
