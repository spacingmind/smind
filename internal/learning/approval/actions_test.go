package approval

import (
	"errors"
	"slices"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

func TestVersions_UndoRestoresAsNewVersion(t *testing.T) {
	f := newFixture(t)
	k := f.land(skillA)
	v2, err := f.st.PatchSkill(store.PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: skillA.Description, Body: "Totally different body text.", Actor: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Undo(k.ID, v2.Version, "human"); !errors.Is(err, ErrUndoCurrent) {
		t.Errorf("undo current err = %v", err)
	}
	if _, err := f.svc.Undo(k.ID, 9, "human"); !errors.Is(err, ErrNoSuchVersion) {
		t.Errorf("undo missing err = %v", err)
	}
	got, err := f.svc.Undo(k.ID, 1, "human")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || got.Body != skillA.Body || got.Description != skillA.Description {
		t.Errorf("undone = %+v", got)
	}
	vs, _ := f.st.ListSkillVersions(k.ID)
	if len(vs) != 3 || vs[0].Body != skillA.Body {
		t.Errorf("versions = %+v", vs)
	}
	if acts := f.ledgerActions(k.ID); acts[len(acts)-1] != "undo" {
		t.Errorf("ledger = %v", acts)
	}
}

func TestUndo_RechecksAgainstCurrentLibrary(t *testing.T) {
	f := newFixture(t)
	k := f.land(skillA)
	if _, err := f.st.PatchSkill(store.PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: skillA.Description, Body: "Totally different body text.", Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	// A new skill now carries v1's text under another name.
	clone := skillA
	clone.Name = "learned-tempdir-clone"
	f.land(clone)
	_, err := f.svc.Undo(k.ID, 1, "human")
	var ge *GateError
	if !errors.As(err, &ge) || ge.Findings[0].Reason != gate.ReasonDuplicate {
		t.Errorf("err = %v, want duplicate GateError", err)
	}
	if cur, _ := f.st.GetLearnedSkill(k.ID); cur.Version != 2 {
		t.Errorf("version = %d, want untouched 2", cur.Version)
	}
}

func TestRestore_NameCollisionRejected(t *testing.T) {
	f := newFixture(t)
	k := f.land(skillA)
	if err := f.svc.Archive(k.ID, "human", "old"); err != nil {
		t.Fatal(err)
	}
	other := skillA
	other.Description, other.Body = "Use when the build cache is cold.", "Warm the cache with a dry run."
	f.land(other) // same name, live
	err := f.svc.Restore(k.ID, "human", "")
	var ge *GateError
	if !errors.As(err, &ge) || ge.Findings[0].Reason != gate.ReasonNameCollision {
		t.Errorf("err = %v, want collision GateError", err)
	}
	if cur, _ := f.st.GetLearnedSkill(k.ID); cur.Status != store.SkillArchived {
		t.Errorf("status = %s, want still archived", cur.Status)
	}

	f2 := newFixture(t)
	k2 := f2.land(skillA)
	_ = f2.svc.Archive(k2.ID, "human", "")
	if err := f2.svc.Restore(k2.ID, "human", "back"); err != nil {
		t.Errorf("clean restore err = %v", err)
	}
}

func TestForgetWorkspace_HardDeletesAll(t *testing.T) {
	f := newFixture(t)
	other := f0(t, f.st, "/ws2")
	f.land(skillA)
	f.propose(skillB)
	if _, err := f.st.LandSkill(store.LandSkillInput{Skill: store.LearnedSkill{WorkspaceID: other.ID, Name: skillC.Name, Description: skillC.Description, Body: skillC.Body, Source: "human"}, Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetLearningMode(f.ws.ID, "propose"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Forget(f.ws.ID); err != nil {
		t.Fatal(err)
	}
	if ks, _ := f.st.ListLearnedSkills(f.ws.ID); len(ks) != 0 {
		t.Errorf("skills left: %+v", ks)
	}
	if ps, _ := f.st.ListProposals(f.ws.ID, ""); len(ps) != 0 {
		t.Errorf("proposals left: %+v", ps)
	}
	if es, _ := f.st.ListLedger(f.ws.ID, 0, 0); len(es) != 0 {
		t.Errorf("ledger left: %+v", es)
	}
	if m, _ := f.st.GetWorkspaceLearningMode(f.ws.ID); m != "off" {
		t.Errorf("mode = %s, want off", m)
	}
	if ks, _ := f.st.ListLearnedSkills(other.ID); len(ks) != 1 {
		t.Errorf("other workspace skills = %+v, want untouched", ks)
	}
}

func TestLearningOff_KeepsDataStopsDelivery(t *testing.T) {
	f := newFixture(t)
	f.land(skillA)
	f.propose(skillB)
	if err := f.svc.SetLearningMode(f.ws.ID, "propose"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetLearningMode(f.ws.ID, "off"); err != nil {
		t.Fatal(err)
	}
	if m, _ := f.st.GetWorkspaceLearningMode(f.ws.ID); m != "off" {
		t.Errorf("mode = %s", m)
	}
	if ks, _ := f.st.ListLearnedSkills(f.ws.ID); len(ks) != 1 {
		t.Errorf("skills = %+v, want kept", ks)
	}
	if ps, _ := f.st.ListProposals(f.ws.ID, "pending"); len(ps) != 1 {
		t.Errorf("proposals = %+v, want kept", ps)
	}
	if err := f.svc.SetLearningMode(f.ws.ID, "bogus"); err == nil {
		t.Error("bogus mode accepted")
	}
}

func TestPinDisableArchive_LedgerActions(t *testing.T) {
	f := newFixture(t)
	k := f.land(skillA)
	steps := []func() error{
		func() error { return f.svc.Pin(k.ID, "") },
		func() error { return f.svc.Unpin(k.ID, "") },
		func() error { return f.svc.Disable(k.ID, "") },
		func() error { return f.svc.Enable(k.ID, "") },
		func() error { return f.svc.Archive(k.ID, "", "stale") },
		func() error { return f.svc.Restore(k.ID, "", "needed") },
		func() error { return f.svc.Delete(k.ID, "") },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	want := []string{"create", "pin", "unpin", "disable", "enable", "archive", "restore", "delete"}
	if got := f.ledgerActions(k.ID); !slices.Equal(got, want) {
		t.Errorf("ledger = %v, want %v", got, want)
	}
	if _, err := f.st.GetLearnedSkill(k.ID); err == nil {
		t.Error("skill still exists after Delete")
	}
	es, _ := f.st.ListLedger(f.ws.ID, k.ID, 0)
	if len(es) != len(want) || es[0].Actor != "human" {
		t.Errorf("ledger rows = %d, newest actor %q", len(es), es[0].Actor)
	}
}
