package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func counterSkill(t *testing.T, s *Store, name string) LearnedSkill {
	t.Helper()
	ws := newTestWorkspace(t, s, "/ctr-"+name)
	return mustLand(t, s, ws.ID, name)
}

func TestCounters_OfferedOncePerRun(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	for i := 0; i < 3; i++ {
		if err := s.RecordOffered("run-1", []int64{k.ID}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordOffered("run-2", []int64{k.ID}, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetLearnedSkill(k.ID)
	if got.OfferedRuns != 2 || got.LastOfferedAt == nil {
		t.Fatalf("offered_runs = %d last=%v, want 2 and set", got.OfferedRuns, got.LastOfferedAt)
	}
}

func TestCounters_SkillToolLoad(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	s.RecordOffered("r", []int64{k.ID}, true)
	for i := 0; i < 3; i++ { // repeated loads in one run count once per via
		if err := s.RecordLoad("r", k.ID, LoadViaSkill); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.GetLearnedSkill(k.ID)
	if got.Loads != 1 || got.LoadsViaSkill != 1 || got.LoadsViaRead != 0 || got.LoadedRuns != 1 || got.LastLoadedAt == nil {
		t.Fatalf("counters = %+v", got)
	}
}

func TestCounters_ReadUnderProjectionLoad(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	s.RecordOffered("r", []int64{k.ID}, true)
	s.RecordLoad("r", k.ID, LoadViaRead)
	s.RecordLoad("r", k.ID, LoadViaSkill)
	got, _ := s.GetLearnedSkill(k.ID)
	if got.Loads != 2 || got.LoadsViaSkill != 1 || got.LoadsViaRead != 1 || got.LoadedRuns != 1 {
		t.Fatalf("counters = %+v, want loads 2 (skill 1, read 1), loaded_runs 1", got)
	}
	if err := s.RecordLoad("r", k.ID, "bogus"); err == nil {
		t.Fatal("unknown via: error = nil")
	}
}

func TestCounters_LoadWithoutOfferIgnored(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	s.RecordOffered("other", []int64{k.ID}, true)
	if err := s.RecordLoad("r", k.ID, LoadViaSkill); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetLearnedSkill(k.ID)
	if got.Loads != 0 || got.LoadedRuns != 0 {
		t.Fatalf("counters = %+v, want untouched", got)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_loads`); n != 0 {
		t.Fatalf("load rows = %d, want 0", n)
	}
}

func TestCounters_UnmeasuredWhenNoSignalProvider(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	s.RecordOffered("glm-run", []int64{k.ID}, false)
	got, _ := s.GetLearnedSkill(k.ID)
	if got.Measured || got.OfferedRuns != 1 {
		t.Fatalf("after unmeasured offer: measured=%v offered=%d", got.Measured, got.OfferedRuns)
	}
	s.RecordOffered("claude-run", []int64{k.ID}, true)
	got, _ = s.GetLearnedSkill(k.ID)
	if !got.Measured {
		t.Fatal("measured offer did not flip measured")
	}
	s.RecordOffered("glm-run-2", []int64{k.ID}, false)
	got, _ = s.GetLearnedSkill(k.ID)
	if !got.Measured {
		t.Fatal("unmeasured offer flipped measured back")
	}
}

func TestCounters_ReusedAfterPatch(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	s.RecordOffered("r1", []int64{k.ID}, true)
	s.RecordLoad("r1", k.ID, LoadViaSkill) // before any patch: no reuse
	if got, _ := s.GetLearnedSkill(k.ID); got.ReusedAfterPatch != 0 {
		t.Fatalf("reused before patch = %d", got.ReusedAfterPatch)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := s.PatchSkill(PatchSkillInput{ID: k.ID, ExpectVersion: 1, Description: "d2", Body: "b2", Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	s.RecordOffered("r2", []int64{k.ID}, true)
	s.RecordOffered("r3", []int64{k.ID}, true)
	s.RecordLoad("r2", k.ID, LoadViaSkill)
	s.RecordLoad("r2", k.ID, LoadViaRead)  // same run: not a second reuse
	s.RecordLoad("r3", k.ID, LoadViaSkill) // a later load: only the first counts
	got, _ := s.GetLearnedSkill(k.ID)
	if got.ReusedAfterPatch != 1 {
		t.Fatalf("reused_after_patch = %d, want 1", got.ReusedAfterPatch)
	}
}

func TestCounters_DeleteAndForgetCleanRows(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := counterSkill(t, s, "a")
	s.RecordOffered("r", []int64{k.ID}, true)
	s.RecordLoad("r", k.ID, LoadViaSkill)
	if err := s.DeleteLearnedSkill(k.ID, "human"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_offers`) + countRows(t, s, `SELECT COUNT(*) FROM learned_skill_loads`); n != 0 {
		t.Fatalf("counter rows left after delete: %d", n)
	}
	k2 := counterSkill(t, s, "b")
	s.RecordOffered("r", []int64{k2.ID}, true)
	if err := s.ForgetWorkspaceLearning(k2.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM learned_skill_offers`); n != 0 {
		t.Fatalf("offer rows left after forget: %d", n)
	}
}

// A database created before the counters existed gains the tables on Open.
func TestMigrate_AddsLearningCounterTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE workspaces (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, title TEXT NOT NULL, routing_policy TEXT NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		for _, table := range []string{"learned_skill_offers", "learned_skill_loads"} {
			if n := countRows(t, s, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?`, table); n != 1 {
				t.Fatalf("Open #%d: table %s missing", i, table)
			}
		}
		s.Close()
	}
}
