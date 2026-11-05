package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// skelEnv is a store with one task and helpers for finished runs.
type skelEnv struct {
	*retEnv
	task int64
}

func newSkelEnv(t *testing.T) *skelEnv {
	e := newRetEnv(t)
	return &skelEnv{retEnv: e, task: e.task(t, false, false)}
}

func (e *skelEnv) run(t *testing.T, id, status string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := e.s.CreateRun(Run{ID: id, TaskID: e.task, Provider: "glm", Prompt: "p", Status: status, StartedAt: now, FinishedAt: &now}); err != nil {
		t.Fatal(err)
	}
}

func (e *skelEnv) row(t *testing.T, id string, seq int64, data string) RunEvent {
	t.Helper()
	ev, err := e.s.AppendRunEvent(id, seq, data)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestCutRunes(t *testing.T) {
	for _, tc := range []struct {
		name, in   string
		n          int
		head, tail string
	}{
		{"short", "abc", 8, "abc", ""},
		{"ascii", "abcdefghij", 3, "abc", "hij"},
		{"multibyte head cut back", "日本語日本語", 4, "日", "語"},
		{"empty", "", 4, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, tl := CutRunes(tc.in, tc.n)
			if h != tc.head || tl != tc.tail {
				t.Fatalf("CutRunes(%q, %d) = %q, %q; want %q, %q", tc.in, tc.n, h, tl, tc.head, tc.tail)
			}
			if !utf8.ValidString(h) || !utf8.ValidString(tl) {
				t.Fatalf("invalid UTF-8: %q %q", h, tl)
			}
		})
	}
}

func v2Row(think bool) string {
	if think {
		return `{"type":5,"v":2,"skel":{"think":{"n":4096}}}`
	}
	return `{"type":6,"toolCallId":"c","toolName":"Bash","toolStatus":"success","toolResult":"summary…","v":2,"skel":{"out":{"n":9,"sha":"x","ex":true}}}`
}

func TestRehydrate(t *testing.T) {
	e := newSkelEnv(t)
	e.run(t, "r", "done")
	plain := `{"type":0,"text":"hello"}`
	e.row(t, "r", 0, plain)
	e.row(t, "r", 1, v2Row(true))
	e.row(t, "r", 2, v2Row(false))
	e.row(t, "r", 3, `{"type":6,"toolCallId":"d","toolName":"Bash","toolResult":"summary","v":2,"skel":{"out":{"n":9,"ex":true}}}`)
	if _, err := e.s.db.Exec(`INSERT INTO run_excerpts (run_id, seq, field, head, tail, bytes, sha256) VALUES ('r', 2, 'out', '"full result"', '', 13, 'x')`); err != nil {
		t.Fatal(err)
	}
	// seq 3: excerpt gone (pruned): the row keeps its summary and says so.
	rows, err := e.s.ListRunEvents("r")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].EventData != plain || rows[0].Truncated {
		t.Fatalf("a v1 row must be returned byte for byte: %+v", rows[0])
	}
	if !strings.Contains(rows[1].EventData, `[thinking not kept: 4096 bytes]`) || !rows[1].Truncated || strings.Contains(rows[1].EventData, `"skel"`) {
		t.Fatalf("thinking row = %+v", rows[1])
	}
	if !strings.Contains(rows[2].EventData, `"toolResult":"full result"`) || rows[2].Truncated {
		t.Fatalf("whole excerpt must come back as the original payload: %+v", rows[2])
	}
	if !strings.Contains(rows[3].EventData, `"toolResult":"summary"`) || !rows[3].Truncated {
		t.Fatalf("a row whose excerpt is gone keeps its summary and is truncated: %+v", rows[3])
	}
	raw, _ := e.s.ListRunEventsRaw("r")
	if !strings.Contains(raw[2].EventData, `"v":2`) {
		t.Fatalf("the raw read must not rehydrate: %s", raw[2].EventData)
	}
}

func TestRehydrateHeadTail(t *testing.T) {
	e := newSkelEnv(t)
	e.run(t, "r", "done")
	e.row(t, "r", 0, v2Row(false))
	head, tail := strings.Repeat("h", 4096), strings.Repeat("t", 4096)
	if _, err := e.s.db.Exec(`INSERT INTO run_excerpts (run_id, seq, field, head, tail, bytes, sha256) VALUES ('r', 0, 'out', ?, ?, 20000, 'x')`, head, tail); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.s.ListRunEvents("r")
	want := head + OmittedMarker(20000-8192) + tail
	var got struct {
		ToolResult string `json:"toolResult"`
	}
	if err := json.Unmarshal([]byte(rows[0].EventData), &got); err != nil {
		t.Fatal(err)
	}
	if got.ToolResult != want || !rows[0].Truncated {
		t.Fatalf("head+tail not rehydrated: %.200s", got.ToolResult)
	}
}

