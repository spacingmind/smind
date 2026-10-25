package wsapi

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/skills"
)

// skillEntry is one row of skill.list: a skill smind ships or a plugin
// contributes. Owner is the plugin id, null for a bundled skill; ReadOnly is
// true for every entry (neither kind is editable through the API).
type skillEntry struct {
	Name        string  `json:"name"`
	Source      string  `json:"source"` // "bundled" | "plugin"
	Owner       *string `json:"owner"`
	ReadOnly    bool    `json:"readOnly"`
	Description string  `json:"description,omitempty"`
}

// handleSkillList lists the skills a profile may name: the bundled ones and
// those of ACTIVE plugins as "<plugin>:<skill>". A disabled, removed or
// switched-off plugin's skills are not listed. Readable over the relay.
func handleSkillList(mgr *plugins.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		bundle, err := skills.Bundle()
		if err != nil {
			return nil, err
		}
		out := make([]skillEntry, 0, len(bundle))
		for _, s := range bundle {
			out = append(out, skillEntry{Name: s.Name, Source: "bundled", ReadOnly: true})
		}
		for _, s := range mgr.Skills() {
			owner := s.Plugin
			out = append(out, skillEntry{Name: s.Qualified, Source: "plugin", Owner: &owner, ReadOnly: true, Description: s.Description})
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out, nil
	}
}
