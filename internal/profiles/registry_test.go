package profiles

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return New(s)
}

func TestRegistry_CreateRejectsEmptyName(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.AgentProfile{Name: "  ", Provider: "claude-native"}); err == nil {
		t.Fatalf("Create() with empty name error = nil, want error")
	}
}

func TestRegistry_CreateRejectsUnknownProvider(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.AgentProfile{Name: "x", Provider: "not-a-real-provider"}); err == nil {
		t.Fatalf("Create() with unknown provider error = nil, want error")
	}
}

func TestRegistry_CreateRejectsInvalidApprovalPolicy(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.AgentProfile{Name: "x", Provider: "claude-native", ApprovalPolicy: "not-a-policy"}); err == nil {
		t.Fatalf("Create() with invalid approvalPolicy error = nil, want error")
	}
}

func TestRegistry_CreateRejectsInvalidThinkingLevel(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.AgentProfile{Name: "x", Provider: "claude-native", ThinkingLevel: "not-a-level"}); err == nil {
		t.Fatalf("Create() with invalid thinkingLevel error = nil, want error")
	}
}

func TestRegistry_CreateAcceptsOmittedApprovalPolicyAndThinkingLevel(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	p, err := r.Create(store.AgentProfile{Name: "x", Provider: "claude-native"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if p.ApprovalPolicy != "" || p.ThinkingLevel != "" {
		t.Fatalf("Create() = %+v, want empty approvalPolicy/thinkingLevel preserved", p)
	}
}

func TestRegistry_UpdateAppliesSameValidation(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	created, err := r.Create(store.AgentProfile{Name: "x", Provider: "claude-native"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := r.Update(store.AgentProfile{ID: created.ID, Name: "x", Provider: "not-a-real-provider"}); err == nil {
		t.Fatalf("Update() with unknown provider error = nil, want error")
	}
}

// fakeNotifier records every notification it receives, for the tests
// below -- mirrors internal/runs.Registry's own notifier test doubles.
type fakeNotifier struct {
	created []store.AgentProfile
	updated []store.AgentProfile
	deleted []int64
}

func (f *fakeNotifier) NotifyProfileCreated(p store.AgentProfile) { f.created = append(f.created, p) }
func (f *fakeNotifier) NotifyProfileUpdated(p store.AgentProfile) { f.updated = append(f.updated, p) }
func (f *fakeNotifier) NotifyProfileDeleted(id int64)             { f.deleted = append(f.deleted, id) }

func TestRegistry_NotifierFiresOnMutation(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	n := &fakeNotifier{}
	r.SetNotifier(n)

	created, err := r.Create(store.AgentProfile{Name: "x", Provider: "claude-native"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(n.created) != 1 || n.created[0].ID != created.ID {
		t.Fatalf("NotifyProfileCreated calls = %+v, want one call for %+v", n.created, created)
	}

	if _, err := r.Update(store.AgentProfile{ID: created.ID, Name: "y", Provider: "glm"}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(n.updated) != 1 || n.updated[0].ID != created.ID {
		t.Fatalf("NotifyProfileUpdated calls = %+v, want one call for id %d", n.updated, created.ID)
	}

	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(n.deleted) != 1 || n.deleted[0] != created.ID {
		t.Fatalf("NotifyProfileDeleted calls = %+v, want one call for id %d", n.deleted, created.ID)
	}
}

func TestRegistry_NilNotifierIsNoop(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	// No SetNotifier call -- Create/Update/Delete must not panic.
	created, err := r.Create(store.AgentProfile{Name: "x", Provider: "claude-native"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := r.Update(store.AgentProfile{ID: created.ID, Name: "y", Provider: "claude-native"}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestRegistry_NilRegistrySetNotifierIsNoop(t *testing.T) {
	t.Parallel()
	var r *Registry
	r.SetNotifier(&fakeNotifier{}) // must not panic
}
