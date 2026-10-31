package main

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/profiles"
)

func TestProfileSeedRoles_CreatesAndReports(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	t.Setenv("SMIND_RUN_ID", "")

	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles"}) })
	if code != 0 {
		t.Fatalf("run(profile seed roles) = %d; stdout %q", code, out)
	}
	list, err := profiles.New(s).List()
	if err != nil || len(list) != 4 {
		t.Fatalf("profiles = %+v, err %v; want 4", list, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("output = %q, want 4 lines", out)
	}
	for i, name := range []string{"peer-sonnet", "lead", "validator", "steward"} {
		want := "created " + name + " (id "
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], want)
		}
	}
}

func TestProfileSeedRoles_RerunKeeps(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	t.Setenv("SMIND_RUN_ID", "")
	captureStdout(t, func() { run([]string{"profile", "seed", "roles"}) })

	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles"}) })
	if code != 0 {
		t.Fatalf("rerun = %d; stdout %q", code, out)
	}
	if strings.Contains(out, "created") || strings.Count(out, "kept ") != 4 || !strings.Contains(out, "kept lead (id 2)") {
		t.Errorf("rerun output = %q, want four kept lines", out)
	}
	if list, _ := profiles.New(s).List(); len(list) != 4 {
		t.Errorf("profiles = %d, want 4", len(list))
	}
}

func TestProfileSeedRoles_RefusedInsideARun(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	t.Setenv("SMIND_RUN_ID", "run-1")

	var code int
	stderr := captureStderr(t, func() { code = run([]string{"profile", "seed", "roles"}) })
	if code == 0 || !strings.Contains(stderr, "profile seed is for humans, not agents") {
		t.Fatalf("code = %d stderr %q, want refusal", code, stderr)
	}
	if list, _ := profiles.New(s).List(); len(list) != 0 {
		t.Errorf("created %d profiles inside a run", len(list))
	}
}

func TestProfileSeed_UnknownSubjectUsage(t *testing.T) {
	newTestProfileDaemon(t)
	var code int
	stderr := captureStderr(t, func() { code = run([]string{"profile", "seed", "bogus"}) })
	if code != 2 || !strings.Contains(stderr, "usage: smind profile seed roles") {
		t.Fatalf("code = %d stderr %q", code, stderr)
	}
}
