package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

// humanEnv clears the agent-run markers so `run` takes the human path even
// when the test binary itself was started inside an agent run.
func humanEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SMIND_RUN_ID", "")
	t.Setenv("SMIND_AGENT_RUN", "")
}

// seedRolePack seeds the standard pack through the CLI, as a human would.
func seedRolePack(t *testing.T) {
	t.Helper()
	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles"}) })
	if code != 0 {
		t.Fatalf("run(profile seed roles) = %d; stdout %q", code, out)
	}
}

// seatProfile reads the stored profile of a seat name.
func seatProfile(t *testing.T, s *store.Store, name string) store.AgentProfile {
	t.Helper()
	list, err := profiles.New(s).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("seat %q not found in %d profiles", name, len(list))
	return store.AgentProfile{}
}

// staleNotes is the text the first pack release seeded as peer-sonnet's notes.
const staleNotes = "Hard outcomes and the GLM fallback. Sonnet 5.5, auto mode. No orchestration tools."

// storeSetSeat rewrites one stored seat through the store (no RPC, no
// validation), the way an earlier release's text or a human edit would sit
// there. staleNotes is the first pack release's peer-sonnet notes, so the history
// table classifies them stale.
func storeSetSeat(t *testing.T, s *store.Store, name string, set func(*store.AgentProfile)) {
	t.Helper()
	p := seatProfile(t, s, name)
	set(&p)
	if _, err := s.UpdateAgentProfile(p); err != nil {
		t.Fatalf("UpdateAgentProfile(%s) error = %v", name, err)
	}
}

func TestProfileSeedRolesCheck_ExitCodes(t *testing.T) {
	t.Run("fresh pack is up to date", func(t *testing.T) {
		_, _ = newTestProfileDaemon(t)
		humanEnv(t)
		seedRolePack(t)

		var code int
		out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--check"}) })
		if code != 0 || !strings.Contains(out, "up to date") {
			t.Fatalf("--check = %d, out %q; want 0 and up to date", code, out)
		}
	})
	t.Run("stale field exits 10", func(t *testing.T) {
		_, s := newTestProfileDaemon(t)
		humanEnv(t)
		seedRolePack(t)
		storeSetSeat(t, s, "peer-sonnet", func(p *store.AgentProfile) { p.Notes = staleNotes })

		var code int
		out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--check"}) })
		if code != 10 {
			t.Fatalf("--check = %d, want 10; out %q", code, out)
		}
		if !strings.Contains(out, "updates available for 1 seat(s)") {
			t.Errorf("out %q, want updates available for 1 seat(s)", out)
		}
		if !strings.Contains(out, "peer-sonnet (id ") || !strings.Contains(out, "notes") || !strings.Contains(out, "stale") {
			t.Errorf("out %q, want a stale notes line for peer-sonnet", out)
		}
	})
	t.Run("customised only exits 0", func(t *testing.T) {
		_, s := newTestProfileDaemon(t)
		humanEnv(t)
		seedRolePack(t)
		storeSetSeat(t, s, "lead", func(p *store.AgentProfile) { p.Notes = "my own notes" })

		var code int
		out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--check"}) })
		if code != 0 {
			t.Fatalf("--check = %d, want 0 (customised is not an update); out %q", code, out)
		}
		if !strings.Contains(out, "customised") {
			t.Errorf("out %q, want the customised notes reported", out)
		}
	})
}

func TestProfileSeedRolesDiff_ShowsStaleAndCustomised(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)
	storeSetSeat(t, s, "peer-sonnet", func(p *store.AgentProfile) { p.Notes = staleNotes })
	storeSetSeat(t, s, "lead", func(p *store.AgentProfile) { p.Notes = "my own notes" })

	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--diff"}) })
	if code != 0 {
		t.Fatalf("--diff = %d, want 0; out %q", code, out)
	}
	if !strings.Contains(out, "+++ peer-sonnet.notes") {
		t.Errorf("out %q, want a +++ peer-sonnet.notes header", out)
	}
	if !strings.Contains(out, "+Bounded step") {
		t.Errorf("out %q, want + lines carrying the pack text", out)
	}
	if !strings.Contains(out, "lead.notes") || !strings.Contains(out, "customised") {
		t.Errorf("out %q, want the customised lead notes as customised", out)
	}

	out = captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--diff", "lead.notes"}) })
	if code != 0 || strings.Contains(out, "peer-sonnet.notes") || !strings.Contains(out, "lead.notes") {
		t.Fatalf("--diff lead.notes = %d, out %q; want only the filtered field", code, out)
	}
	out = captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--diff", "peer-sonnet.skills"}) })
	if code != 0 || !strings.Contains(out, "no differences") {
		t.Errorf("--diff peer-sonnet.skills = %d, out %q, want no differences for a field that matches nothing", code, out)
	}
}

func TestProfileSeedRolesDiff_CleanPackHasNoDifferences(t *testing.T) {
	_, _ = newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)

	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--diff"}) })
	if code != 0 || !strings.Contains(out, "no differences") {
		t.Fatalf("--diff on a clean pack = %d, out %q; want 0 and no differences", code, out)
	}
}

