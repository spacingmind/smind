package profiles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// seededPack is the pack as a fresh seeding would have stored it: one profile per seat
// with an id and an updatedAt, like profile.list answers.
func seededPack() []store.AgentProfile {
	var out []store.AgentProfile
	ids := map[string]int64{}
	for i, seat := range RoleSeats {
		p := seat.Build(ids)
		p.ID = int64(i + 1)
		p.OnRateLimit = fieldValue(p, "onRateLimit")
		p.UpdatedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		ids[seat.Name] = p.ID
		out = append(out, p)
	}
	return out
}

func seatOf(t *testing.T, pack []store.AgentProfile, name string) *store.AgentProfile {
	t.Helper()
	for i := range pack {
		if pack[i].Name == name {
			return &pack[i]
		}
	}
	t.Fatalf("no seat %q", name)
	return nil
}

// historyWith is the real history plus one older text for a seat's field.
func historyWith(seat, field, oldText string) map[string]map[string][]string {
	h := map[string]map[string][]string{}
	for s, fs := range roleHistory {
		h[s] = map[string][]string{}
		for f, v := range fs {
			h[s][f] = slices.Clone(v)
		}
	}
	h[seat][field] = append(h[seat][field], fieldHash(oldText))
	return h
}

func fieldState(t *testing.T, checks []SeatSync, seat, field string) string {
	t.Helper()
	for _, c := range checks {
		if c.Name != seat {
			continue
		}
		for _, f := range c.Fields {
			if f.Name == field {
				return f.State
			}
		}
	}
	t.Fatalf("no field %s.%s", seat, field)
	return ""
}

// The drift guard: a release that changes a seat's text without recording the old and
// new hashes in rolepack_history.go fails here.
func TestRolePackHistory_ContainsTheCurrentText(t *testing.T) {
	t.Parallel()
	ids := map[string]int64{}
	for _, seat := range RoleSeats {
		p := seat.Build(ids)
		for _, f := range syncedFields {
			h := fieldHash(fieldValue(p, f.name))
			if !slices.Contains(roleHistory[seat.Name][f.name], h) {
				t.Errorf("%s.%s: the current value's hash %s is missing from roleHistory: add it (rolepack_history.go) when a release changes a seat's text", seat.Name, f.name, h)
			}
		}
	}
}

func TestRolePackSync_ClassifiesCurrentStaleCustomised(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	if got := CheckRoles(pack); len(got) != len(RoleSeats) || got[0].HasUpdates() {
		t.Fatalf("a fresh pack = %+v, want every field current", got)
	}
	const old = "You are a Lead. Old text of an earlier release."
	seatOf(t, pack, "lead").Instructions = old
	seatOf(t, pack, "peer-sonnet").Instructions = "My own peer rules"
	checks := checkRoles(pack, historyWith("lead", "instructions", old))
	if got := fieldState(t, checks, "lead", "instructions"); got != StateStale {
		t.Errorf("lead.instructions = %s, want stale (an earlier release's text)", got)
	}
	if got := fieldState(t, checks, "peer-sonnet", "instructions"); got != StateCustomised {
		t.Errorf("peer-sonnet.instructions = %s, want customised", got)
	}
	if got := fieldState(t, checks, "validator", "instructions"); got != StateCurrent {
		t.Errorf("validator.instructions = %s, want current", got)
	}
}

func TestRolePackSync_WhitespaceOnlyDifferenceIsNotCustomised(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	l := seatOf(t, pack, "lead")
	l.Instructions = strings.ReplaceAll(l.Instructions, "\n", "  \r\n") + "\n\n"
	if got := fieldState(t, CheckRoles(pack), "lead", "instructions"); got != StateCurrent {
		t.Fatalf("lead.instructions with only whitespace edits = %s, want current", got)
	}
}

