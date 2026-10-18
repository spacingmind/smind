package store

import (
	"database/sql"
	"errors"
	"testing"
)

func newLearningWorkspace(t *testing.T, s *Store) Workspace {
	t.Helper()
	return newTestWorkspace(t, s, "/learn")
}

func mustLand(t *testing.T, s *Store, wsID int64, name string) LearnedSkill {
	t.Helper()
	k, err := s.LandSkill(LandSkillInput{
		Skill: LearnedSkill{WorkspaceID: wsID, Name: name, Description: "d-" + name, Body: "b-" + name, Source: "human"},
		Actor: "human",
	})
	if err != nil {
		t.Fatalf("LandSkill(%q) error = %v", name, err)
	}
	return k
}

func countRows(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

func TestLandSkill_AtomicAllOrNothing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)

	if _, err := s.LandSkill(LandSkillInput{
		Skill: LearnedSkill{WorkspaceID: ws.ID, Name: "dup", Description: "d", Body: "b"},
		Actor: "human",
	}); err != nil {
		t.Fatal(err)
	}

	// Same live name: error, and nothing (skill/version/ledger) written.
	if _, err := s.LandSkill(LandSkillInput{
		Skill: LearnedSkill{WorkspaceID: ws.ID, Name: "dup", Description: "d", Body: "b"},
		Actor: "human",
	}); err == nil {
		t.Fatal("duplicate live name: error = nil, want error")
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skills`); got != 1 {
		t.Fatalf("skills = %d, want 1", got)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_versions`); got != 1 {
		t.Fatalf("versions = %d, want 1", got)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_ledger`); got != 1 {
		t.Fatalf("ledger = %d, want 1", got)
	}

	// Non-pending proposal id: error, nothing written (not even a ledger row).
	if _, err := s.LandSkill(LandSkillInput{
		Skill:      LearnedSkill{WorkspaceID: ws.ID, Name: "fresh", Description: "d", Body: "b"},
		Actor:      "human",
		ProposalID: 9999,
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing proposal error = %v, want sql.ErrNoRows", err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skills`); got != 1 {
		t.Fatalf("skills after failed proposal = %d, want 1", got)
	}

	// A pending proposal lands and is approved in the same transaction.
	p, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "create", Source: "reflector", Name: "from-proposal"})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.LandSkill(LandSkillInput{
		Skill:      LearnedSkill{WorkspaceID: ws.ID, Name: "from-proposal", Description: "d", Body: "b"},
		Actor:      "human",
		Action:     "approve",
		ProposalID: p.ID,
	})
	if err != nil {
		t.Fatalf("LandSkill with proposal: %v", err)
	}
	if k.Status != SkillActive || k.Version != 1 {
		t.Fatalf("landed = %+v", k)
	}
	got, err := s.GetProposal(p.ID)
	if err != nil || got.Status != "approved" || got.DecidedAt == nil {
		t.Fatalf("proposal = %+v err=%v, want approved with decided_at", got, err)
	}
}

func TestPatchSkill_VersionAndConflict(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	k := mustLand(t, s, ws.ID, "sk")

	patched, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: "d2", Body: "b2", Actor: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Version != 2 || patched.Patched != 1 || patched.Description != "d2" || patched.Body != "b2" {
		t.Fatalf("patched = %+v", patched)
	}

	if _, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: "x", Body: "x", Actor: "human"}); !errors.Is(err, ErrSkillVersionConflict) {
		t.Fatalf("stale version error = %v, want ErrSkillVersionConflict", err)
	}
	// Nothing was written by the failed patch.
	after, _ := s.GetLearnedSkill(k.ID)
	if after.Version != 2 || after.Description != "d2" {
		t.Fatalf("skill after conflict = %+v", after)
	}

	// Non-pending proposal rolls the whole patch back.
	if _, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 2, Description: "x", Body: "x", Actor: "human", ProposalID: 4242}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("bad proposal error = %v, want sql.ErrNoRows", err)
	}
	after, _ = s.GetLearnedSkill(k.ID)
	if after.Version != 2 || after.Description != "d2" {
		t.Fatalf("skill after failed-proposal patch = %+v", after)
	}
}

func TestVersions_UndoRestoresAsNewVersion(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	k := mustLand(t, s, ws.ID, "sk") // v1: "b-sk"
	if _, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: "d2", Body: "b2", Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 2, Description: "d3", Body: "b3", Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	undo, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 3, Description: "d-sk", Body: "b-sk", Actor: "human", Action: "undo"})
	if err != nil {
		t.Fatal(err)
	}
	if undo.Version != 4 || undo.Body != "b-sk" {
		t.Fatalf("undo = version %d body %q, want 4 / b-sk", undo.Version, undo.Body)
	}
	versions, err := s.ListSkillVersions(k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 4 {
		t.Fatalf("versions = %d, want 4", len(versions))
	}
	for i, v := range versions {
		if v.Version != 4-i {
			t.Fatalf("versions not newest-first: %+v at %d", v, i)
		}
	}
}

