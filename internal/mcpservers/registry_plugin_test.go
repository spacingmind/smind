package mcpservers

import (
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

type fakeOverlay struct{ list, run []store.McpServer }

func (o *fakeOverlay) OwnedProfile(int64) (store.AgentProfile, bool) {
	return store.AgentProfile{}, false
}
func (o *fakeOverlay) OwnedProfiles() []store.AgentProfile { return nil }
func (o *fakeOverlay) OwnedMcp() []store.McpServer         { return o.list }
func (o *fakeOverlay) OwnedMcpForRun() []store.McpServer   { return o.run }

func TestRegistry_OwnedRows(t *testing.T) {
	r := newTestRegistry(t)
	user, err := r.Create(store.McpServer{Name: "acme__files", Transport: "stdio", Command: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	o := &fakeOverlay{
		list: []store.McpServer{{ID: store.OwnedIDBase + 1, Name: "acme:files", Transport: "stdio", Command: "plug", Enabled: true, OwnerPlugin: "acme", Env: `{"T":"${setting.t}"}`}},
		run:  []store.McpServer{{ID: store.OwnedIDBase + 1, Name: "acme__files", Transport: "stdio", Command: "plug", Enabled: true, OwnerPlugin: "acme"}, {ID: store.OwnedIDBase + 2, Name: "acme__other", Transport: "stdio", Command: "p2", Enabled: true, OwnerPlugin: "acme"}},
	}
	r.store.SetOverlay(o)

	rows, err := r.List()
	if err != nil || len(rows) != 2 || rows[0].ID != user.ID || rows[1].Name != "acme:files" {
		t.Fatalf("List = user rows then owned: %+v %v", rows, err)
	}
	if got, err := r.Get(o.list[0].ID); err != nil || got.OwnerPlugin != "acme" {
		t.Fatalf("Get owned: %+v %v", got, err)
	}
	if _, err := r.Get(store.OwnedIDBase + 99); err == nil {
		t.Fatal("an unknown owned id is not found")
	}

	// A run sees user rows first; an owned server whose run name a user row
	// already holds is left out (user wins), others follow.
	run, err := r.ListForWorkspace(1)
	if err != nil || len(run) != 2 || run[0].Command != "mine" || run[1].Name != "acme__other" {
		t.Fatalf("ListForWorkspace = %+v %v", run, err)
	}
	// The result is a snapshot: the plugin going away later does not reach into it.
	r.store.SetOverlay(&fakeOverlay{})
	if len(run) != 2 {
		t.Fatal("snapshot changed")
	}
	if again, _ := r.ListForWorkspace(1); len(again) != 1 {
		t.Fatalf("the next run sees the new state: %+v", again)
	}

	r.store.SetOverlay(o)
	var oe *store.OwnedRowError
	if _, err := r.Update(store.McpServer{ID: o.list[0].ID, Name: "x", Transport: "stdio", Command: "y"}); !errors.As(err, &oe) || oe.Plugin != "acme" {
		t.Fatalf("Update owned: %v", err)
	}
	if err := r.Delete(o.list[0].ID); !errors.As(err, &oe) {
		t.Fatalf("Delete owned: %v", err)
	}
	if _, err := r.SetEnabled(o.list[0].ID, false); !errors.As(err, &oe) {
		t.Fatalf("SetEnabled owned: %v", err)
	}
	if _, err := r.Create(store.McpServer{Name: "acme:files", Transport: "stdio", Command: "z"}); err == nil || !strings.Contains(err.Error(), "reserved for servers contributed by plugins") {
		t.Fatalf("colon names are reserved: %v", err)
	}
}
