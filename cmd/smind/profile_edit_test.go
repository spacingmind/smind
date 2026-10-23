package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

// addProfile runs `profile add` against the test daemon and returns the
// stored profile it created (the last one by id).
func addProfile(t *testing.T, s *store.Store, args ...string) store.AgentProfile {
	t.Helper()
	full := append([]string{"profile", "add"}, args...)
	var code int
	captureStdout(t, func() { code = run(full) })
	if code != 0 {
		t.Fatalf("run(%v) = %d, want 0", full, code)
	}
	return mustGetProfile(t, profiles.New(s), lastProfileID(t, s))
}

// editProfile runs `profile edit` and returns its exit code.
func editProfile(t *testing.T, args ...string) int {
	t.Helper()
	full := append([]string{"profile", "edit"}, args...)
	var code int
	captureStdout(t, func() { code = run(full) })
	return code
}

func lastProfileID(t *testing.T, s *store.Store) int64 {
	t.Helper()
	list, err := profiles.New(s).List()
	if err != nil || len(list) == 0 {
		t.Fatalf("List() = %+v, %v, want at least one profile", list, err)
	}
	return list[len(list)-1].ID
}

// mustGetProfile fetches one stored profile by id.
func mustGetProfile(t *testing.T, reg *profiles.Registry, id int64) store.AgentProfile {
	t.Helper()
	list, err := reg.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, p := range list {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("profile %d not stored: %+v", id, list)
	return store.AgentProfile{}
}

// TestProfileEditChangesOnlyNamedFields proves edit overlays only the
// flags given: every other stored field stays identical.
func TestProfileEditChangesOnlyNamedFields(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)

	before := addProfile(t, s, "seat", "claude-native",
		"--model=haiku", "--notes=old", "--on-rate-limit=wait",
		"--instructions=x", "--skills=smind-peer", "--thinking-level=extended")

	if code := editProfile(t, strconv.FormatInt(before.ID, 10), "--notes=new"); code != 0 {
		t.Fatalf("run(profile edit --notes=new) = %d, want 0", code)
	}
	after := mustGetProfile(t, reg, before.ID)

	if after.Notes != "new" {
		t.Errorf("Notes = %q, want %q", after.Notes, "new")
	}
	if after.Name != before.Name || after.Provider != before.Provider ||
		after.PermissionMode != before.PermissionMode || after.AutoAccept != before.AutoAccept ||
		after.ThinkingLevel != before.ThinkingLevel || after.Model != before.Model ||
		after.OnRateLimit != before.OnRateLimit || after.Instructions != before.Instructions ||
		after.ChildPolicy != before.ChildPolicy || after.CreatedAt != before.CreatedAt {
		t.Errorf("edit touched untouched fields:\nbefore = %+v\nafter  = %+v", before, after)
	}
	if !reflect.DeepEqual(after.Skills, before.Skills) || !reflect.DeepEqual(after.ChildProfileIDs, before.ChildProfileIDs) {
		t.Errorf("edit touched slice fields:\nbefore = %+v/%+v\nafter  = %+v/%+v",
			before.Skills, before.ChildProfileIDs, after.Skills, after.ChildProfileIDs)
	}
	if (before.FallbackProfileID == nil) != (after.FallbackProfileID == nil) ||
		(before.FallbackProfileID != nil && *before.FallbackProfileID != *after.FallbackProfileID) {
		t.Errorf("edit touched fallback: before %v after %v", before.FallbackProfileID, after.FallbackProfileID)
	}
	if (before.OrchestrationTools == nil) != (after.OrchestrationTools == nil) ||
		(before.OrchestrationTools != nil && *before.OrchestrationTools != *after.OrchestrationTools) {
		t.Errorf("edit touched orchestrationTools: before %v after %v", before.OrchestrationTools, after.OrchestrationTools)
	}
}

// TestProfileEditClearsModelAndFallback proves the clear spellings:
// `--model=` empties the model and `--fallback-profile=0` drops the
// fallback while --on-rate-limit=none turns failover off.
func TestProfileEditClearsModelAndFallback(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)

	target, err := reg.Create(store.AgentProfile{Name: "target", Provider: "glm"})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	main := addProfile(t, s, "main", "claude-native", "--model=haiku",
		"--on-rate-limit=fallback", "--fallback-profile="+strconv.FormatInt(target.ID, 10))

	if code := editProfile(t, strconv.FormatInt(main.ID, 10),
		"--on-rate-limit=none", "--fallback-profile=0", "--model="); code != 0 {
		t.Fatalf("run(profile edit) = %d, want 0", code)
	}
	after := mustGetProfile(t, reg, main.ID)
	if after.Model != "" {
		t.Errorf("Model = %q, want cleared", after.Model)
	}
	if after.FallbackProfileID != nil {
		t.Errorf("FallbackProfileID = %d, want nil", *after.FallbackProfileID)
	}
	if after.OnRateLimit != "none" {
		t.Errorf("OnRateLimit = %q, want none", after.OnRateLimit)
	}
}