func TestLedger_MergeVsRetire(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)

	merged := mustLand(t, s, ws.ID, "merged")
	if err := s.SetSkillStatus(merged.ID, SkillArchived, "curator", "folded into learned-x", "learned-x"); err != nil {
		t.Fatal(err)
	}
	retired := mustLand(t, s, ws.ID, "retired")
	if err := s.SetSkillStatus(retired.ID, SkillArchived, "curator", "stale", ""); err != nil {
		t.Fatal(err)
	}

	ledger, err := s.ListLedger(ws.ID, merged.ID, 0)
	if err != nil || len(ledger) == 0 {
		t.Fatalf("ledger = %v err=%v", ledger, err)
	}
	if ledger[0].Action != "archive" || ledger[0].AbsorbedInto != "learned-x" {
		t.Fatalf("merge ledger = %+v, want archive with absorbed_into learned-x", ledger[0])
	}
	ledger, err = s.ListLedger(ws.ID, retired.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ledger[0].Action != "archive" || ledger[0].AbsorbedInto != "" {
		t.Fatalf("retire ledger = %+v, want archive with empty absorbed_into", ledger[0])
	}

	// Whole-workspace list includes both; restore/disable/enable transitions.
	if err := s.SetSkillStatus(retired.ID, SkillActive, "human", "restore", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillStatus(retired.ID, SkillDisabled, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillStatus(retired.ID, SkillActive, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListLedger(ws.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantActions := []string{"enable", "disable", "restore", "archive", "create"}
	for i, want := range wantActions {
		if all[i].Action != want {
			t.Fatalf("ledger[%d].action = %q, want %q", i, all[i].Action, want)
		}
	}
	// Invalid transition and unknown status are rejected.
	if err := s.SetSkillStatus(merged.ID, SkillDisabled, "human", "", ""); err == nil {
		t.Fatal("archived -> disabled: error = nil, want error")
	}
	if err := s.SetSkillStatus(retired.ID, "bogus", "human", "", ""); err == nil {
		t.Fatal("unknown status: error = nil, want error")
	}
}

func TestProposals_DecideOnlyFromPending(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	p, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "create", Source: "reflector", Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "create", Source: "reflector", Name: "b"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DecideProposal(p.ID, "bogus", "", "human"); err == nil {
		t.Fatal("unknown decision: error = nil, want error")
	}
	if err := s.DecideProposal(p.ID, "rejected", "nope", "human"); err != nil {
		t.Fatal(err)
	}
	// Second decision on the same proposal fails and changes nothing.
	if err := s.DecideProposal(p.ID, "approved", "", "human"); err == nil {
		t.Fatal("re-decide: error = nil, want error")
	}
	got, _ := s.GetProposal(p.ID)
	if got.Status != "rejected" || got.Note != "nope" || got.DecidedAt == nil {
		t.Fatalf("decided proposal = %+v", got)
	}

	pending, err := s.ListProposals(ws.ID, "pending")
	if err != nil || len(pending) != 1 || pending[0].Name != "b" {
		t.Fatalf("pending = %+v err=%v", pending, err)
	}
	rejected, err := s.ListRejectedProposals(ws.ID, 10)
	if err != nil || len(rejected) != 1 || rejected[0].ID != p.ID {
		t.Fatalf("rejected = %+v err=%v", rejected, err)
	}

	// Rejection appended a ledger row with the proposal's name, no skill id.
	ledger, err := s.ListLedger(ws.ID, 0, 0)
	if err != nil || len(ledger) != 1 {
		t.Fatalf("ledger = %+v err=%v", ledger, err)
	}
	if ledger[0].Action != "reject" || ledger[0].SkillName != "a" || ledger[0].SkillID != nil || ledger[0].Reason != "nope" {
		t.Fatalf("reject ledger = %+v", ledger[0])
	}
}

func TestApplyBundle_AtomicAllOrNothing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws1 := newLearningWorkspace(t, s)
	ws2 := newTestWorkspace(t, s, "/other")

	umbrella := mustLand(t, s, ws1.ID, "umbrella")
	absorbed := mustLand(t, s, ws1.ID, "absorbed")
	foreign := mustLand(t, s, ws2.ID, "foreign")

	p, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws1.ID, Kind: "bundle", Source: "curator", Name: "umbrella", TargetSkillID: &umbrella.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Absorbing a skill from another workspace fails and rolls back the
	// umbrella patch and the proposal approval.
	bad := ApplyBundleInput{
		ProposalID: p.ID, UmbrellaID: umbrella.ID, ExpectVersion: 1,
		Description: "merged", Body: "merged-body", Actor: "curator",
		AbsorbedIDs: []int64{absorbed.ID, foreign.ID},
	}
	if err := s.ApplyBundle(bad); err == nil {
		t.Fatal("cross-workspace absorb: error = nil, want error")
	}
	after, _ := s.GetLearnedSkill(umbrella.ID)
	if after.Version != 1 || after.Body != "b-umbrella" {
		t.Fatalf("umbrella after failed bundle = %+v", after)
	}
	prop, _ := s.GetProposal(p.ID)
	if prop.Status != "pending" {
		t.Fatalf("proposal after failed bundle = %q, want pending", prop.Status)
	}

	if err := s.ApplyBundle(ApplyBundleInput{
		ProposalID: p.ID, UmbrellaID: umbrella.ID, ExpectVersion: 1,
		Description: "merged", Body: "merged-body", Actor: "curator",
		AbsorbedIDs: []int64{absorbed.ID},
	}); err != nil {
		t.Fatal(err)
	}
	gotUmbrella, _ := s.GetLearnedSkill(umbrella.ID)
	if gotUmbrella.Version != 2 || gotUmbrella.Body != "merged-body" {
		t.Fatalf("umbrella = %+v", gotUmbrella)
	}
	gotAbsorbed, _ := s.GetLearnedSkill(absorbed.ID)
	if gotAbsorbed.Status != SkillArchived {
		t.Fatalf("absorbed status = %q, want archived", gotAbsorbed.Status)
	}
	prop, _ = s.GetProposal(p.ID)
	if prop.Status != "approved" {
		t.Fatalf("proposal = %q, want approved", prop.Status)
	}
	ledger, err := s.ListLedger(ws1.ID, absorbed.ID, 0)
	if err != nil || len(ledger) < 2 || ledger[0].Action != "archive" || ledger[0].AbsorbedInto != "umbrella" {
		t.Fatalf("absorbed ledger = %+v err=%v", ledger, err)
	}
}

func TestJobs_HasActiveJob(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)

	active, err := s.HasActiveLearningJob(ws.ID)
	if err != nil || active {
		t.Fatalf("active = %v err=%v, want false", active, err)
	}
	j, err := s.CreateLearningJob(LearningJob{WorkspaceID: ws.ID, Kind: "reflect", TriggerKind: "tail", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if j.ID == 0 {
		t.Fatal("job ID = 0")
	}
	if active, _ := s.HasActiveLearningJob(ws.ID); !active {
		t.Fatal("queued job not active")
	}
	j.Status = "running"
	if err := s.UpdateLearningJob(j); err != nil {
		t.Fatal(err)
	}
	if active, _ := s.HasActiveLearningJob(ws.ID); !active {
		t.Fatal("running job not active")
	}
	j.Status = "landed"
	j2, err := s.GetLearningJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	j2.Status = "landed"
	j2.FinishedAt = nil
	if err := s.UpdateLearningJob(j2); err != nil {
		t.Fatal(err)
	}
	if active, _ := s.HasActiveLearningJob(ws.ID); active {
		t.Fatal("landed job still active")
	}
	if err := s.UpdateLearningJob(LearningJob{ID: 999, Status: "x"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("update missing job error = %v, want sql.ErrNoRows", err)
	}
	list, err := s.ListLearningJobs(ws.ID, 10)
	if err != nil || len(list) != 1 || list[0].ID != j.ID {
		t.Fatalf("jobs = %+v err=%v", list, err)
	}
}

func TestForgetWorkspace_HardDeletesAll(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws1 := newLearningWorkspace(t, s)
	ws2 := newTestWorkspace(t, s, "/keep")

	for _, ws := range []Workspace{ws1, ws2} {
		k := mustLand(t, s, ws.ID, "sk")
		if _, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: "d2", Body: "b2", Actor: "human"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "patch", Source: "reflector", Name: "sk", TargetSkillID: &k.ID}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateLearningJob(LearningJob{WorkspaceID: ws.ID, Kind: "reflect", TriggerKind: "tail", Status: "landed"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetWorkspaceLearningMode(ws.ID, LearningModePropose); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.ForgetWorkspaceLearning(ws1.ID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM learned_skills WHERE workspace_id = ?`,
		`SELECT COUNT(*) FROM learned_skill_versions v JOIN learned_skills k ON v.skill_id = k.id WHERE k.workspace_id = ?`,
		`SELECT COUNT(*) FROM learned_skill_ledger WHERE workspace_id = ?`,
		`SELECT COUNT(*) FROM learned_skill_proposals WHERE workspace_id = ?`,
		`SELECT COUNT(*) FROM learning_jobs WHERE workspace_id = ?`,
	} {
		if got := countRows(t, s, q, ws1.ID); got != 0 {
			t.Fatalf("%s = %d for forgotten workspace, want 0", q, got)
		}
	}
	if mode, _ := s.GetWorkspaceLearningMode(ws1.ID); mode != LearningModeOff {
		t.Fatalf("learning_mode = %q, want off", mode)
	}
	// The untouched workspace keeps everything.
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skills WHERE workspace_id = ?`, ws2.ID); got != 1 {
		t.Fatalf("kept workspace skills = %d, want 1", got)
	}
	if mode, _ := s.GetWorkspaceLearningMode(ws2.ID); mode != LearningModePropose {
		t.Fatalf("kept workspace learning_mode = %q, want propose", mode)
	}
}

func TestDeleteWorkspace_RemovesLearningRows(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws1 := newLearningWorkspace(t, s)
	ws2 := newTestWorkspace(t, s, "/keep")

	mustLand(t, s, ws1.ID, "gone")
	k := mustLand(t, s, ws2.ID, "stay")
	if _, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws1.ID, Kind: "create", Source: "reflector", Name: "p"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteWorkspace(ws1.ID); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skills`); got != 1 {
		t.Fatalf("skills = %d, want 1", got)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_proposals`); got != 0 {
		t.Fatalf("proposals = %d, want 0", got)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_ledger`); got != 1 {
		t.Fatalf("ledger = %d, want 1", got)
	}
	if _, err := s.GetLearnedSkill(k.ID); err != nil {
		t.Fatalf("kept skill: %v", err)
	}
}

func TestSetWorkspaceLearningMode_RejectsUnknown(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)

	if err := s.SetWorkspaceLearningMode(ws.ID, LearningModePropose); err != nil {
		t.Fatal(err)
	}
	if mode, _ := s.GetWorkspaceLearningMode(ws.ID); mode != LearningModePropose {
		t.Fatalf("mode = %q, want propose", mode)
	}
	if err := s.SetWorkspaceLearningMode(ws.ID, "yolo"); err == nil {
		t.Fatal("unknown mode: error = nil, want error")
	}
	if err := s.SetWorkspaceLearningMode(9999, LearningModeOff); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing workspace error = %v, want sql.ErrNoRows", err)
	}
}

func TestLearnedSkillsPartialUniqueIndexAllowsArchivedDuplicates(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)

	first := mustLand(t, s, ws.ID, "dup")
	if err := s.SetSkillStatus(first.ID, SkillArchived, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	second := mustLand(t, s, ws.ID, "dup") // same name, previous row archived
	if second.Status != SkillActive {
		t.Fatalf("second = %q, want active", second.Status)
	}
	// ...but a second LIVE duplicate still collides.
	if err := s.SetSkillStatus(first.ID, SkillActive, "human", "", ""); err == nil {
		t.Fatal("two live same-name skills: error = nil, want unique violation")
	}
	filtered, err := s.ListLearnedSkills(ws.ID, SkillActive)
	if err != nil || len(filtered) != 1 {
		t.Fatalf("active list = %+v err=%v", filtered, err)
	}
	all, err := s.ListLearnedSkills(ws.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("all list = %+v err=%v", all, err)
	}
}

func TestProposals_UniquePendingCreateName(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)

	if _, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "create", Source: "reflector", Name: "same"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "create", Source: "reflector", Name: "same"}); err == nil {
		t.Fatal("second pending create with same name: error = nil, want unique violation")
	}
	// Rejecting the first frees the name for a new pending proposal.
	pending, _ := s.ListProposals(ws.ID, "pending")
	if err := s.DecideProposal(pending[0].ID, "rejected", "", "human"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProposal(LearnedProposal{WorkspaceID: ws.ID, Kind: "create", Source: "reflector", Name: "same"}); err != nil {
		t.Fatalf("re-propose after reject: %v", err)
	}
}

func TestDeleteLearnedSkill_KeepsLedger(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	k := mustLand(t, s, ws.ID, "sk")

	if err := s.SetSkillPinned(k.ID, true, "human"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLearnedSkill(k.ID, "human"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLearnedSkill(k.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("get deleted skill error = %v, want sql.ErrNoRows", err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_versions`); got != 0 {
		t.Fatalf("versions = %d, want 0", got)
	}
	ledger, err := s.ListLedger(ws.ID, k.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// create, pin, delete -- all retained, skill_id and name intact.
	if len(ledger) != 3 || ledger[0].Action != "delete" || ledger[0].SkillName != "sk" || ledger[0].SkillID == nil {
		t.Fatalf("ledger = %+v, want 3 rows ending in delete with name kept", ledger)
	}
}