func TestRolePackSync_AnEmptyStoredValueIsCustomisedNotStale(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	seatOf(t, pack, "peer-sonnet").Instructions = ""
	checks := CheckRoles(pack)
	if got := fieldState(t, checks, "peer-sonnet", "instructions"); got != StateCustomised {
		t.Fatalf("peer-sonnet.instructions cleared = %s, want customised (never overwritten unasked)", got)
	}
	if items := PlanApply(checks, pack, nil); len(items) != 0 {
		t.Fatalf("PlanApply over a cleared field = %d items, want none", len(items))
	}
}

func TestRolePackSync_ASymlinkedBackupDirIsRefused(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	seatOf(t, pack, "lead").Instructions = "x"
	real, link := t.TempDir(), filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	items := PlanApply(checkRoles(pack, historyWith("lead", "instructions", "x")), pack, []Take{{Seat: "lead", Field: "instructions"}})
	called := false
	_, _, err := ApplyRoles(items, ApplyOptions{BackupDir: link, Update: func(p store.AgentProfile, _ time.Time) (store.AgentProfile, error) { called = true; return p, nil }})
	if err == nil || called {
		t.Fatalf("ApplyRoles into a symlinked dir: err = %v, update called = %v; want a refusal and no write", err, called)
	}
}

func TestRolePackSync_MissingAndOwnedSeatsAreReported(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	pack = slices.DeleteFunc(pack, func(p store.AgentProfile) bool { return p.Name == "validator" })
	owner := "plugin-x"
	seatOf(t, pack, "steward").Owner = &owner
	checks := CheckRoles(pack)
	status := map[string]string{}
	for _, c := range checks {
		status[c.Name] = c.Status
	}
	if status["validator"] != SeatMissing || status["steward"] != SeatOwned || status["lead"] != SeatOK {
		t.Fatalf("statuses = %v", status)
	}
	if items := PlanApply(checks, pack, []Take{{Seat: "validator"}, {Seat: "steward"}}); len(items) != 0 {
		t.Fatalf("a missing or owned seat produced writes: %+v", items)
	}
}

func staleLead(t *testing.T) ([]store.AgentProfile, []SeatSync) {
	t.Helper()
	pack := seededPack()
	const old = "Old lead text."
	seatOf(t, pack, "lead").Instructions = old
	return pack, checkRoles(pack, historyWith("lead", "instructions", old))
}

func TestRolePackSync_ApplyUpdatesOnlyStalePromptFieldsOfAnUntouchedSeat(t *testing.T) {
	t.Parallel()
	pack, checks := staleLead(t)
	// a stale capability field of the same untouched seat: never written without a take
	oldMode := "plan"
	lead := seatOf(t, pack, "lead")
	lead.PermissionMode = oldMode
	checks = checkRoles(pack, func() map[string]map[string][]string {
		h := historyWith("lead", "instructions", "Old lead text.")
		h["lead"]["permissionMode"] = append(h["lead"]["permissionMode"], fieldHash(oldMode))
		return h
	}())
	items := PlanApply(checks, pack, nil)
	if len(items) != 1 || items[0].Seat != "lead" || !slices.Equal(items[0].Fields, []string{"instructions"}) {
		t.Fatalf("plan = %+v, want only lead.instructions", items)
	}
	if items[0].After.Instructions != RoleSeats[1].Build(nil).Instructions || items[0].After.PermissionMode != oldMode {
		t.Fatalf("after = %+v, want the new instructions and the capability field untouched", items[0].After)
	}
}

func TestRolePackSync_CustomisedSeatIsNeverWrittenWithoutATake(t *testing.T) {
	t.Parallel()
	pack, _ := staleLead(t)
	seatOf(t, pack, "lead").Notes = "my own notes"
	checks := checkRoles(pack, historyWith("lead", "instructions", "Old lead text."))
	if items := PlanApply(checks, pack, nil); len(items) != 0 {
		t.Fatalf("plan = %+v: a seat with a customised field must not be written by default, even its stale fields", items)
	}
	items := PlanApply(checks, pack, []Take{{Seat: "lead", Field: "notes"}})
	if len(items) != 1 || !slices.Equal(items[0].Fields, []string{"notes"}) {
		t.Fatalf("plan with a take = %+v, want only the taken field", items)
	}
	if items[0].After.Instructions != "Old lead text." {
		t.Fatal("a take of one field rewrote another")
	}
}

