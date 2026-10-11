package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkspaceSetup_SetShowClear(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	repo := newTestRepo(t)
	var code int
	out := captureStdout(t, func() { code = run([]string{"workspace", "create", repo, "W", "hard"}) })
	if code != 0 {
		t.Fatalf("workspace create = %d: %s", code, out)
	}
	wss, _ := s.ListWorkspaces()
	id := wss[0].ID
	ids := strconv.FormatInt(id, 10)

	dir := t.TempDir()
	setupFile := filepath.Join(dir, "setup.sh")
	if err := os.WriteFile(setupFile, []byte("npm ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	teardownFile := filepath.Join(dir, "td.sh")
	if err := os.WriteFile(teardownFile, []byte("rm -rf node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out = captureStdout(t, func() {
		code = run([]string{"workspace", "setup", "set", ids, "--setup-file", setupFile, "--teardown-file", teardownFile})
	})
	if code != 0 {
		t.Fatalf("set = %d: %s", code, out)
	}
	if w, _ := s.GetWorkspace(id); w.SetupScript != "npm ci\n" || w.TeardownScript != "rm -rf node_modules\n" {
		t.Fatalf("stored = %+v", w)
	}

	out = captureStdout(t, func() { code = run([]string{"workspace", "setup", "show", ids}) })
	if code != 0 || !strings.Contains(out, "npm ci") || !strings.Contains(out, "rm -rf node_modules") {
		t.Fatalf("show = %d: %q", code, out)
	}

	// Clearing only the setup script keeps the teardown script.
	out = captureStdout(t, func() { code = run([]string{"workspace", "setup", "clear", ids, "--setup"}) })
	if code != 0 {
		t.Fatalf("clear --setup = %d: %s", code, out)
	}
	if w, _ := s.GetWorkspace(id); w.SetupScript != "" || w.TeardownScript == "" {
		t.Fatalf("after clear --setup = %+v", w)
	}
	out = captureStdout(t, func() { code = run([]string{"workspace", "setup", "clear", ids}) })
	if code != 0 {
		t.Fatalf("clear = %d: %s", code, out)
	}
	if w, _ := s.GetWorkspace(id); w.SetupScript != "" || w.TeardownScript != "" {
		t.Fatalf("after clear = %+v", w)
	}
	out = captureStdout(t, func() { code = run([]string{"workspace", "setup", "show", ids}) })
	if code != 0 || strings.Count(out, "(none)") != 2 {
		t.Fatalf("show after clear = %d: %q", code, out)
	}
}