func TestApplySkeletonGuards(t *testing.T) {
	e := newSkelEnv(t)
	plan := func(id string) []SkeletonRow {
		rows, _ := e.s.ListRunEventsRaw(id)
		return []SkeletonRow{{ID: rows[0].ID, Seq: 0, EventData: `{"type":0,"text":"x"}`, Excerpts: []Excerpt{{Field: "out", Head: `"h"`, Bytes: 3, SHA256: "s"}}}}
	}
	for _, tc := range []struct {
		name, status string
		mutate       string
		want         bool
	}{
		{"finished and eligible", "done", ``, true},
		{"running", "running", ``, false},
		{"legacy: finished before skeleton storage", "done", `UPDATE runs SET skeleton_rev = -1 WHERE id = ?`, false},
		{"pruned", "done", `UPDATE runs SET history_pruned_at = '2026-01-01' WHERE id = ?`, false},
		{"already converted", "done", `UPDATE runs SET skeleton_at = '2026-01-01', skeleton_rev = 1 WHERE id = ?`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "r-" + strings.ReplaceAll(tc.name, " ", "-")
			e.run(t, id, tc.status)
			e.row(t, id, 0, `{"type":0,"text":"orig"}`)
			if tc.mutate != "" {
				if _, err := e.s.db.Exec(tc.mutate, id); err != nil {
					t.Fatal(err)
				}
			}
			ok, err := e.s.ApplySkeleton(id, plan(id), time.Now())
			if err != nil || ok != tc.want {
				t.Fatalf("ApplySkeleton = %v, %v; want %v", ok, err, tc.want)
			}
			rows, _ := e.s.ListRunEventsRaw(id)
			var excerpts int
			_ = e.s.db.QueryRow(`SELECT COUNT(*) FROM run_excerpts WHERE run_id = ?`, id).Scan(&excerpts)
			if tc.want && (rows[0].EventData != `{"type":0,"text":"x"}` || excerpts != 1) {
				t.Fatalf("rows not rewritten: %v, %d excerpts", rows[0].EventData, excerpts)
			}
			if !tc.want && (rows[0].EventData != `{"type":0,"text":"orig"}` || excerpts != 0) {
				t.Fatalf("a refused run was changed: %v, %d excerpts", rows[0].EventData, excerpts)
			}
			sk, _ := e.s.GetRunSkeleton(id)
			if tc.want && (sk.SkeletonAt == nil || sk.Rev != 1) {
				t.Fatalf("run not marked: %+v", sk)
			}
		})
	}
}

// A failure inside the transaction leaves the run untouched.
func TestApplySkeletonIsAtomic(t *testing.T) {
	e := newSkelEnv(t)
	if _, err := e.s.db.Exec(`DROP TABLE run_excerpts`); err != nil {
		t.Fatal(err)
	}
	e.run(t, "r", "done")
	ev := e.row(t, "r", 0, `{"type":0,"text":"orig"}`)
	ev2 := e.row(t, "r", 1, `{"type":0,"text":"orig2"}`)
	if _, err := e.s.ApplySkeleton("r", []SkeletonRow{
		{ID: ev.ID, Seq: 0, EventData: `{"type":0,"text":"changed"}`},
		{ID: ev2.ID, Seq: 1, EventData: `{"type":0,"text":"changed2"}`, Excerpts: []Excerpt{{Field: "out", Head: "h"}}}, // fails: no table
	}, time.Now()); err == nil {
		t.Fatal("want an error when the excerpt table is missing")
	}
	rows, _ := e.s.ListRunEventsRaw("r")
	if rows[0].EventData != `{"type":0,"text":"orig"}` || rows[1].EventData != `{"type":0,"text":"orig2"}` {
		t.Fatalf("a failed conversion left a partial rewrite: %s / %s", rows[0].EventData, rows[1].EventData)
	}
	if sk, _ := e.s.GetRunSkeleton("r"); sk.SkeletonAt != nil {
		t.Fatal("a failed conversion marked the run")
	}
}