func TestRolePackSync_CapabilityFieldNeedsAnExplicitTake(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	seatOf(t, pack, "peer-sonnet").PermissionMode = "bypass_permissions"
	checks := CheckRoles(pack)
	if got := fieldState(t, checks, "peer-sonnet", "permissionMode"); got != StateCustomised {
		t.Fatalf("state = %s", got)
	}
	if items := PlanApply(checks, pack, nil); len(items) != 0 {
		t.Fatalf("plan = %+v, want nothing without a take", items)
	}
	items := PlanApply(checks, pack, []Take{{Seat: "peer-sonnet", Field: "permissionMode"}})
	if len(items) != 1 || items[0].After.PermissionMode != "auto" {
		t.Fatalf("plan with a take = %+v", items)
	}
}

func TestRolePackSync_UserSettingsAreNeverWritten(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	fb := int64(99)
	lead := seatOf(t, pack, "lead")
	lead.Provider, lead.ThinkingLevel, lead.AutoAccept, lead.FallbackProfileID, lead.ChildProfileIDs = "glm", "high", true, &fb, []int64{3, 4}
	lead.Instructions = "mine"
	checks := CheckRoles(pack)
	items := PlanApply(checks, pack, []Take{{Seat: "lead"}}) // take everything
	if len(items) != 1 {
		t.Fatalf("plan = %+v", items)
	}
	a := items[0].After
	if a.Provider != "glm" || a.ThinkingLevel != "high" || !a.AutoAccept || a.FallbackProfileID == nil || *a.FallbackProfileID != 99 || !slices.Equal(a.ChildProfileIDs, []int64{3, 4}) || a.Name != "lead" || a.ID != lead.ID {
		t.Fatalf("after = %+v: the Human's own settings, the name and the id must survive a take of every field", a)
	}
}

func fakeUpdate(calls *[]string, fail map[string]error) func(store.AgentProfile, time.Time) (store.AgentProfile, error) {
	return func(p store.AgentProfile, _ time.Time) (store.AgentProfile, error) {
		*calls = append(*calls, p.Name)
		return p, fail[p.Name]
	}
}

