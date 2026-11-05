package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDebugRunStatsRefusesWithoutAPathAndOnLiveDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	t.Setenv("HOME", t.TempDir())
	live := filepath.Join(home, "smind.db")
	if err := os.WriteFile(live, []byte("not a db"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := runDebug(context.Background(), []string{"run-stats"}, &out, &errb); code != 2 {
		t.Errorf("no --db: exit %d, want 2", code)
	}
	errb.Reset()
	if code := runDebug(context.Background(), []string{"run-stats", "--db", live}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "refusing") {
		t.Errorf("live db: exit %d, stderr %q", code, errb.String())
	}
}

func TestDebugRunStatsPrintsTableAndJSON(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "copy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE runs (id TEXT PRIMARY KEY, provider TEXT, started_at TIMESTAMP)`,
		`CREATE TABLE run_events (id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT, seq INTEGER, event_data TEXT, created_at TIMESTAMP)`,
		`INSERT INTO runs VALUES ('r1','glm','2026-11-01 10:00:00')`,
		`INSERT INTO run_events (run_id, seq, event_data, created_at) VALUES ('r1',0,'{"type":0,"text":"hello"}','2026-11-01 10:00:00')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	var out, errb bytes.Buffer
	if code := runDebug(context.Background(), []string{"run-stats", "--db", path}, &out, &errb); code != 0 || !strings.Contains(out.String(), "runs 1, rows 1") {
		t.Fatalf("table: exit %d: %s%s", code, out.String(), errb.String())
	}
	if strings.Contains(out.String(), "hello") {
		t.Errorf("the table prints event text:\n%s", out.String())
	}
	out.Reset()
	if code := runDebug(context.Background(), []string{"run-stats", "--db", path, "--json"}, &out, &errb); code != 0 || !strings.Contains(out.String(), `"rows": 1`) {
		t.Fatalf("json: exit %d: %s", code, out.String())
	}
}