// TestProfileEditOrchestrationToolsUnmanaged proves edit's
// --orchestration-tools=unmanaged resets the seat to a legacy profile
// (nil orchestrationTools), while add keeps rejecting the word.
func TestProfileEditOrchestrationToolsUnmanaged(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)

	p := addProfile(t, s, "lead", "claude-native", "--orchestration-tools=true", "--instructions=x", "--skills=smind-peer")

	if code := editProfile(t, strconv.FormatInt(p.ID, 10),
		"--orchestration-tools=unmanaged", "--instructions=", "--skills="); code != 0 {
		t.Fatalf("run(profile edit --orchestration-tools=unmanaged) = %d, want 0", code)
	}
	after := mustGetProfile(t, reg, p.ID)
	if after.OrchestrationTools != nil {
		t.Errorf("OrchestrationTools = %v, want nil", *after.OrchestrationTools)
	}

	var code int
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			code = run([]string{"profile", "add", "x", "claude-native", "--orchestration-tools=unmanaged"})
		})
	})
	if code != 2 || !strings.Contains(stderr, "not a boolean") {
		t.Fatalf("add --orchestration-tools=unmanaged = %d, stderr %q; want 2 saying not a boolean", code, stderr)
	}
}

// TestProfileEditSurfacesDaemonValidationError proves the daemon
// registry's rejection text reaches stderr and the stored profile is left
// unchanged.
func TestProfileEditSurfacesDaemonValidationError(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)

	before := addProfile(t, s, "sup", "claude-native")

	var code int
	stderr := captureStderr(t, func() {
		code = editProfile(t, strconv.FormatInt(before.ID, 10), "--child-policy=profiles")
	})
	if code != 1 || !strings.Contains(stderr, "childProfileIds") {
		t.Fatalf("run(profile edit --child-policy=profiles) = %d, stderr %q; want 1 naming childProfileIds", code, stderr)
	}
	after := mustGetProfile(t, reg, before.ID)
	if after.ChildPolicy != before.ChildPolicy || after.Name != before.Name || after.Model != before.Model {
		t.Fatalf("rejected edit changed the profile:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

// TestProfileEditUnknownID proves editing a missing id exits 1 saying so.
func TestProfileEditUnknownID(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	stderr := captureStderr(t, func() { code = editProfile(t, "999", "--notes=x") })
	if code != 1 || !strings.Contains(stderr, "not found") {
		t.Fatalf("run(profile edit 999) = %d, stderr %q; want 1 saying not found", code, stderr)
	}
}

// TestProfileEditNoFlagsUsage proves a flagless edit prints usage and
// exits 2.
func TestProfileEditNoFlagsUsage(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	stderr := captureStderr(t, func() { code = editProfile(t, "1") })
	if code != 2 || !strings.Contains(stderr, "usage: smind profile edit") {
		t.Fatalf("run(profile edit 1) = %d, stderr %q; want 2 with usage", code, stderr)
	}
}

// TestProfileEditInstructionsFile proves --instructions-file loads the
// role text from disk and still conflicts with --instructions.
func TestProfileEditInstructionsFile(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	file := filepath.Join(t.TempDir(), "role.md")
	if err := os.WriteFile(file, []byte("from a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := addProfile(t, s, "filed", "claude-native", "--instructions=inline")

	if code := editProfile(t, strconv.FormatInt(p.ID, 10), "--instructions-file="+file); code != 0 {
		t.Fatalf("run(profile edit --instructions-file) = %d, want 0", code)
	}
	if after := mustGetProfile(t, reg, p.ID); after.Instructions != "from a file\n" {
		t.Fatalf("Instructions = %q, want the file body", after.Instructions)
	}

	var code int
	stderr := captureStderr(t, func() {
		code = editProfile(t, strconv.FormatInt(p.ID, 10), "--instructions=x", "--instructions-file="+file)
	})
	if code != 2 || !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("both flags = %d, stderr %q; want 2 saying mutually exclusive", code, stderr)
	}
	if after := mustGetProfile(t, reg, p.ID); after.Instructions != "from a file\n" {
		t.Fatalf("rejected edit changed instructions to %q", after.Instructions)
	}
}
