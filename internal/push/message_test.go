package push

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
)

func buildJSON(t *testing.T, m Message) map[string]any {
	t.Helper()
	raw, err := json.Marshal(buildExpo(m))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuildExpoAndroidIsDataOnly(t *testing.T) {
	m := buildJSON(t, Message{Token: "tok", Platform: PlatformAndroid, Data: Blob{V: 1, N: "n", C: "c"}})
	for _, key := range []string{"title", "body", "mutableContent"} {
		if _, ok := m[key]; ok {
			t.Fatalf("android message must not carry %q: %v", key, m)
		}
	}
	if m["channelId"] != ChannelAttention {
		t.Fatalf("channelId = %v, want %q", m["channelId"], ChannelAttention)
	}
	if m["priority"] != "high" {
		t.Fatalf("priority = %v, want high", m["priority"])
	}
	if m["ttl"] != float64(TTLSeconds) {
		t.Fatalf("ttl = %v, want %d", m["ttl"], TTLSeconds)
	}
	data, ok := m["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want an object", m["data"])
	}
	if got := slices.Sorted(maps.Keys(data)); !slices.Equal(got, []string{"c", "n", "v"}) {
		t.Fatalf("data keys = %v, want exactly [c n v]", got)
	}
}

func TestBuildExpoIOSHasGenericAlert(t *testing.T) {
	m := buildJSON(t, Message{Token: "tok", Platform: PlatformIOS, Data: Blob{V: 1, N: "n", C: "c"}})
	if m["title"] != GenericTitle {
		t.Fatalf("title = %v, want %q", m["title"], GenericTitle)
	}
	if m["body"] != GenericBody {
		t.Fatalf("body = %v, want %q", m["body"], GenericBody)
	}
	if m["mutableContent"] != true {
		t.Fatalf("mutableContent = %v, want true", m["mutableContent"])
	}
	if _, ok := m["channelId"]; ok {
		t.Fatalf("ios message must not carry channelId: %v", m)
	}
}
