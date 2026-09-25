package store

import "testing"

func TestStore_AgentProfiles(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created, err := s.CreateAgentProfile(AgentProfile{
		Name: "UI work", Provider: "claude-native", ApprovalPolicy: "manual", ThinkingLevel: "standard", Notes: "For UI polish.",
	})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("CreateAgentProfile() ID = 0, want nonzero")
	}

	got, err := s.GetAgentProfile(created.ID)
	if err != nil {
		t.Fatalf("GetAgentProfile() error = %v", err)
	}
	assertAgentProfilesEqual(t, got, created)
}

func TestStore_ListAgentProfilesOrdering(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	first, err := s.CreateAgentProfile(AgentProfile{Name: "first", Provider: "claude-native"})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}
	second, err := s.CreateAgentProfile(AgentProfile{Name: "second", Provider: "glm"})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}

	list, err := s.ListAgentProfiles()
	if err != nil {
		t.Fatalf("ListAgentProfiles() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("ListAgentProfiles() = %+v, want [%+v %+v]", list, first, second)
	}
}

func TestStore_ListAgentProfilesEmpty(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	list, err := s.ListAgentProfiles()
	if err != nil {
		t.Fatalf("ListAgentProfiles() error = %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Fatalf("ListAgentProfiles() = %+v, want empty non-nil slice", list)
	}
}

func TestStore_UpdateAgentProfile(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created, err := s.CreateAgentProfile(AgentProfile{Name: "old", Provider: "claude-native", ApprovalPolicy: "manual"})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}

	updated, err := s.UpdateAgentProfile(AgentProfile{
		ID: created.ID, Name: "new", Provider: "glm", ApprovalPolicy: "auto-safe", ThinkingLevel: "", Notes: "updated",
	})
	if err != nil {
		t.Fatalf("UpdateAgentProfile() error = %v", err)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("UpdateAgentProfile() CreatedAt = %v, want unchanged %v", updated.CreatedAt, created.CreatedAt)
	}
	if updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("UpdateAgentProfile() UpdatedAt unchanged, want a newer timestamp")
	}

	got, err := s.GetAgentProfile(created.ID)
	if err != nil {
		t.Fatalf("GetAgentProfile() error = %v", err)
	}
	if got.Name != "new" || got.Provider != "glm" || got.ApprovalPolicy != "auto-safe" || got.Notes != "updated" {
		t.Fatalf("GetAgentProfile() after update = %+v, want fields replaced", got)
	}
}

func TestStore_UpdateAgentProfileMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if _, err := s.UpdateAgentProfile(AgentProfile{ID: 999, Name: "x", Provider: "claude-native"}); err == nil {
		t.Fatalf("UpdateAgentProfile(missing) error = nil, want error")
	}
}

func TestStore_DeleteAgentProfile(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created, err := s.CreateAgentProfile(AgentProfile{Name: "throwaway", Provider: "claude-native"})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}

	if err := s.DeleteAgentProfile(created.ID); err != nil {
		t.Fatalf("DeleteAgentProfile() error = %v", err)
	}
	if _, err := s.GetAgentProfile(created.ID); err == nil {
		t.Fatalf("GetAgentProfile() after delete error = nil, want not-found error")
	}
}

func TestStore_DeleteAgentProfileMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.DeleteAgentProfile(999); err == nil {
		t.Fatalf("DeleteAgentProfile(missing) error = nil, want error")
	}
}

func assertAgentProfilesEqual(t *testing.T, got, want AgentProfile) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || got.Provider != want.Provider ||
		got.ApprovalPolicy != want.ApprovalPolicy || got.ThinkingLevel != want.ThinkingLevel || got.Notes != want.Notes {
		t.Errorf("agent profile fields = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("agent profile timestamps = %+v, want %+v", got, want)
	}
}
