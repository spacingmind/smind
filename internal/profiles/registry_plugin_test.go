package profiles

import (
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// fakeOverlay is a plugin overlay with one owned profile.
type fakeOverlay struct {
	profiles []store.AgentProfile
}

func (o *fakeOverlay) OwnedProfile(id int64) (store.AgentProfile, bool) {
	for _, p := range o.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return store.AgentProfile{}, false
}
func (o *fakeOverlay) OwnedProfiles() []store.AgentProfile { return o.profiles }
func (o *fakeOverlay) OwnedMcp() []store.McpServer         { return nil }
func (o *fakeOverlay) OwnedMcpForRun() []store.McpServer   { return nil }

type fakeSkills map[string]string // "plugin:skill" -> plugin

func (f fakeSkills) PluginSkill(q string) (string, bool) { p, ok := f[q]; return p, ok }
func (f fakeSkills) PluginActive(id string) bool {
	for _, p := range f {
		if p == id {
			return true
		}
	}
	return false
}

func TestProfileSkills_AcceptsPluginNamespace(t *testing.T) {
	r := newTestRegistry(t)
	r.SetSkillResolver(fakeSkills{"acme:review": "acme"})

	p, err := r.Create(store.AgentProfile{Name: "s", Provider: "claude-native", Skills: []string{"acme:review"}})
	if err != nil || len(p.Skills) != 1 || p.Skills[0] != "acme:review" {
		t.Fatalf("a profile may name an active plugin's skill: %+v %v", p, err)
	}
	// An unknown plugin skill is a clear error naming it.
	_, err = r.Create(store.AgentProfile{Name: "t", Provider: "claude-native", Skills: []string{"acme:nope"}})
	if err == nil || !strings.Contains(err.Error(), `"acme:nope"`) || !strings.Contains(err.Error(), "no skill") {
		t.Fatalf("unknown skill of an active plugin: %v", err)
	}
	_, err = r.Create(store.AgentProfile{Name: "u", Provider: "claude-native", Skills: []string{"ghost:thing"}})
	if err == nil || !strings.Contains(err.Error(), `"ghost:thing"`) || !strings.Contains(err.Error(), "not installed and enabled") {
		t.Fatalf("skill of an unknown plugin: %v", err)
	}
	// The plugin is disabled later: the stored profile still loads.
	r.SetSkillResolver(fakeSkills{})
	got, err := r.Get(p.ID)
	if err != nil || got.Skills[0] != "acme:review" {
		t.Fatalf("the profile must still load once its plugin is off: %+v %v", got, err)
	}
	if _, err := r.List(); err != nil {
		t.Fatal(err)
	}
	// And with no resolver wired, a plugin skill is rejected, not accepted blindly.
	r2 := newTestRegistry(t)
	if _, err := r2.Create(store.AgentProfile{Name: "v", Provider: "claude-native", Skills: []string{"acme:review"}}); err == nil {
		t.Fatal("no resolver: plugin skill names must be rejected")
	}
}

func TestOwnedProfiles_ReadOnly_ForEveryCaller(t *testing.T) {
	r := newTestRegistry(t)
	owner := "acme"
	owned := store.AgentProfile{ID: store.OwnedIDBase + 7, Name: "acme:boss", Provider: "claude-native", Owner: &owner, ReadOnly: true}
	o := &fakeOverlay{profiles: []store.AgentProfile{owned}}
	r.store.SetOverlay(o)
	user := mustCreate(t, r, store.AgentProfile{Name: "mine", Provider: "claude-native"})

	list, _ := r.List()
	if len(list) != 2 || list[0].ID != user.ID || list[1].Name != "acme:boss" || list[1].Owner == nil {
		t.Fatalf("list = user rows then owned: %+v", list)
	}
	got, err := r.Get(owned.ID)
	if err != nil || got.Name != "acme:boss" {
		t.Fatalf("get owned: %+v %v", got, err)
	}

	var oe *store.OwnedRowError
	if _, err := r.Update(store.AgentProfile{ID: owned.ID, Name: "x", Provider: "claude-native"}); !errors.As(err, &oe) || oe.Plugin != "acme" {
		t.Fatalf("update of an owned profile: %v", err)
	}
	if err := r.Delete(owned.ID); !errors.As(err, &oe) || oe.Plugin != "acme" {
		t.Fatalf("delete of an owned profile: %v", err)
	}
	// A user row can never take an owned name, nor be created with a forged owner.
	if _, err := r.Create(store.AgentProfile{Name: "acme:boss", Provider: "claude-native"}); err == nil || !strings.Contains(err.Error(), "acme") {
		t.Fatalf("a name taken by a plugin profile must be rejected: %v", err)
	}
	if _, err := r.Update(store.AgentProfile{ID: user.ID, Name: "acme:boss", Provider: "claude-native"}); err == nil {
		t.Fatal("renaming a user row onto an owned name must be rejected")
	}
	forged := "acme"
	made := mustCreate(t, r, store.AgentProfile{Name: "forged", Provider: "claude-native", Owner: &forged, ReadOnly: true})
	if made.Owner != nil || made.ReadOnly {
		t.Fatalf("a caller cannot mark its own row owned: %+v", made)
	}
	// A stored profile cannot lean on a plugin's profile (it vanishes with the plugin).
	_, err = r.Create(store.AgentProfile{Name: "boss2", Provider: "claude-native", OrchestrationTools: boolPtr(true), ChildPolicy: "profiles", ChildProfileIDs: []int64{owned.ID}})
	wantErr(t, err, "belongs to a plugin")
	fb := owned.ID
	_, err = r.Create(store.AgentProfile{Name: "fb", Provider: "codex-native", OnRateLimit: "fallback", FallbackProfileID: &fb})
	wantErr(t, err, "belongs to a plugin")
}