func TestRolePackSync_BackupIsWrittenBeforeTheFirstUpdate(t *testing.T) {
	t.Parallel()
	pack, checks := staleLead(t)
	items := PlanApply(checks, pack, nil)
	dir := t.TempDir()
	var order []string
	update := func(p store.AgentProfile, _ time.Time) (store.AgentProfile, error) {
		matches, _ := filepath.Glob(filepath.Join(dir, "*", "lead-*.json"))
		order = append(order, p.Name+":backups="+string(rune('0'+len(matches))))
		return p, nil
	}
	backup, results, err := ApplyRoles(items, ApplyOptions{BackupDir: dir, Now: func() time.Time { return time.Date(2026, 11, 9, 1, 2, 3, 0, time.UTC) }, Update: update})
	if err != nil || len(results) != 1 || results[0].Outcome != OutcomeUpdated {
		t.Fatalf("ApplyRoles = %q, %+v, %v", backup, results, err)
	}
	if len(order) != 1 || order[0] != "lead:backups=1" {
		t.Fatalf("order = %v: the backup file must exist before the update runs", order)
	}
	if !strings.Contains(backup, "20261109T010203Z") {
		t.Fatalf("backup dir = %q", backup)
	}
	raw, err := os.ReadFile(filepath.Join(backup, "lead-2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved store.AgentProfile
	if json.Unmarshal(raw, &saved) != nil || saved.Instructions != "Old lead text." {
		t.Fatalf("the backup holds %+v, want the profile as it was before the update", saved)
	}
	fi, _ := os.Stat(filepath.Join(backup, "lead-2.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file mode = %v, want 0600", fi.Mode().Perm())
	}
	if di, _ := os.Stat(backup); di.Mode().Perm() != 0o700 {
		t.Fatalf("backup directory mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestRolePackSync_NoBackupNoWrite(t *testing.T) {
	t.Parallel()
	pack, checks := staleLead(t)
	items := PlanApply(checks, pack, nil)
	var calls []string
	if _, _, err := ApplyRoles(items, ApplyOptions{BackupDir: "", Update: fakeUpdate(&calls, nil)}); !errors.Is(err, ErrNoBackupDir) {
		t.Fatalf("error = %v, want ErrNoBackupDir", err)
	}
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyRoles(items, ApplyOptions{BackupDir: filepath.Join(file, "sub"), Update: fakeUpdate(&calls, nil)}); err == nil {
		t.Fatal("an unwritable backup directory was accepted")
	}
	if len(calls) != 0 {
		t.Fatalf("updates ran without a backup: %v", calls)
	}
}

func TestRolePackSync_ChangedMeanwhileIsSkippedAndTheRestProceed(t *testing.T) {
	t.Parallel()
	pack := seededPack()
	for _, n := range []string{"lead", "peer-sonnet"} {
		seatOf(t, pack, n).Instructions = "old " + n
	}
	h := historyWith("lead", "instructions", "old lead")
	h["peer-sonnet"]["instructions"] = append(h["peer-sonnet"]["instructions"], fieldHash("old peer-sonnet"))
	items := PlanApply(checkRoles(pack, h), pack, nil)
	if len(items) != 2 {
		t.Fatalf("plan = %+v", items)
	}
	var calls []string
	_, results, err := ApplyRoles(items, ApplyOptions{BackupDir: t.TempDir(), Update: fakeUpdate(&calls, map[string]error{"peer-sonnet": store.ErrAgentProfileChanged})})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range results {
		got[r.Seat] = r.Outcome
	}
	if got["peer-sonnet"] != OutcomeChangedMeanwhile || got["lead"] != OutcomeUpdated || len(calls) != 2 {
		t.Fatalf("results = %v calls = %v", got, calls)
	}
}

func TestRolePackSync_ApplyIsIdempotent(t *testing.T) {
	t.Parallel()
	pack, checks := staleLead(t)
	h := historyWith("lead", "instructions", "Old lead text.")
	items := PlanApply(checks, pack, nil)
	_, _, err := ApplyRoles(items, ApplyOptions{BackupDir: t.TempDir(), Update: func(p store.AgentProfile, _ time.Time) (store.AgentProfile, error) {
		*seatOf(t, pack, p.Name) = p
		return p, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	again := checkRoles(pack, h)
	for _, c := range again {
		if c.HasUpdates() {
			t.Fatalf("after apply %s still has updates: %+v", c.Name, c.Fields)
		}
	}
	if items := PlanApply(again, pack, nil); len(items) != 0 {
		t.Fatalf("a second apply plans writes: %+v", items)
	}
}

func TestRolePackSync_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	pack, checks := staleLead(t)
	dir := t.TempDir()
	var calls []string
	backup, results, err := ApplyRoles(PlanApply(checks, pack, nil), ApplyOptions{BackupDir: dir, DryRun: true, Update: fakeUpdate(&calls, nil)})
	if err != nil || backup != "" || len(calls) != 0 || len(results) != 1 || results[0].Outcome != OutcomeDryRun {
		t.Fatalf("dry run = %q %+v %v calls %v", backup, results, err, calls)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a dry run wrote a backup: %v", entries)
	}
}

func TestParseTake(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Take{"lead": {Seat: "lead"}, "lead.notes": {Seat: "lead", Field: "notes"}} {
		if got, err := ParseTake(in); err != nil || got != want {
			t.Errorf("ParseTake(%q) = %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", ".notes", "nobody", "lead.nope"} {
		if _, err := ParseTake(bad); err == nil {
			t.Errorf("ParseTake(%q) accepted", bad)
		}
	}
}
