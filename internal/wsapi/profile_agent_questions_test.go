package wsapi

import (
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// ADR-0055 D4: agentQuestions is an explicit opt-in. Create without it is off;
// an update that does not name it keeps the stored value (an older client must
// not flip it); naming it changes it.
func TestProfile_AgentQuestionsOptInIsExplicitAndKeptOnOmittedUpdate(t *testing.T) {
	e := newContribEnv(t)
	created, err := e.call("profile.create", localRC(), map[string]any{"name": "acp-default", "provider": "glm"})
	if err != nil {
		t.Fatal(err)
	}
	p := created.(store.AgentProfile)
	if p.AgentQuestions {
		t.Fatal("a profile created without agentQuestions must be off")
	}
	upd := func(extra map[string]any) store.AgentProfile {
		params := map[string]any{"id": p.ID, "name": "acp-default", "provider": "glm"}
		for k, v := range extra {
			params[k] = v
		}
		got, err := e.call("profile.update", localRC(), params)
		if err != nil {
			t.Fatal(err)
		}
		return got.(store.AgentProfile)
	}
	if !upd(map[string]any{"agentQuestions": true}).AgentQuestions {
		t.Fatal("naming agentQuestions: true must opt in")
	}
	if !upd(map[string]any{"notes": "edit"}).AgentQuestions {
		t.Fatal("an update that omits agentQuestions must keep the opt-in")
	}
	if upd(map[string]any{"agentQuestions": false}).AgentQuestions {
		t.Fatal("naming agentQuestions: false must opt out")
	}
}
