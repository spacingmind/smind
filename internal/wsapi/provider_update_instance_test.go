package wsapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func (h *secretsHarness) file(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func (h *secretsHarness) listEntry(t *testing.T, id string) providerListEntry {
	t.Helper()
	res, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.(providerListResult).Providers {
		if string(p.ID) == id {
			return p
		}
	}
	t.Fatalf("provider %q not listed", id)
	return providerListEntry{}
}

func (h *secretsHarness) listWire(t *testing.T) map[string]map[string]any {
	t.Helper()
	res, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	var wire struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	by := map[string]map[string]any{}
	for _, p := range wire.Providers {
		by[p["id"].(string)] = p
	}
	return by
}

func updateParams(id string, kv ...any) map[string]any {
	m := map[string]any{"id": id}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

const handWrittenInstance = "    # hand written, keep me\n    - id: claude-hand\n      label: Claude Hand\n      extends: claude-native\n\n      env:\n          CLAUDE_CONFIG_DIR: /tmp/hand-dir # trailing comment\n"

func TestProviderUpdateInstance_RefusesAgentCaller(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	before := h.file(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	_, err := h.call(t, "provider.updateInstance", &requestContext{runScoped: true},
		updateParams("claude-zai", "disallowedTools", []string{"WebSearch"}))
	if err == nil || codedError(err).Code != codeForbidden {
		t.Fatalf("agent caller: err = %v, want forbidden", err)
	}
	if h.file(t) != before {
		t.Fatal("an agent caller changed config.yaml")
	}
	if ev := queued(sub); len(ev) != 0 {
		t.Fatalf("events = %v, want none", ev)
	}
	for _, m := range RunScopedMethods {
		if m == "provider.updateInstance" {
			t.Fatal("provider.updateInstance is on the run-scoped allowlist")
		}
	}
	if _, ok := h.api.hs["provider.updateInstance"]; !ok {
		t.Fatal("provider.updateInstance is not registered")
	}
}

func TestProviderUpdateInstance_RecipeMadeUIMadeHandWritten(t *testing.T) {
	h := newSecretsHarness(t)
	if _, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe": "zai-glm", "secrets": map[string]string{"api-key": keySentinel},
	}); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if _, err := h.call(t, "provider.addInstance", &requestContext{}, map[string]string{"extends": "claude-native", "id": "claude-ui", "label": "Claude UI", "dir": work}); err != nil {
		t.Fatal(err)
	}
	// Append the hand-written entry and reload.
	if err := os.WriteFile(h.path, []byte(h.file(t)+handWrittenInstance), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, "provider.reload", &requestContext{}, nil); err != nil {
		t.Fatal(err)
	}
	recipeEnvOf := func(text string) string {
		i := strings.Index(text, "id: zai-glm")
		if i < 0 {
			t.Fatalf("no zai-glm entry in %s", text)
		}
		rest := text[i:]
		if j := strings.Index(rest, "id: claude-ui"); j >= 0 {
			rest = rest[:j]
		}
		a := strings.Index(rest, "env:")
		b := strings.Index(rest, "\n    -")
		if a < 0 {
			t.Fatalf("recipe entry has no env: %s", rest)
		}
		if b < 0 || b < a {
			b = len(rest)
		}
		env := rest[a:b]
		for _, key := range []string{"\n      models:", "\n      additionalModels:", "\n      disallowedTools:"} {
			if k := strings.Index(env, key); k >= 0 {
				env = env[:k]
			}
		}
		return env
	}
	envBefore := recipeEnvOf(h.file(t))

	cases := []struct {
		id     string
		params map[string]any
		check  func(t *testing.T, o map[string]any)
	}{
		{"zai-glm", updateParams("zai-glm", "models", []map[string]any{{"id": "glm-extra", "label": "GLM Extra"}}, "disallowedTools", []string{"WebSearch"}),
			func(t *testing.T, o map[string]any) {
				if ms, _ := o["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != "glm-extra" {
					t.Fatalf("instance = %v", o)
				}
				if !reflect.DeepEqual(o["disallowedTools"], []any{"WebSearch"}) {
					t.Fatalf("instance = %v", o)
				}
			}},
		{"claude-ui", updateParams("claude-ui", "models", []map[string]any{{"id": "m-ui", "default": true}}, "inheritClaudeConfig", true),
			func(t *testing.T, o map[string]any) {
				if ms, _ := o["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != "m-ui" || o["inheritClaudeConfig"] != true {
					t.Fatalf("instance = %v", o)
				}
			}},
		{"claude-hand", updateParams("claude-hand", "additionalModels", []map[string]any{{"id": "m-hand"}}, "disallowedTools", []string{"Bash"}),
			func(t *testing.T, o map[string]any) {
				if am, _ := o["additionalModels"].([]any); len(am) != 1 || !reflect.DeepEqual(o["disallowedTools"], []any{"Bash"}) {
					t.Fatalf("instance = %v", o)
				}
			}},
	}
	for _, c := range cases {
		res, err := h.call(t, "provider.updateInstance", &requestContext{}, c.params)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if p := res.(providerUpdatedPayload); p.ID != c.id {
			t.Fatalf("%s: payload = %+v", c.id, p)
		}
		raw, _ := json.Marshal(res)
		if strings.Contains(string(raw), keySentinel) {
			t.Fatalf("%s: the result carries the key: %s", c.id, raw)
		}
		o, _ := h.listWire(t)[c.id]["instance"].(map[string]any)
		if o == nil {
			t.Fatalf("%s: no instance field", c.id)
		}
		c.check(t, o)
	}
	text := h.file(t)
	if got := recipeEnvOf(text); got != envBefore {
		t.Fatalf("recipe env changed:\nbefore %q\nafter  %q", envBefore, got)
	}
	if strings.Contains(text, keySentinel) {
		t.Fatalf("the key is in config.yaml: %s", text)
	}
	for _, keep := range []string{"# hand written, keep me", "# trailing comment", "CLAUDE_CONFIG_DIR: /tmp/hand-dir"} {
		if !strings.Contains(text, keep) {
			t.Fatalf("lost %q in %s", keep, text)
		}
	}
	if !h.stored(t, "zai-glm", "api-key") {
		t.Fatal("the recipe key is gone")
	}
}

func TestProviderUpdateInstance_AppliesLive(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	if _, err := h.call(t, "provider.updateInstance", &requestContext{}, updateParams("claude-zai",
		"models", []map[string]any{{"id": "live-a"}, {"id": "live-b", "label": "Live B"}})); err != nil {
		t.Fatal(err)
	}
	cat, ok := h.runner.ProviderCatalogFor("claude-zai")
	if !ok {
		t.Fatal("instance not in the runner catalog")
	}
	var ids []string
	for _, m := range cat.Models {
		ids = append(ids, m.ID)
	}
	if !reflect.DeepEqual(ids, []string{"live-a", "live-b"}) {
		t.Fatalf("merged models = %v", ids)
	}
	if got := h.listEntry(t, "claude-zai"); len(got.Models) != 2 {
		t.Fatalf("listed models = %+v", got.Models)
	}
	evs := queued(sub)
	if len(evs) != 1 || evs[0].Topic != TopicProviderUpdated {
		t.Fatalf("events = %v, want one provider.updated", evs)
	}
	if p, _ := evs[0].Payload.(providerUpdatedPayload); p.ID != "claude-zai" {
		t.Fatalf("event payload = %+v", evs[0].Payload)
	}
}

func TestProviderUpdateInstance_InvalidKeepsRegistry(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	before := h.file(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	listBefore, _ := json.Marshal(h.listWire(t))

	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"both lists", updateParams("claude-zai", "models", []map[string]any{{"id": "a"}}, "additionalModels", []map[string]any{{"id": "b"}}), "additionalModels"},
		{"unknown id", updateParams("nope", "disallowedTools", []string{"Bash"}), "not found"},
		{"empty id", updateParams("", "disallowedTools", []string{"Bash"}), "id is required"},
		{"no fields", updateParams("claude-zai"), "nothing to update"},
	}
	for _, c := range cases {
		_, err := h.call(t, "provider.updateInstance", &requestContext{}, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if h.file(t) != before {
		t.Fatal("an invalid update changed config.yaml")
	}
	if after, _ := json.Marshal(h.listWire(t)); string(after) != string(listBefore) {
		t.Fatalf("listing changed:\n%s\n%s", listBefore, after)
	}
	if ev := queued(sub); len(ev) != 0 {
		t.Fatalf("events = %v, want none", ev)
	}
}

func TestProviderUpdateInstance_IdempotentNoop(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	params := updateParams("claude-zai", "additionalModels", []map[string]any{{"id": "x1"}}, "disallowedTools", []string{"WebSearch"})
	if _, err := h.call(t, "provider.updateInstance", &requestContext{}, params); err != nil {
		t.Fatal(err)
	}
	before := h.file(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	res, err := h.call(t, "provider.updateInstance", &requestContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := res.(providerUpdatedPayload); !ok || p.ID != "claude-zai" {
		t.Fatalf("result = %+v", res)
	}
	if h.file(t) != before {
		t.Fatal("a no-op update changed config.yaml")
	}
	if ev := queued(sub); len(ev) != 0 {
		t.Fatalf("events = %v, want none", ev)
	}
}

func TestProviderList_InstanceEditableFields(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance, "    - id: acp-x\n      label: ACP X\n      command: \""+fakeACPAgentPath+"\"\n")
	if _, err := h.call(t, "provider.updateInstance", &requestContext{}, updateParams("claude-zai",
		"additionalModels", []map[string]any{{"id": "ed-a", "label": "Ed A", "description": "d"}},
		"disallowedTools", []string{"WebSearch"}, "inheritClaudeConfig", true)); err != nil {
		t.Fatal(err)
	}
	by := h.listWire(t)
	o, _ := by["claude-zai"]["instance"].(map[string]any)
	if o == nil {
		t.Fatalf("instance entry = %v", by["claude-zai"])
	}
	am, _ := o["additionalModels"].([]any)
	if len(am) != 1 || am[0].(map[string]any)["id"] != "ed-a" || am[0].(map[string]any)["label"] != "Ed A" ||
		!reflect.DeepEqual(o["disallowedTools"], []any{"WebSearch"}) || o["inheritClaudeConfig"] != true {
		t.Fatalf("instance = %v", o)
	}
	if _, has := o["models"]; has {
		t.Fatalf("instance has models in add mode: %v", o)
	}
	for _, id := range []string{"claude-native", "acp-x"} {
		e, ok := by[id]
		if !ok {
			t.Fatalf("%s not listed", id)
		}
		if _, has := e["instance"]; has {
			t.Fatalf("%s has an instance key: %v", id, e["instance"])
		}
	}
}
