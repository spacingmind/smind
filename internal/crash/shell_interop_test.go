package crash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// crash-go-reads-rust-report-fixture: a report in the shape the desktop
// shell's Rust crate writes (testdata/desktop_shell_report.json, which the Rust
// test also checks its own output against, key for key) is read by List, the
// reader `smind doctor` uses.
func TestCrashGoReadsRustReportFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash("testdata/desktop_shell_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "crash-20261115T010203.004-desktop-shell-desktop.thread.json"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	s := List(dir)
	if s.Count != 1 || s.Err != "" || s.Refused {
		t.Fatalf("List = %+v, want one readable report", s)
	}
	if s.NewestSource != "desktop-shell" || s.NewestSite != "desktop.thread" {
		t.Fatalf("source/site = %q/%q", s.NewestSource, s.NewestSite)
	}
	if want := time.Date(2026, 11, 15, 1, 2, 3, 4005006, time.UTC); !s.Newest.Equal(want) {
		t.Fatalf("newest = %v, want %v", s.Newest, want)
	}
	// the fixture has exactly the keys the Go report struct has
	var fixture, own map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(report{})
	if err := json.Unmarshal(b, &own); err != nil {
		t.Fatal(err)
	}
	if len(fixture) != len(own) {
		t.Fatalf("fixture keys %d, Go report keys %d", len(fixture), len(own))
	}
	for k := range own {
		if _, ok := fixture[k]; !ok {
			t.Errorf("fixture lacks key %q", k)
		}
	}
}

// crash-go-desktop-dir-identifier-matches-tauri-conf: DesktopDir derives the
// same directory name the shell's identifier gives Tauri's app_data_dir.
func TestCrashGoDesktopDirIdentifierMatchesTauriConf(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "desktop", "src-tauri", "tauri.conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Identifier string `json:"identifier"`
	}
	if err := json.Unmarshal(raw, &conf); err != nil || conf.Identifier == "" {
		t.Fatalf("identifier: %v %q", err, conf.Identifier)
	}
	dir := DesktopDir()
	if dir == "" {
		t.Skip("no home directory")
	}
	if !strings.Contains(filepath.ToSlash(dir), "/"+conf.Identifier+"/crashes") {
		t.Fatalf("DesktopDir() = %q does not contain /%s/crashes: the shell would write somewhere doctor does not look", dir, conf.Identifier)
	}
}
