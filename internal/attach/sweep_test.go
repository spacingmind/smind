package attach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var sweepNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func mkSet(t *testing.T, root, id string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01-a"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := sweepNow.Add(-age)
	if err := os.Chtimes(dir, mt, mt); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestSweep_OrphansOlderThan24h(t *testing.T) {
	s, root := newTestStore(t)
	a := mkSet(t, root, strings.Repeat("a", 32), 25*time.Hour)
	b := mkSet(t, root, strings.Repeat("b", 32), 48*time.Hour)
	n, err := s.Sweep(nil, 24*time.Hour, sweepNow)
	if err != nil || n != 2 || exists(a) || exists(b) {
		t.Fatalf("removed=%d err=%v", n, err)
	}
}

func TestSweep_KeepsReferencedAndYoung(t *testing.T) {
	s, root := newTestStore(t)
	ref := mkSet(t, root, strings.Repeat("a", 32), 72*time.Hour)
	young := mkSet(t, root, strings.Repeat("b", 32), 23*time.Hour)
	edge := mkSet(t, root, strings.Repeat("c", 32), 24*time.Hour)
	old := mkSet(t, root, strings.Repeat("d", 32), 24*time.Hour+time.Second)
	n, err := s.Sweep(map[string]bool{strings.Repeat("a", 32): true}, 24*time.Hour, sweepNow)
	if err != nil || n != 1 {
		t.Fatalf("removed=%d err=%v", n, err)
	}
	if !exists(ref) || !exists(young) || !exists(edge) || exists(old) {
		t.Fatal("wrong survivors")
	}
}

func TestSweep_IgnoresForeignEntries(t *testing.T) {
	s, root := newTestStore(t)
	if n, err := s.Sweep(nil, time.Hour, sweepNow); err != nil || n != 0 {
		t.Fatalf("missing root: %d %v", n, err)
	}
	foreign := mkSet(t, root, "not-a-set", 100*time.Hour)
	upper := mkSet(t, root, strings.Repeat("A", 32), 100*time.Hour)
	short := mkSet(t, root, strings.Repeat("a", 31), 100*time.Hour)
	file := filepath.Join(root, strings.Repeat("e", 32))
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := sweepNow.Add(-100 * time.Hour)
	_ = os.Chtimes(file, old, old)
	n, err := s.Sweep(nil, time.Hour, sweepNow)
	if err != nil || n != 0 {
		t.Fatalf("removed=%d err=%v", n, err)
	}
	for _, p := range []string{foreign, upper, short, file} {
		if !exists(p) {
			t.Errorf("%s was removed", p)
		}
	}
}
