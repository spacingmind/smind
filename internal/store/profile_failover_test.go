package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_ProfileFailoverColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-failover.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	for _, q := range []string{
		`ALTER TABLE agent_profiles DROP COLUMN on_rate_limit`,
		`ALTER TABLE agent_profiles DROP COLUMN fallback_profile_id`,
		`ALTER TABLE runs DROP COLUMN profile_id`,
		`ALTER TABLE runs DROP COLUMN failover_of`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(
		`INSERT INTO runs (id, task_id, chat_id, provider, prompt, status, started_at) VALUES ('old-run', ?, ?, 'glm', 'hi', 'done', ?)`,
		task.ID, chat.ID, now,
	); err != nil {
		t.Fatalf("seed old run: %v", err)
	}
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
		prof, err := s.GetAgentProfile(1)
		if err != nil {
			t.Fatalf("GetAgentProfile() error = %v", err)
		}
		if prof.OnRateLimit != "none" || prof.FallbackProfileID != nil {
			t.Fatalf("migrated profile = %q / %v, want none / nil", prof.OnRateLimit, prof.FallbackProfileID)
		}
		run, err := s.GetRun("old-run")
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if run.ProfileID != 0 || run.FailoverOf != "" {
			t.Fatalf("migrated run = %d / %q, want 0 / empty", run.ProfileID, run.FailoverOf)
		}
		s.Close()
	}
}

func TestStore_AgentProfileFailoverRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	other, err := s.CreateAgentProfile(AgentProfile{Name: "b", Provider: "codex"})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if other.OnRateLimit != "none" {
		t.Fatalf("default OnRateLimit = %q, want none", other.OnRateLimit)
	}
	p, err := s.CreateAgentProfile(AgentProfile{Name: "a", Provider: "glm", OnRateLimit: "fallback", FallbackProfileID: &other.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetAgentProfile(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OnRateLimit != "fallback" || got.FallbackProfileID == nil || *got.FallbackProfileID != other.ID {
		t.Fatalf("got %q / %v", got.OnRateLimit, got.FallbackProfileID)
	}
	got.OnRateLimit, got.FallbackProfileID = "wait", nil
	if _, err := s.UpdateAgentProfile(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := s.ListAgentProfiles()
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if list[1].OnRateLimit != "wait" || list[1].FallbackProfileID != nil {
		t.Fatalf("listed %q / %v", list[1].OnRateLimit, list[1].FallbackProfileID)
	}
}

func TestStore_DeleteProfileClearsFallbackReferences(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	x, _ := s.CreateAgentProfile(AgentProfile{Name: "x", Provider: "codex"})
	fb, err := s.CreateAgentProfile(AgentProfile{Name: "fb", Provider: "glm", OnRateLimit: "fallback", FallbackProfileID: &x.ID})
	if err != nil {
		t.Fatalf("create fb: %v", err)
	}
	wait, err := s.CreateAgentProfile(AgentProfile{Name: "wait", Provider: "glm", OnRateLimit: "wait"})
	if err != nil {
		t.Fatalf("create wait: %v", err)
	}
	if err := s.DeleteAgentProfile(x.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := s.GetAgentProfile(fb.ID)
	if got.FallbackProfileID != nil || got.OnRateLimit != "none" {
		t.Fatalf("fb = %q / %v, want none / nil", got.OnRateLimit, got.FallbackProfileID)
	}
	got, _ = s.GetAgentProfile(wait.ID)
	if got.OnRateLimit != "wait" {
		t.Fatalf("wait profile changed to %q", got.OnRateLimit)
	}
}

func TestStore_RunProfileIDRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "")
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	prof, _ := s.CreateAgentProfile(AgentProfile{Name: "p", Provider: "glm"})
	now := time.Now().UTC()
	base := Run{TaskID: task.ID, ChatID: chat.ID, Provider: "glm", Prompt: "hi", Status: "running", StartedAt: now}

	with := base
	with.ID, with.ProfileID, with.FailoverOf = "r-with", prof.ID, "r-prev"
	without := base
	without.ID = "r-without"
	for _, r := range []Run{with, without} {
		if _, err := s.CreateRun(r); err != nil {
			t.Fatalf("CreateRun(%s): %v", r.ID, err)
		}
	}
	got, _ := s.GetRun("r-with")
	if got.ProfileID != prof.ID || got.FailoverOf != "r-prev" {
		t.Fatalf("with = %d / %q", got.ProfileID, got.FailoverOf)
	}
	got, _ = s.GetRun("r-without")
	if got.ProfileID != 0 || got.FailoverOf != "" {
		t.Fatalf("without = %d / %q", got.ProfileID, got.FailoverOf)
	}
	var isNull bool
	if err := s.db.QueryRow(`SELECT profile_id IS NULL FROM runs WHERE id = 'r-without'`).Scan(&isNull); err != nil || !isNull {
		t.Fatalf("profile_id IS NULL = %v, %v", isNull, err)
	}
	// Deleting the profile leaves the run's history untouched.
	if err := s.DeleteAgentProfile(prof.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = s.GetRun("r-with")
	if got.ProfileID != prof.ID {
		t.Fatalf("profile_id after delete = %d, want %d", got.ProfileID, prof.ID)
	}
}
