package wsapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

func TestUsage_AccountJSONKeysAllowlist(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(AccountUsage{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	var got []string
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"accountId", "credentialType", "displayName", "inFlight", "key", "lastError",
		"nextRefreshAt", "provider", "refreshBlocked", "report", "stale"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	// with a cooldown, exactly one more key
	b, _ = json.Marshal(AccountUsage{Cooldown: &usageCooldown{}})
	m = nil
	_ = json.Unmarshal(b, &m)
	if _, ok := m["cooldown"]; !ok || len(m) != len(want)+1 {
		t.Fatalf("cooldown key missing or extra keys: %v", m)
	}
}

func TestUsage_AccountCooldownAndObservedFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	in := AccountUsage{
		AccountID: 1, Key: "anthropic-1",
		Cooldown: &usageCooldown{Until: time.Unix(0, 0).UTC().Format(time.RFC3339), Scope: "model", Model: "opus", Reason: "rate_limited"},
		Report:   &quota.Report{Version: 2, Windows: []quota.Window{{ID: "w", Source: "observed", Status: "limited"}}},
	}
	b, _ := json.Marshal(in)
	var out AccountUsage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Cooldown == nil || *out.Cooldown != *in.Cooldown {
		t.Fatalf("cooldown = %+v", out.Cooldown)
	}
	w := out.Report.Windows[0]
	if w.Source != "observed" || w.Status != "limited" {
		t.Fatalf("window = %+v", w)
	}
	// omitempty: absent when unset
	b, _ = json.Marshal(quota.Window{ID: "x"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["source"]; ok {
		t.Fatal("source should be omitted")
	}
	if _, ok := m["status"]; ok {
		t.Fatal("status should be omitted")
	}
}
