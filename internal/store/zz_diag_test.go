package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TEMPORARY diagnostics (windows nightly investigation): what does opening a
// fresh database cost under different durability settings?
func TestDiagOpenCosts(t *testing.T) {
	variants := []struct {
		name, extra string
		singleTx    bool
	}{
		{"current(DELETE,FULL)", "", false},
		{"schema-in-one-tx", "", true},
		{"WAL+NORMAL", "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", false},
		{"synchronous=OFF", "&_pragma=synchronous(OFF)", false},
		{"one-tx+WAL+NORMAL", "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", true},
	}
	for _, v := range variants {
		for run := 0; run < 3; run++ {
			path := filepath.Join(t.TempDir(), "d.db")
			t0 := time.Now()
			db, err := sql.Open("sqlite", sqliteDSN(path)+v.extra)
			if err != nil {
				t.Fatal(err)
			}
			if v.singleTx {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(schema); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := db.Exec(schema); err != nil {
				t.Fatal(err)
			}
			tSchema := time.Since(t0)
			if err := migrate(db); err != nil {
				t.Fatal(err)
			}
			tOpen := time.Since(t0)
			t1 := time.Now()
			if _, err := db.Exec(`CREATE TABLE diag(x INTEGER)`); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 100; i++ {
				if _, err := db.Exec(`INSERT INTO diag(x) VALUES (?)`, i); err != nil {
					t.Fatal(err)
				}
			}
			tIns := time.Since(t1)
			_ = db.Close()
			t.Logf("DIAG %-24s run%d schema=%-10v open=%-10v 100 inserts=%v", v.name, run, tSchema.Round(time.Millisecond), tOpen.Round(time.Millisecond), tIns.Round(time.Millisecond))
		}
	}
}
