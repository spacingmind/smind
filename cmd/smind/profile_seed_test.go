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
	if err != nil || len(list) != 5 {
		t.Fatalf("profiles = %+v, err %v; want 5", list, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("output = %q, want 5 lines", out)
	}
	for i, name := range []string{"peer-sonnet", "peer-glm", "lead", "reviewer", "supervisor"} {
		want := "created " + name + " (id "
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], want)
		}
	}
	var glmFallback, sonnetID int64
	for _, p := range list {
		switch p.Name {
		case "peer-sonnet":
			sonnetID = p.ID
		case "peer-glm":
			if p.FallbackProfileID != nil {
				glmFallback = *p.FallbackProfileID
			}
		}
	}
	if sonnetID == 0 || glmFallback != sonnetID {
		t.Errorf("peer-glm fallback = %d, want %d", glmFallback, sonnetID)
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
	if strings.Contains(out, "created") || strings.Count(out, "kept ") != 5 || !strings.Contains(out, "kept lead (id 3)") {
		t.Errorf("rerun output = %q, want five kept lines", out)
	}
	if list, _ := profiles.New(s).List(); len(list) != 5 {
		t.Errorf("profiles = %d, want 5", len(list))
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
