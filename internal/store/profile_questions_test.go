package store

import "testing"

func TestAgentProfile_QuestionsDefaultsToAsk(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	p, err := s.CreateAgentProfile(AgentProfile{Name: "plain", Provider: "glm"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Questions != "ask" {
		t.Fatalf("created Questions = %q, want ask", p.Questions)
	}
	got, err := s.GetAgentProfile(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Questions != "ask" {
		t.Fatalf("stored Questions = %q, want ask", got.Questions)
	}
	p.Questions = ""
	if _, err := s.UpdateAgentProfile(p); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetAgentProfile(p.ID)
	if got.Questions != "ask" {
		t.Fatalf("Questions after empty update = %q, want ask", got.Questions)
	}
}

func TestAgentProfile_QuestionsRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	for _, v := range []string{"ask", "decline", "disabled"} {
		p, err := s.CreateAgentProfile(AgentProfile{Name: "p-" + v, Provider: "glm", Questions: v})
		if err != nil {
			t.Fatalf("create %s: %v", v, err)
		}
		got, err := s.GetAgentProfile(p.ID)
		if err != nil {
			t.Fatalf("get %s: %v", v, err)
		}
		if got.Questions != v {
			t.Fatalf("got Questions = %q, want %q", got.Questions, v)
		}
		next := map[string]string{"ask": "decline", "decline": "disabled", "disabled": "ask"}[v]
		got.Questions = next
		if _, err := s.UpdateAgentProfile(got); err != nil {
			t.Fatalf("update %s -> %s: %v", v, next, err)
		}
		got, _ = s.GetAgentProfile(p.ID)
		if got.Questions != next {
			t.Fatalf("Questions after update = %q, want %q", got.Questions, next)
		}
	}
	list, err := s.ListAgentProfiles()
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %d profiles, %v; want 3", len(list), err)
	}
	for _, p := range list {
		if p.Questions == "" {
			t.Fatalf("profile %q listed with empty Questions", p.Name)
		}
	}
}

// An update that does not name the setting keeps the stored one: a profile
// that declines questions never silently goes back to asking.
func TestAgentProfile_UpdateKeepsQuestionsWhenOmitted(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	p, err := s.CreateAgentProfile(AgentProfile{Name: "peer", Provider: "glm", Questions: "decline"})
	if err != nil {
		t.Fatal(err)
	}
	p.Questions = ""
	p.Notes = "edited by an older client"
	if _, err := s.UpdateAgentProfile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgentProfile(p.ID)
	if got.Questions != "decline" || got.Notes != "edited by an older client" {
		t.Fatalf("after update: questions=%q notes=%q", got.Questions, got.Notes)
	}
}

// ADR-0055 D4: ACP elicitation is an explicit per-profile opt-in. A profile
// created without it (every profile that existed before) reads false, and the
// flag round-trips through create and update.
func TestAgentProfile_AgentQuestionsDefaultsOffAndRoundTrips(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	p, err := s.CreateAgentProfile(AgentProfile{Name: "old", Provider: "glm"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAgentProfile(p.ID); got.AgentQuestions || got.Questions != "ask" {
		t.Fatalf("default profile: agentQuestions=%v questions=%q; want false/ask", got.AgentQuestions, got.Questions)
	}
	p.AgentQuestions = true
	if _, err := s.UpdateAgentProfile(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAgentProfile(p.ID); !got.AgentQuestions {
		t.Fatal("opt-in did not persist")
	}
	list, _ := s.ListAgentProfiles()
	if len(list) != 1 || !list[0].AgentQuestions {
		t.Fatalf("list: %+v", list)
	}
}
