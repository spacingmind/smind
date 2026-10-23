package wsapi

import (
	"reflect"
	"strings"
	"testing"
)

func TestProfileCreate_ContractFields(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	child, errMsg := profileCall(t, ws, "a", "profile.create", map[string]any{"name": "child", "provider": "glm"})
	if errMsg != "" {
		t.Fatalf("create child: %s", errMsg)
	}
	created, errMsg := profileCall(t, ws, "b", "profile.create", map[string]any{
		"name": "sup", "provider": "claude-native", "orchestrationTools": true, "instructions": "coordinate",
		"skills": []string{"smind-leader"}, "childPolicy": "profiles", "childProfileIds": []int64{child.ID},
	})
	if errMsg != "" {
		t.Fatalf("create sup: %s", errMsg)
	}
	got, errMsg := profileCall(t, ws, "c", "profile.get", map[string]any{"id": created.ID})
	if errMsg != "" {
		t.Fatalf("get: %s", errMsg)
	}
	if got.OrchestrationTools == nil || !*got.OrchestrationTools || got.Instructions != "coordinate" ||
		!reflect.DeepEqual(got.Skills, []string{"smind-leader"}) || got.ChildPolicy != "profiles" ||
		!reflect.DeepEqual(got.ChildProfileIDs, []int64{child.ID}) {
		t.Fatalf("get = %+v", got)
	}
	// An explicit null orchestrationTools is a legacy (unmanaged) profile.
	plain, errMsg := profileCall(t, ws, "d", "profile.create", map[string]any{"name": "plain", "provider": "glm", "orchestrationTools": nil})
	if errMsg != "" || plain.OrchestrationTools != nil || plain.ChildPolicy != "" {
		t.Fatalf("plain = %+v, %q", plain, errMsg)
	}
}

func TestProfileUpdate_ContractReplaced(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	created, errMsg := profileCall(t, ws, "a", "profile.create", map[string]any{
		"name": "lead", "provider": "claude-native", "orchestrationTools": true, "instructions": "lead", "skills": []string{"smind-leader"}, "childPolicy": "leaf",
	})
	if errMsg != "" {
		t.Fatalf("create: %s", errMsg)
	}
	// Full-record replace: omitting the contract fields clears them.
	updated, errMsg := profileCall(t, ws, "b", "profile.update", map[string]any{"id": created.ID, "name": "lead", "provider": "claude-native"})
	if errMsg != "" {
		t.Fatalf("update: %s", errMsg)
	}
	if updated.OrchestrationTools != nil || updated.Instructions != "" || len(updated.Skills) != 0 || updated.ChildPolicy != "" {
		t.Fatalf("updated = %+v, want an empty contract", updated)
	}
	peer, errMsg := profileCall(t, ws, "c", "profile.update", map[string]any{"id": created.ID, "name": "lead", "provider": "claude-native", "orchestrationTools": false})
	if errMsg != "" || peer.OrchestrationTools == nil || *peer.OrchestrationTools || peer.ChildPolicy != "none" {
		t.Fatalf("peer = %+v, %q", peer, errMsg)
	}
}

func TestProfileCreate_ContractValidationErrors(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"peer with leaf", map[string]any{"orchestrationTools": false, "childPolicy": "leaf"}, "a profile without orchestration tools cannot launch children"},
		{"unknown skill", map[string]any{"skills": []string{"nope"}}, "unknown skill"},
		{"profiles without ids", map[string]any{"orchestrationTools": true, "childPolicy": "profiles"}, "requires childProfileIds"},
		{"missing child", map[string]any{"orchestrationTools": true, "childPolicy": "profiles", "childProfileIds": []int64{99}}, "not found"},
		{"instructions too long", map[string]any{"instructions": strings.Repeat("x", 16<<10+1)}, "over the"},
	}
	for i, c := range cases {
		params := map[string]any{"name": "x", "provider": "glm"}
		for k, v := range c.params {
			params[k] = v
		}
		_, errMsg := profileCall(t, ws, string(rune('a'+i)), "profile.create", params)
		if !strings.Contains(errMsg, c.want) {
			t.Errorf("%s: error = %q, want containing %q", c.name, errMsg, c.want)
		}
	}
}
