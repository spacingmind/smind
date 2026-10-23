package profiles

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func boolPtr(b bool) *bool { return &b }

func wantErr(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Fatalf("error = %v, want containing %q", err, sub)
	}
}

func TestProfile_LegacyProfileUnchanged(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	got := mustCreate(t, r, store.AgentProfile{Name: "p", Provider: "claude-native"})
	if got.OrchestrationTools != nil || got.Instructions != "" || len(got.Skills) != 0 || got.ChildPolicy != "" || len(got.ChildProfileIDs) != 0 {
		t.Fatalf("legacy profile gained contract fields: %+v", got)
	}
	back, err := r.Get(got.ID)
	if err != nil || back.ChildPolicy != "" || back.OrchestrationTools != nil {
		t.Fatalf("Get = %+v, %v", back, err)
	}
}

func TestProfile_PeerForcesChildPolicyNone(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	child := mustCreate(t, r, store.AgentProfile{Name: "c", Provider: "claude-native"})
	for _, pol := range []string{"leaf", "profiles"} {
		p := store.AgentProfile{Name: "peer", Provider: "claude-native", OrchestrationTools: boolPtr(false), ChildPolicy: pol}
		if pol == "profiles" {
			p.ChildProfileIDs = []int64{child.ID}
		}
		_, err := r.Create(p)
		wantErr(t, err, "a profile without orchestration tools cannot launch children")
	}
	for _, pol := range []string{"", "none"} {
		got := mustCreate(t, r, store.AgentProfile{Name: "peer", Provider: "claude-native", OrchestrationTools: boolPtr(false), ChildPolicy: pol})
		if got.ChildPolicy != "none" {
			t.Errorf("policy %q stored as %q, want none", pol, got.ChildPolicy)
		}
	}
	// orchestrationTools=true with an unset policy also becomes none.
	got := mustCreate(t, r, store.AgentProfile{Name: "orch", Provider: "claude-native", OrchestrationTools: boolPtr(true)})
	if got.ChildPolicy != "none" {
		t.Errorf("managed unset policy = %q, want none", got.ChildPolicy)
	}
}

func TestProfile_InstructionsOverCapRejected(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	mustCreate(t, r, store.AgentProfile{Name: "ok", Provider: "claude-native", Instructions: strings.Repeat("x", 16<<10)})
	_, err := r.Create(store.AgentProfile{Name: "big", Provider: "claude-native", Instructions: strings.Repeat("x", 16<<10+1)})
	wantErr(t, err, "over the")
}

func TestProfile_UnknownSkillRejected(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	_, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", Skills: []string{"no-such-skill"}})
	wantErr(t, err, "unknown skill")
	_, err = r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", Skills: []string{"smind-leader", "smind-leader"}})
	wantErr(t, err, "duplicate skill")
}

func TestProfile_SkillsPlusInstructionsOverCapRejected(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	all := []string{"smind", "smind-advisor", "smind-committee", "smind-handoff", "smind-leader"}
	// Instructions alone are under the cap, the bodies tip the total over.
	_, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", Instructions: strings.Repeat("x", 16<<10), Skills: all})
	wantErr(t, err, "instructions plus skills")
	mustCreate(t, r, store.AgentProfile{Name: "ok", Provider: "claude-native", Instructions: "short", Skills: all})
}

func TestProfile_ChildProfilesRules(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	child := mustCreate(t, r, store.AgentProfile{Name: "c", Provider: "claude-native"})
	yes := boolPtr(true)

	_, err := r.Create(store.AgentProfile{Name: "s", Provider: "claude-native", OrchestrationTools: yes, ChildPolicy: "profiles"})
	wantErr(t, err, "requires childProfileIds")
	_, err = r.Create(store.AgentProfile{Name: "s", Provider: "claude-native", OrchestrationTools: yes, ChildPolicy: "profiles", ChildProfileIDs: []int64{999}})
	wantErr(t, err, "not found")
	_, err = r.Create(store.AgentProfile{Name: "s", Provider: "claude-native", OrchestrationTools: yes, ChildPolicy: "profiles", ChildProfileIDs: []int64{child.ID, child.ID}})
	wantErr(t, err, "duplicate child")
	for _, pol := range []string{"none", "leaf"} {
		_, err = r.Create(store.AgentProfile{Name: "s", Provider: "claude-native", OrchestrationTools: yes, ChildPolicy: pol, ChildProfileIDs: []int64{child.ID}})
		wantErr(t, err, "childProfileIds requires childPolicy profiles")
	}
	_, err = r.Create(store.AgentProfile{Name: "s", Provider: "claude-native", ChildPolicy: "bogus"})
	wantErr(t, err, "invalid childPolicy")

	sup := mustCreate(t, r, store.AgentProfile{Name: "s", Provider: "claude-native", OrchestrationTools: yes, ChildPolicy: "profiles", ChildProfileIDs: []int64{child.ID}})
	sup.ChildProfileIDs = []int64{sup.ID}
	_, err = r.Update(sup)
	wantErr(t, err, "itself")
	mustCreate(t, r, store.AgentProfile{Name: "lead", Provider: "claude-native", OrchestrationTools: yes, ChildPolicy: "leaf"})
}

func TestProfile_ContractRoundTrip(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	child := mustCreate(t, r, store.AgentProfile{Name: "c", Provider: "claude-native"})
	created := mustCreate(t, r, store.AgentProfile{
		Name: "sup", Provider: "claude-native", OrchestrationTools: boolPtr(true), Instructions: "coordinate",
		Skills: []string{"smind-leader"}, ChildPolicy: "profiles", ChildProfileIDs: []int64{child.ID},
	})
	got, err := r.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.OrchestrationTools == nil || !*got.OrchestrationTools || got.Instructions != "coordinate" ||
		!reflect.DeepEqual(got.Skills, []string{"smind-leader"}) || got.ChildPolicy != "profiles" ||
		!reflect.DeepEqual(got.ChildProfileIDs, []int64{child.ID}) {
		t.Fatalf("round trip = %+v", got)
	}
	got.OrchestrationTools, got.Skills, got.ChildPolicy, got.ChildProfileIDs = boolPtr(false), nil, "", nil
	up, err := r.Update(got)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if up.ChildPolicy != "none" || *up.OrchestrationTools || len(up.Skills) != 0 {
		t.Fatalf("updated = %+v", up)
	}
}