func TestProfileSeedRolesApply_UpdatesStaleAndBacksUp(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)
	id := seatProfile(t, s, "peer-sonnet").ID
	storeSetSeat(t, s, "peer-sonnet", func(p *store.AgentProfile) { p.Notes = staleNotes })

	backup := t.TempDir()
	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--apply", "--backup-dir", backup}) })
	if code != 0 {
		t.Fatalf("--apply = %d, want 0; out %q", code, out)
	}
	want := "updated peer-sonnet (id " + strconv.FormatInt(id, 10) + "): notes"
	if !strings.Contains(out, want) {
		t.Errorf("out %q, want %q", out, want)
	}

	if got := seatProfile(t, s, "peer-sonnet").Notes; !strings.Contains(got, "json handoff") {
		t.Errorf("stored notes = %q, want the pack text", got)
	}

	var dir string
	for _, l := range strings.Split(out, "\n") {
		if d, ok := strings.CutPrefix(l, "backup: "); ok {
			dir = d
		}
	}
	if dir == "" {
		t.Fatalf("out %q, want a backup: line", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("peer-sonnet-%d.json", id)))
	if err != nil {
		t.Fatalf("read the backup: %v", err)
	}
	var saved store.AgentProfile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("parse the backup: %v", err)
	}
	if saved.Notes != staleNotes {
		t.Errorf("backup notes = %q, want the old ones", saved.Notes)
	}

	out = captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--apply", "--backup-dir", backup}) })
	if code != 0 || !strings.Contains(out, "nothing to update") {
		t.Fatalf("second --apply = %d, out %q; want 0 and nothing to update", code, out)
	}
}

func TestProfileSeedRolesApply_CustomisedNeedsATake(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)
	id := seatProfile(t, s, "lead").ID
	storeSetSeat(t, s, "lead", func(p *store.AgentProfile) { p.Notes = "my own notes" })

	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--apply"}) })
	if code != 0 || !strings.Contains(out, "nothing to update") {
		t.Fatalf("--apply = %d, out %q; want 0 and nothing to update", code, out)
	}
	if got := seatProfile(t, s, "lead").Notes; got != "my own notes" {
		t.Errorf("notes = %q, want the customised ones untouched", got)
	}

	out = captureStdout(t, func() { code = run([]string{"profile", "seed", "roles", "--apply", "--take", "lead.notes"}) })
	if code != 0 {
		t.Fatalf("--apply --take lead.notes = %d, want 0; out %q", code, out)
	}
	want := "updated lead (id " + strconv.FormatInt(id, 10) + "): notes"
	if !strings.Contains(out, want) {
		t.Errorf("out %q, want %q", out, want)
	}
	got := seatProfile(t, s, "lead").Notes
	if got == "my own notes" || got == "" {
		t.Errorf("notes = %q, want the pack text", got)
	}
}

func TestProfileSeedRolesApply_DryRunWritesNothing(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)
	storeSetSeat(t, s, "peer-sonnet", func(p *store.AgentProfile) { p.Notes = staleNotes })

	backup := t.TempDir()
	var code int
	out := captureStdout(t, func() {
		code = run([]string{"profile", "seed", "roles", "--apply", "--dry-run", "--backup-dir", backup})
	})
	if code != 0 {
		t.Fatalf("--apply --dry-run = %d, want 0; out %q", code, out)
	}
	if !strings.Contains(out, "would update peer-sonnet") || strings.Contains(out, "backup:") {
		t.Errorf("out %q, want would update and no backup line", out)
	}
	entries, err := os.ReadDir(backup)
	if err != nil || len(entries) != 0 {
		t.Errorf("backup dir after a dry run = %d entries (%v), want none", len(entries), err)
	}
	if got := seatProfile(t, s, "peer-sonnet").Notes; got != staleNotes {
		t.Errorf("notes = %q, want still the stale ones", got)
	}
}

func TestProfileSeedRolesApply_RefusedInAnAgentRun(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)
	storeSetSeat(t, s, "peer-sonnet", func(p *store.AgentProfile) { p.Notes = staleNotes })

	t.Setenv("SMIND_AGENT_RUN", "1")
	var code int
	stderr := captureStderr(t, func() { code = run([]string{"profile", "seed", "roles", "--apply"}) })
	if code != 1 || !strings.Contains(stderr, "is for humans, not agents") {
		t.Fatalf("--apply in an agent run = %d, stderr %q; want 1 and the refusal", code, stderr)
	}
	t.Setenv("SMIND_AGENT_RUN", "")
	if got := seatProfile(t, s, "peer-sonnet").Notes; got != staleNotes {
		t.Errorf("notes = %q, want nothing written", got)
	}
}

func TestProfileSeedRoles_UsageErrors(t *testing.T) {
	newTestProfileDaemon(t)
	humanEnv(t)
	for _, args := range [][]string{
		{"roles", "--check", "--apply"},
		{"roles", "--take", "lead"},
		{"roles", "--apply", "--take", "nobody"},
		{"roles", "--bogus"},
		{"roles", "--diff", "--check"},
	} {
		var code int
		stderr := captureStderr(t, func() { code = run(append([]string{"profile", "seed"}, args...)) })
		if code != 2 {
			t.Errorf("run(profile seed %v) = %d, want 2; stderr %q", args, code, stderr)
		}
		if !strings.Contains(stderr, "usage: smind profile seed roles [--check") {
			t.Errorf("run(profile seed %v) stderr %q, want the usage line", args, stderr)
		}
	}
}

func TestProfileSeedRolesApply_NoBackupNoWrite(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	humanEnv(t)
	seedRolePack(t)
	storeSetSeat(t, s, "peer-sonnet", func(p *store.AgentProfile) { p.Notes = staleNotes })

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("a regular file"), 0o600); err != nil {
		t.Fatalf("write the blocker: %v", err)
	}
	var code int
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			code = run([]string{"profile", "seed", "roles", "--apply", "--backup-dir", filepath.Join(blocker, "sub")})
		})
	})
	if code != 1 || !strings.Contains(stderr, "profile seed:") {
		t.Fatalf("--apply with an unusable backup dir = %d, stderr %q; want 1 and the error", code, stderr)
	}
	if got := seatProfile(t, s, "peer-sonnet").Notes; got != staleNotes {
		t.Errorf("notes = %q, want nothing written without a backup", got)
	}
}
