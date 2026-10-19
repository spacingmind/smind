package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshot_BeforeCuratorAndRotation(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	other := newTestWorkspace(t, s, "/other")
	mustLand(t, s, ws.ID, "a")
	if _, err := s.CreateLearningSnapshot(other.ID, "other", 5); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 7; i++ {
		snap, err := s.CreateLearningSnapshot(ws.ID, "before curation job", 5)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, snap.ID)
	}
	list, err := s.ListLearningSnapshots(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("snapshots = %d, want 5", len(list))
	}
	for i, sn := range list { // newest first
		if sn.ID != ids[6-i] {
			t.Fatalf("snapshot %d id = %d, want %d", i, sn.ID, ids[6-i])
		}
		if sn.Payload != "" || sn.Reason != "before curation job" {
			t.Fatalf("list entry = %+v, want reason and no payload", sn)
		}
	}
	if l, _ := s.ListLearningSnapshots(other.ID); len(l) != 1 {
		t.Fatalf("other workspace snapshots = %d, want 1 (rotation is per workspace)", len(l))
	}
	got, err := s.GetLearningSnapshot(ids[6])
	if err != nil || got.Payload == "" {
		t.Fatalf("GetLearningSnapshot = %+v, %v; want payload", got, err)
	}
	// keep <= 0 defaults to 5.
	for i := 0; i < 3; i++ {
		if _, err := s.CreateLearningSnapshot(ws.ID, "x", 0); err != nil {
			t.Fatal(err)
		}
	}
	if l, _ := s.ListLearningSnapshots(ws.ID); len(l) != 5 {
		t.Fatalf("snapshots with default keep = %d, want 5", len(l))
	}
}

