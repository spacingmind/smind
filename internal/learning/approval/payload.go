package approval

import (
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/learning/gate"
)

// Item is one gated change inside a proposal's stored payload
// (learned_skill_proposals.intents).
type Item struct {
	// Intent is the model's intent as proposed (post-parse).
	Intent gate.Intent `json:"intent"`
	// Result is the gate verdict's final create/patch text.
	Result gate.Skill `json:"result"`
	// TargetSkillID is the patch/delete/bundle target skill.
	TargetSkillID int64 `json:"targetSkillId,omitempty"`
	// BaseVersion is the learned_skills.version a patch was computed against.
	BaseVersion int `json:"baseVersion,omitempty"`
}

// EncodeItems serialises items for the proposal's intents column.
func EncodeItems(items []Item) (string, error) {
	if items == nil {
		items = []Item{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("encode proposal items: %w", err)
	}
	return string(b), nil
}

// DecodeItems parses a proposal's intents column.
func DecodeItems(s string) ([]Item, error) {
	var items []Item
	if err := json.Unmarshal([]byte(s), &items); err != nil {
		return nil, fmt.Errorf("decode proposal items: %w", err)
	}
	return items, nil
}
