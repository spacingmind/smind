package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMigrate_ProfileContractColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-contract.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, c := range []string{"orchestration_tools", "instructions", "skills", "child_policy", "child_profile_ids"} {
		if _, err := s.db.Exec(`ALTER TABLE agent_profiles DROP COLUMN ` + c); err != nil {
			t.Fatalf("drop %s: %v", c, err)
		}
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(
		`INSERT INTO agent_profiles (id, name, provider, created_at, updated_at) VALUES (1, 'old', 'glm', ?, ?)`, now, now,
	); err != nil {
		t.Fatalf("seed old profile: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for i := 0; i < 2; i++ { // second Open proves idempotence
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		p, err := s.GetAgentProfile(1)
		if err != nil {
			t.Fatalf("GetAgentProfile() error = %v", err)
		}
		if p.OrchestrationTools != nil || p.Instructions != "" || len(p.Skills) != 0 || p.ChildPolicy != "" || len(p.ChildProfileIDs) != 0 {
			t.Fatalf("migrated profile = %+v, want an empty contract", p)
		}
		s.Close()
	}
}

func TestStore_ProfileContractRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	yes, no := true, false
	p, err := s.CreateAgentProfile(AgentProfile{
		Name: "lead", Provider: "glm", OrchestrationTools: &yes, Instructions: "lead the work",
		Skills: []string{"smind-leader", "other"}, ChildPolicy: "profiles", ChildProfileIDs: []int64{3, 4},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetAgentProfile(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrchestrationTools == nil || !*got.OrchestrationTools || got.Instructions != "lead the work" ||
		!reflect.DeepEqual(got.Skills, []string{"smind-leader", "other"}) || got.ChildPolicy != "profiles" ||
		!reflect.DeepEqual(got.ChildProfileIDs, []int64{3, 4}) {
		t.Fatalf("got %+v", got)
	}

	got.OrchestrationTools, got.Instructions, got.Skills, got.ChildPolicy, got.ChildProfileIDs = &no, "", nil, "none", nil
	if _, err := s.UpdateAgentProfile(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := s.ListAgentProfiles()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	l := list[0]
	if l.OrchestrationTools == nil || *l.OrchestrationTools || l.Instructions != "" || len(l.Skills) != 0 || l.ChildPolicy != "none" || len(l.ChildProfileIDs) != 0 {
		t.Fatalf("listed %+v", l)
	}
	var skills, ids string
	if err := s.db.QueryRow(`SELECT skills, child_profile_ids FROM agent_profiles WHERE id = ?`, p.ID).Scan(&skills, &ids); err != nil || skills != "" || ids != "" {
		t.Fatalf("empty lists stored as %q / %q, %v", skills, ids, err)
	}
}

func TestStore_DeleteProfileRemovesChildReference(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	yes := true
	a, _ := s.CreateAgentProfile(AgentProfile{Name: "a", Provider: "glm"})
	b, _ := s.CreateAgentProfile(AgentProfile{Name: "b", Provider: "glm"})
	both, err := s.CreateAgentProfile(AgentProfile{Name: "both", Provider: "glm", OrchestrationTools: &yes, ChildPolicy: "profiles", ChildProfileIDs: []int64{a.ID, b.ID}})
	if err != nil {
		t.Fatalf("create both: %v", err)
	}
	only, err := s.CreateAgentProfile(AgentProfile{Name: "only", Provider: "glm", OrchestrationTools: &yes, ChildPolicy: "profiles", ChildProfileIDs: []int64{a.ID}})
	if err != nil {
		t.Fatalf("create only: %v", err)
	}
	if err := s.DeleteAgentProfile(a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := s.GetAgentProfile(both.ID)
	if got.ChildPolicy != "profiles" || !reflect.DeepEqual(got.ChildProfileIDs, []int64{b.ID}) {
		t.Fatalf("both = %q / %v, want profiles / [%d]", got.ChildPolicy, got.ChildProfileIDs, b.ID)
	}
	got, _ = s.GetAgentProfile(only.ID)
	if got.ChildPolicy != "none" || len(got.ChildProfileIDs) != 0 {
		t.Fatalf("only = %q / %v, want none / empty", got.ChildPolicy, got.ChildProfileIDs)
	}
}