func TestSnapshot_Restore(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	a := mustLand(t, s, ws.ID, "a")
	b := mustLand(t, s, ws.ID, "b")
	gone := mustLand(t, s, ws.ID, "gone")
	if err := s.SetSkillPinned(a.ID, true, "human"); err != nil {
		t.Fatal(err)
	}

	snap, err := s.CreateLearningSnapshot(ws.ID, "before curation job 1", 5)
	if err != nil {
		t.Fatal(err)
	}

	// After the snapshot: patch a, archive b, hard-delete gone, add fresh.
	if _, err := s.PatchSkill(PatchSkillInput{ID: a.ID, ExpectVersion: 1, Description: "d-new", Body: "b-new", Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillPinned(a.ID, false, "human"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillStatus(b.ID, SkillArchived, "human", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLearnedSkill(gone.ID, "human"); err != nil {
		t.Fatal(err)
	}
	fresh := mustLand(t, s, ws.ID, "fresh")

	res, err := s.RestoreLearningSnapshot(snap.ID, "human")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Restored) != 2 || res.Restored[0] != "a" || res.Restored[1] != "b" {
		t.Fatalf("Restored = %v, want [a b]", res.Restored)
	}
	if len(res.Untouched) != 1 || res.Untouched[0] != "fresh" {
		t.Fatalf("Untouched = %v, want [fresh]", res.Untouched)
	}

	ga, _ := s.GetLearnedSkill(a.ID)
	if ga.Description != "d-a" || ga.Body != "b-a" || !ga.Pinned || ga.Version != 3 {
		t.Fatalf("a = desc %q body %q pinned %v v%d, want d-a/b-a/pinned/v3", ga.Description, ga.Body, ga.Pinned, ga.Version)
	}
	gb, _ := s.GetLearnedSkill(b.ID)
	if gb.Status != SkillActive || gb.Version != 1 {
		t.Fatalf("b = %s v%d, want active v1 (text unchanged, no new version)", gb.Status, gb.Version)
	}
	vs, _ := s.ListSkillVersions(a.ID)
	if len(vs) != 3 || vs[0].Body != "b-a" || vs[1].Body != "b-new" {
		t.Fatalf("a versions = %+v, want restore as a new top version", vs)
	}
	if _, err := s.GetLearnedSkill(gone.ID); err == nil {
		t.Fatal("hard-deleted skill was resurrected")
	}
	gf, _ := s.GetLearnedSkill(fresh.ID)
	if gf.Version != 1 || gf.Status != SkillActive {
		t.Fatalf("fresh touched: %+v", gf)
	}
	for _, id := range []int64{a.ID, b.ID} {
		led, _ := s.ListLedger(ws.ID, id, 1)
		if len(led) != 1 || led[0].Action != "restore-snapshot" || led[0].Actor != "human" {
			t.Fatalf("ledger head for %d = %+v, want restore-snapshot by human", id, led)
		}
	}
	if led, _ := s.ListLedger(ws.ID, fresh.ID, 0); len(led) != 1 {
		t.Fatalf("fresh ledger rows = %d, want 1 (untouched)", len(led))
	}

	// Restoring again changes nothing.
	again, err := s.RestoreLearningSnapshot(snap.ID, "human")
	if err != nil || len(again.Restored) != 0 {
		t.Fatalf("second restore = %+v, %v; want nothing restored", again, err)
	}
}

func TestSnapshot_RestoreAtomic(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	b := mustLand(t, s, ws.ID, "b")
	a := mustLand(t, s, ws.ID, "a")
	snap, err := s.CreateLearningSnapshot(ws.ID, "r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchSkill(PatchSkillInput{ID: b.ID, ExpectVersion: 1, Description: "d2", Body: "b2", Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillStatus(a.ID, SkillArchived, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	// A new live skill now owns a's name: restoring a to active must fail,
	// and b's restore (applied first, lower id) must roll back with it.
	mustLand(t, s, ws.ID, "a")
	before := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_ledger`)
	if _, err := s.RestoreLearningSnapshot(snap.ID, "human"); err == nil {
		t.Fatal("restore with a name clash: error = nil")
	}
	gb, _ := s.GetLearnedSkill(b.ID)
	if gb.Body != "b2" || gb.Version != 2 {
		t.Fatalf("b = %q v%d after failed restore, want untouched b2 v2", gb.Body, gb.Version)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_ledger`); got != before {
		t.Fatalf("ledger rows %d -> %d, want unchanged", before, got)
	}
}

func TestSnapshot_RestoreRefusesOtherWorkspace(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := newLearningWorkspace(t, s)
	other := newTestWorkspace(t, s, "/other")
	a := mustLand(t, s, ws.ID, "a")
	snap, err := s.CreateLearningSnapshot(ws.ID, "r", 5)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt: the skill id now belongs to another workspace.
	if _, err := s.db.Exec(`UPDATE learned_skills SET workspace_id = ? WHERE id = ?`, other.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreLearningSnapshot(snap.ID, "human"); err == nil {
		t.Fatal("restore touching another workspace's skill: error = nil")
	}
}

func TestSnapshot_ForgetDeletesSnapshots(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws1 := newLearningWorkspace(t, s)
	ws2 := newTestWorkspace(t, s, "/keep")
	for _, ws := range []Workspace{ws1, ws2} {
		mustLand(t, s, ws.ID, "a")
		if _, err := s.CreateLearningSnapshot(ws.ID, "r", 5); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ForgetWorkspaceLearning(ws1.ID); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learning_snapshots WHERE workspace_id = ?`, ws1.ID); got != 0 {
		t.Fatalf("snapshots after forget = %d, want 0", got)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learning_snapshots WHERE workspace_id = ?`, ws2.ID); got != 1 {
		t.Fatalf("kept snapshots = %d, want 1", got)
	}
	if err := s.DeleteWorkspace(ws2.ID); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM learning_snapshots`); got != 0 {
		t.Fatalf("snapshots after DeleteWorkspace = %d, want 0", got)
	}
}

func TestMigrate_AddsLearningSnapshotsTable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(`CREATE TABLE workspaces (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, title TEXT NOT NULL, routing_policy TEXT NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workspaces VALUES (1,'/r','r','hard',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		if _, err := s.CreateLearningSnapshot(1, "r", 5); err != nil {
			t.Fatalf("Open #%d: snapshot: %v", i, err)
		}
		s.Close()
	}
}