func TestMigrationMarksFinishedRunsLegacy(t *testing.T) {
	e := newSkelEnv(t)
	e.run(t, "old", "done")
	e.run(t, "live", "running")
	if _, err := e.s.db.Exec(`UPDATE runs SET skeleton_rev = 0`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.name == "run_skeleton" {
			if err := m.apply(e.s.db); err != nil {
				t.Fatal(err)
			}
			// idempotent: a second application changes nothing more
			if err := m.apply(e.s.db); err != nil {
				t.Fatal(err)
			}
		}
	}
	old, _ := e.s.GetRunSkeleton("old")
	live, _ := e.s.GetRunSkeleton("live")
	if old.Rev != -1 || live.Rev != 0 {
		t.Fatalf("finished run rev %d (want -1), running run rev %d (want 0)", old.Rev, live.Rev)
	}
	ids, _ := e.s.SkeletonCandidates(10)
	if len(ids) != 0 {
		t.Fatalf("candidates = %v: a legacy run and a running run are never candidates", ids)
	}
}

// The excerpts_pruned_at column (PR C) exists and GetRunSkeleton reads it.
func TestExcerptsPrunedAtColumnExists(t *testing.T) {
	e := newSkelEnv(t)
	e.run(t, "r", "done")
	sk, err := e.s.GetRunSkeleton("r")
	if err != nil || sk.ExcerptsPrunedAt != nil {
		t.Fatalf("fresh run: skeleton = %+v, %v; ExcerptsPrunedAt must be nil", sk, err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	if _, err := e.s.db.Exec(`UPDATE runs SET excerpts_pruned_at = ? WHERE id = 'r'`, at); err != nil {
		t.Fatal(err)
	}
	if sk, _ := e.s.GetRunSkeleton("r"); sk.ExcerptsPrunedAt == nil || !sk.ExcerptsPrunedAt.Equal(at) {
		t.Fatalf("after the stamp: %+v", sk)
	}
}

func TestSkeletonCandidates(t *testing.T) {
	e := newSkelEnv(t)
	e.run(t, "ok", "done")
	e.row(t, "ok", 0, `{"type":0,"text":"x"}`)
	e.run(t, "noevents", "done")
	e.run(t, "running", "running")
	e.row(t, "running", 0, `{"type":0,"text":"x"}`)
	ids, err := e.s.SkeletonCandidates(10)
	if err != nil || len(ids) != 1 || ids[0] != "ok" {
		t.Fatalf("candidates = %v, %v; want [ok]", ids, err)
	}
}

// Every statement that deletes run_events must delete run_excerpts too, or an
// excerpt outlives its run (ADR-0053). Fails when a new delete site is added
// without it.
func TestEveryRunEventsDeleteAlsoDeletesExcerpts(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	del := regexp.MustCompile(`DELETE FROM run_events`)
	for _, f := range files {
		// run_skeleton.go deletes single rows merged into a neighbour, not a run's rows.
		if strings.HasSuffix(f, "_test.go") || f == "run_skeleton.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if n := len(del.FindAllString(src, -1)); n > 0 {
			if m := strings.Count(src, "DELETE FROM run_excerpts"); m < n {
				t.Errorf("%s: %d DELETE FROM run_events but %d DELETE FROM run_excerpts", f, n, m)
			}
		}
	}
}

func TestPrunedRunDropsItsExcerpts(t *testing.T) {
	e := newSkelEnv(t)
	e.run(t, "r", "done")
	e.row(t, "r", 0, v2Row(false))
	if _, err := e.s.db.Exec(`INSERT INTO run_excerpts (run_id, seq, field, head, tail, bytes, sha256) VALUES ('r', 0, 'out', '"x"', '', 3, 'x')`); err != nil {
		t.Fatal(err)
	}
	// not marked pruned: nothing is deleted
	if _, err := e.s.DeleteRunEventsBatch("r", 10); err != nil {
		t.Fatal(err)
	}
	count := func() (n int) {
		_ = e.s.db.QueryRow(`SELECT COUNT(*) FROM run_excerpts WHERE run_id = 'r'`).Scan(&n)
		return
	}
	if count() != 1 {
		t.Fatal("excerpts of a run that is not marked pruned were deleted")
	}
	if _, err := e.s.db.Exec(`UPDATE runs SET history_pruned_at = ? WHERE id = 'r'`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.DeleteRunEventsBatch("r", 10); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("excerpts of a pruned run survived")
	}
}
