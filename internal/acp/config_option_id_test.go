package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

// An agent that follows the stable ACP schema names an option's identifier
// "id" (OpenCode 1.18.34 does); earlier drafts used "configId". Both must give
// the same ConfigID, or the option can be listed but never set.
func TestConfigOption_DecodesIDAndConfigIDSpellings(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"id (stable schema, OpenCode)": `{"id":"model","name":"Model","category":"model","type":"select","currentValue":"a/b","options":[{"value":"a/b","name":"B"}]}`,
		"configId (earlier draft)":     `{"configId":"model","name":"Model","category":"model","type":"select","currentValue":"a/b","options":[{"value":"a/b","name":"B"}]}`,
	} {
		var o ConfigOption
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if o.ConfigID != "model" || o.Category != "model" || o.Type != "select" || len(o.Options) != 1 || o.Options[0].Value != "a/b" {
			t.Errorf("%s: decoded %+v", name, o)
		}
	}
}

// When both are present configId wins (it is what smind has always read).
func TestConfigOption_ConfigIDWinsWhenBothAreSet(t *testing.T) {
	t.Parallel()
	var o ConfigOption
	if err := json.Unmarshal([]byte(`{"id":"from-id","configId":"from-configId","type":"select"}`), &o); err != nil {
		t.Fatal(err)
	}
	if o.ConfigID != "from-configId" {
		t.Errorf("ConfigID = %q, want configId to win", o.ConfigID)
	}
}

// A list decodes element by element, and an option without either field stays
// empty rather than failing the whole session/new.
func TestConfigOption_ListAndMissingID(t *testing.T) {
	t.Parallel()
	var opts []ConfigOption
	if err := json.Unmarshal([]byte(`[{"id":"model","type":"select"},{"configId":"mode","type":"select"},{"type":"select"}]`), &opts); err != nil {
		t.Fatal(err)
	}
	if len(opts) != 3 || opts[0].ConfigID != "model" || opts[1].ConfigID != "mode" || opts[2].ConfigID != "" {
		t.Errorf("opts = %+v", opts)
	}
	if err := json.Unmarshal([]byte(`{"id": 5}`), &ConfigOption{}); err == nil {
		t.Error("a non-string id must still be an error")
	}
}

// What smind sends on the wire is unchanged: configId, not id.
func TestConfigOption_MarshalKeepsConfigID(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(ConfigOption{ConfigID: "model", Type: "select"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"configId":"model"`) || strings.Contains(string(b), `"id"`) {
		t.Errorf("marshalled = %s", b)
	}
}
