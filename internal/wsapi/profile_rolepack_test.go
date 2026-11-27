package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

// The role-pack sync RPCs (ADR-0058 PR 2). These tests stage the engine's
// check through the rolePackCheck/rolePackBackupRoot package vars (so no real
// pack text has to be written first), which is why none of them is parallel.

// rolePackCraft is the staged check: the lead profile the tests create, the
// state its model field reports, how much older than the profile's updatedAt
// the check claims to have read it, and an all-current switch.
type rolePackCraft struct {
	profile    store.AgentProfile
	modelState string
	age        time.Duration
	allCurrent bool
}

// build is the crafted []profiles.SeatSync the handler's rolePackCheck returns.
func (c *rolePackCraft) build() []profiles.SeatSync {
	s := profiles.SeatSync{Name: "lead", ID: c.profile.ID, Status: profiles.SeatOK, UpdatedAt: c.profile.UpdatedAt.Add(-c.age)}
	model := c.modelState
	if model == "" {
		model = profiles.StateCustomised
	}
	state := func(st string) string {
		if c.allCurrent {
			return profiles.StateCurrent
		}
		return st
	}
	s.Fields = []profiles.SyncField{
		{Name: "instructions", Kind: profiles.KindPrompt, State: state(profiles.StateStale), Stored: "OLD LEAD TEXT\nline two", Want: "NEW LEAD TEXT\nline two"},
		{Name: "notes", Kind: profiles.KindPrompt, State: state(profiles.StateCurrent), Stored: "x", Want: "x"},
		{Name: "skills", Kind: profiles.KindPrompt, State: state(profiles.StateCurrent)},
		{Name: "model", Kind: profiles.KindCapability, State: state(model), Stored: "m1", Want: "m2"},
		{Name: "permissionMode", Kind: profiles.KindCapability, State: state(profiles.StateStale), Stored: "", Want: "plan"},
		{Name: "orchestrationTools", Kind: profiles.KindCapability, State: state(profiles.StateCurrent)},
		{Name: "childPolicy", Kind: profiles.KindCapability, State: state(profiles.StateCurrent)},
		{Name: "onRateLimit", Kind: profiles.KindCapability, State: state(profiles.StateCurrent)},
	}
	out := []profiles.SeatSync{s}
	for _, name := range []string{"peer-sonnet", "validator", "steward", "steward-standards"} {
		out = append(out, profiles.SeatSync{Name: name, Status: profiles.SeatMissing})
	}
	return out
}

// stageRolePackCheck swaps rolePackCheck for craft and the backup root for a
// temp dir, restoring both when the test ends.
func stageRolePackCheck(t *testing.T, craft *rolePackCraft) {
	t.Helper()
	oldCheck, oldRoot := rolePackCheck, rolePackBackupRoot
	rolePackCheck = func([]store.AgentProfile) []profiles.SeatSync { return craft.build() }
	rolePackBackupRoot = func() string { return t.TempDir() }
	t.Cleanup(func() { rolePackCheck, rolePackBackupRoot = oldCheck, oldRoot })
}

// leadSeatBuild is the real pack's lead profile (what an apply writes).
func leadSeatBuild(t *testing.T) store.AgentProfile {
	t.Helper()
	for _, s := range profiles.RoleSeats {
		if s.Name == "lead" {
			return s.Build(nil)
		}
	}
	t.Fatal("no lead seat in the pack")
	return store.AgentProfile{}
}

// rolePackTestConn starts a server, creates the lead profile the crafted
// check talks about and returns a call helper with that profile read back.
func rolePackTestConn(t *testing.T) (func(id, method string, params any) envelope, store.AgentProfile) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")
	call := func(id, method string, params any) envelope {
		sendRequest(t, ws, id, method, params)
		return readEnvelopeFor(t, ws, id, 5*time.Second)
	}
	resp := call("c", "profile.create", map[string]any{
		"name": "lead", "provider": "claude-native",
		"instructions": "OLD LEAD TEXT\nline two", "notes": "old notes", "permissionMode": "",
	})
	if resp.Error != nil {
		t.Fatalf("profile.create error = %v", resp.Error.Message)
	}
	var created store.AgentProfile
	mustDecode(t, resp.Result, &created)
	resp = call("g", "profile.get", map[string]any{"id": created.ID})
	if resp.Error != nil {
		t.Fatalf("profile.get error = %v", resp.Error.Message)
	}
	var lead store.AgentProfile
	mustDecode(t, resp.Result, &lead)
	return call, lead
}

// storedLead reads the lead profile back by id.
func storedLead(t *testing.T, call func(id, method string, params any) envelope, id int64) store.AgentProfile {
	t.Helper()
	resp := call("g", "profile.get", map[string]any{"id": id})
	if resp.Error != nil {
		t.Fatalf("profile.get error = %v", resp.Error.Message)
	}
	var p store.AgentProfile
	mustDecode(t, resp.Result, &p)
	return p
}

func TestProfileRolePack_CheckShape(t *testing.T) {
	call, lead := rolePackTestConn(t)
	craft := &rolePackCraft{profile: lead}
	stageRolePackCheck(t, craft)

	resp := call("chk", "profile.rolePack.check", nil)
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.check error = %v", resp.Error.Message)
	}
	var v rolePackCheckView
	mustDecode(t, resp.Result, &v)
	if v.PackVersion != profiles.PackVersion {
		t.Fatalf("packVersion = %d, want %d", v.PackVersion, profiles.PackVersion)
	}
	if v.SeatsWithUpdates != 1 {
		t.Fatalf("seatsWithUpdates = %d, want 1", v.SeatsWithUpdates)
	}
	wantSeats := []string{"lead", "peer-sonnet", "validator", "steward", "steward-standards"}
	if len(v.Seats) != len(wantSeats) {
		t.Fatalf("seats = %d, want %d", len(v.Seats), len(wantSeats))
	}
	for i, want := range wantSeats {
		if v.Seats[i].Name != want {
			t.Fatalf("seat %d = %q, want %q", i, v.Seats[i].Name, want)
		}
	}
	seat := v.Seats[0]
	if seat.ID != lead.ID {
		t.Fatalf("lead id = %d, want %d", seat.ID, lead.ID)
	}
	if seat.Untouched {
		t.Fatal("lead untouched = true, want false (a customised field exists)")
	}
	if !seat.HasUpdates {
		t.Fatal("lead hasUpdates = false, want true")
	}
	field := func(name string) rolePackFieldView {
		for _, f := range seat.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("no %s field in the lead seat", name)
		return rolePackFieldView{}
	}
	instr := field("instructions")
	if instr.State != profiles.StateStale {
		t.Fatalf("instructions state = %q, want stale", instr.State)
	}
	if !strings.Contains(instr.Diff, "-OLD LEAD TEXT") || !strings.Contains(instr.Diff, "+NEW LEAD TEXT") {
		t.Fatalf("instructions diff = %q, want -OLD LEAD TEXT and +NEW LEAD TEXT", instr.Diff)
	}
	if field("notes").Diff != "" {
		t.Fatalf("notes diff = %q, want empty for a current field", field("notes").Diff)
	}
	model := field("model")
	if model.Stored != "m1" || model.Want != "m2" {
		t.Fatalf("model stored/want = %q/%q, want m1/m2", model.Stored, model.Want)
	}
	if model.Diff == "" {
		t.Fatal("model diff is empty, want one for a non-current field")
	}

	var raw map[string]any
	mustDecode(t, resp.Result, &raw)
	seats := raw["seats"].([]any)
	leadRaw := seats[0].(map[string]any)
	instrRaw := leadRaw["fields"].([]any)[0].(map[string]any)
	for _, key := range []string{"stored", "want"} {
		if _, ok := instrRaw[key]; ok {
			t.Fatalf("prompt field instructions carries %q in the raw JSON: %v", key, instrRaw)
		}
	}
	for i := 1; i < len(wantSeats); i++ {
		fields, ok := seats[i].(map[string]any)["fields"].([]any)
		if !ok || len(fields) != 0 {
			t.Fatalf("missing seat %s fields = %#v, want an empty JSON array", wantSeats[i], seats[i].(map[string]any)["fields"])
		}
	}
}

func TestProfileRolePack_CheckIsReadOnly(t *testing.T) {
	call, lead := rolePackTestConn(t)
	stageRolePackCheck(t, &rolePackCraft{profile: lead})

	listJSON := func() string {
		resp := call("l", "profile.list", nil)
		if resp.Error != nil {
			t.Fatalf("profile.list error = %v", resp.Error.Message)
		}
		return string(resp.Result)
	}
	before := listJSON()
	for i := 0; i < 2; i++ {
		if resp := call("chk", "profile.rolePack.check", nil); resp.Error != nil {
			t.Fatalf("profile.rolePack.check error = %v", resp.Error.Message)
		}
	}
	if after := listJSON(); after != before {
		t.Fatalf("profile.list changed across two checks:\nbefore %s\nafter  %s", before, after)
	}
	entries, err := os.ReadDir(rolePackBackupRoot())
	if err != nil || len(entries) != 0 {
		t.Fatalf("backups under %s = %v, %v: a read must create nothing", rolePackBackupRoot(), entries, err)
	}
}

func TestProfileRolePack_NotRunScoped(t *testing.T) {
	for _, m := range []string{"profile.rolePack.check", "profile.rolePack.apply"} {
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is run-scoped, want callable only by the human", m)
		}
	}
}

func TestProfileRolePack_RPCHumanOnly(t *testing.T) {
	if !slices.Contains(permpolicy.HumanOnlyRPCs, "profile.rolePack.apply") {
		t.Error("profile.rolePack.apply is missing from permpolicy.HumanOnlyRPCs")
	}
	if slices.Contains(RunScopedMethods, "profile.rolePack.apply") {
		t.Error("profile.rolePack.apply is run-scoped")
	}
	if !slices.Contains(pluginNeverMethods, "profile.rolePack.apply") {
		t.Error("profile.rolePack.apply is missing from the plugin session's refused method list")
	}
}

func TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly(t *testing.T) {
	call, lead := rolePackTestConn(t)
	craft := &rolePackCraft{profile: lead}
	stageRolePackCheck(t, craft)

	// A customised field (model) makes the seat touched: nothing is written
	// by default, not even the stale prompt field.
	resp := call("apply", "profile.rolePack.apply", nil)
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	var v rolePackApplyView
	mustDecode(t, resp.Result, &v)
	if len(v.Results) != 0 {
		t.Fatalf("results = %+v, want none for a customised seat", v.Results)
	}
	if got := storedLead(t, call, lead.ID); got.Instructions != "OLD LEAD TEXT\nline two" {
		t.Fatalf("stored instructions = %q, want unchanged", got.Instructions)
	}

	// Untouched: only the stale PROMPT field is written; the stale capability
	// field (permissionMode) is not.
	craft.modelState = profiles.StateCurrent
	resp = call("apply2", "profile.rolePack.apply", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	mustDecode(t, resp.Result, &v)
	if len(v.Results) != 1 || v.Results[0].Seat != "lead" || v.Results[0].Outcome != profiles.OutcomeUpdated {
		t.Fatalf("results = %+v, want one updated lead", v.Results)
	}
	if got := v.Results[0].Fields; !slices.Equal(got, []string{"instructions"}) {
		t.Fatalf("fields = %q, want [instructions] only", got)
	}
	got := storedLead(t, call, lead.ID)
	if want := leadSeatBuild(t).Instructions; got.Instructions != want {
		t.Fatalf("stored instructions = %q, want the pack's lead text", got.Instructions)
	}
	if got.PermissionMode != "" {
		t.Fatalf("stored permissionMode = %q, want unchanged: a capability field needs a take", got.PermissionMode)
	}
}

func TestProfileRolePack_ApplyTakeChangesACapabilityField(t *testing.T) {
	call, lead := rolePackTestConn(t)
	stageRolePackCheck(t, &rolePackCraft{profile: lead})

	resp := call("apply", "profile.rolePack.apply", map[string]any{"take": []string{"lead.permissionMode"}})
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	var v rolePackApplyView
	mustDecode(t, resp.Result, &v)
	if len(v.Results) != 1 || v.Results[0].Outcome != profiles.OutcomeUpdated {
		t.Fatalf("results = %+v, want one updated entry", v.Results)
	}
	if got := v.Results[0].Fields; !slices.Equal(got, []string{"permissionMode"}) {
		t.Fatalf("fields = %q, want [permissionMode]", got)
	}
	if got := storedLead(t, call, lead.ID); got.PermissionMode != leadSeatBuild(t).PermissionMode {
		t.Fatalf("stored permissionMode = %q, want the pack's %q", got.PermissionMode, leadSeatBuild(t).PermissionMode)
	}
}

func TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate(t *testing.T) {
	call, lead := rolePackTestConn(t)
	craft := &rolePackCraft{profile: lead, modelState: profiles.StateCurrent}
	stageRolePackCheck(t, craft)
	backupDir := t.TempDir()

	resp := call("apply", "profile.rolePack.apply", map[string]any{"backupDir": backupDir})
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	var v rolePackApplyView
	mustDecode(t, resp.Result, &v)
	if v.BackupDir == "" {
		t.Fatal("backupDir is empty, want the timestamped directory")
	}
	backupFile := filepath.Join(v.BackupDir, "lead-"+strconv.FormatInt(lead.ID, 10)+".json")
	data, err := os.ReadFile(backupFile)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var backed store.AgentProfile
	if err := json.Unmarshal(data, &backed); err != nil {
		t.Fatalf("decode backup: %v", err)
	}
	if backed.Instructions != "OLD LEAD TEXT\nline two" {
		t.Fatalf("backup instructions = %q, want the BEFORE state", backed.Instructions)
	}
	if fi, err := os.Stat(backupFile); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, %v, want 0600", fi, err)
	}

	// A backup path that cannot be created: no backup, no write.
	plainFile := filepath.Join(t.TempDir(), "a-regular-file")
	if err := os.WriteFile(plainFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp = call("apply2", "profile.rolePack.apply", map[string]any{"backupDir": filepath.Join(plainFile, "under-a-file")})
	if resp.Error == nil {
		t.Fatal("profile.rolePack.apply under a regular file error = nil, want an error")
	}
	if got := storedLead(t, call, lead.ID); got.Instructions != leadSeatBuild(t).Instructions {
		t.Fatalf("stored instructions = %q, want the already-applied pack text (no second write)", got.Instructions)
	}
}

func TestProfileRolePack_ApplyDryRunWritesNothing(t *testing.T) {
	call, lead := rolePackTestConn(t)
	craft := &rolePackCraft{profile: lead, modelState: profiles.StateCurrent}
	stageRolePackCheck(t, craft)

	resp := call("apply", "profile.rolePack.apply", map[string]any{"dryRun": true})
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	var v rolePackApplyView
	mustDecode(t, resp.Result, &v)
	if v.BackupDir != "" {
		t.Fatalf("backupDir = %q, want empty for a dry run", v.BackupDir)
	}
	if len(v.Results) != 1 || v.Results[0].Outcome != profiles.OutcomeDryRun {
		t.Fatalf("results = %+v, want one dry_run entry", v.Results)
	}
	if got := storedLead(t, call, lead.ID); got.Instructions != "OLD LEAD TEXT\nline two" {
		t.Fatalf("stored instructions = %q, want unchanged by a dry run", got.Instructions)
	}
	entries, err := os.ReadDir(rolePackBackupRoot())
	if err != nil || len(entries) != 0 {
		t.Fatalf("backups under %s = %v, %v: a dry run must create nothing", rolePackBackupRoot(), entries, err)
	}
}

func TestProfileRolePack_ApplyChangedMeanwhile(t *testing.T) {
	call, lead := rolePackTestConn(t)
	stageRolePackCheck(t, &rolePackCraft{profile: lead, modelState: profiles.StateCurrent, age: time.Hour})

	resp := call("apply", "profile.rolePack.apply", nil)
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	var v rolePackApplyView
	mustDecode(t, resp.Result, &v)
	if len(v.Results) != 1 || v.Results[0].Outcome != profiles.OutcomeChangedMeanwhile {
		t.Fatalf("results = %+v, want one changed_meanwhile entry", v.Results)
	}
	if got := storedLead(t, call, lead.ID); got.Instructions != "OLD LEAD TEXT\nline two" {
		t.Fatalf("stored instructions = %q, want unchanged: the profile changed since the check", got.Instructions)
	}
}

func TestProfileRolePack_ApplyIsIdempotent(t *testing.T) {
	call, lead := rolePackTestConn(t)
	craft := &rolePackCraft{profile: lead, modelState: profiles.StateCurrent}
	stageRolePackCheck(t, craft)

	resp := call("apply", "profile.rolePack.apply", nil)
	if resp.Error != nil {
		t.Fatalf("profile.rolePack.apply error = %v", resp.Error.Message)
	}
	var v rolePackApplyView
	mustDecode(t, resp.Result, &v)
	if len(v.Results) != 1 || v.Results[0].Outcome != profiles.OutcomeUpdated {
		t.Fatalf("results = %+v, want one updated entry", v.Results)
	}

	craft.allCurrent = true
	resp = call("apply2", "profile.rolePack.apply", nil)
	if resp.Error != nil {
		t.Fatalf("second profile.rolePack.apply error = %v", resp.Error.Message)
	}
	mustDecode(t, resp.Result, &v)
	if len(v.Results) != 0 {
		t.Fatalf("second results = %+v, want nothing left to do", v.Results)
	}
}

func TestProfileRolePack_ApplyRejectsAMalformedTake(t *testing.T) {
	call, lead := rolePackTestConn(t)
	stageRolePackCheck(t, &rolePackCraft{profile: lead})

	for _, take := range []string{"nosuchseat", "lead.nosuchfield"} {
		resp := call("apply", "profile.rolePack.apply", map[string]any{"take": []string{take}})
		if resp.Error == nil {
			t.Fatalf("take %q error = nil, want an error naming it", take)
		}
		if !strings.Contains(resp.Error.Message, take) {
			t.Fatalf("take %q error message = %q, want it to name the take", take, resp.Error.Message)
		}
		if got := storedLead(t, call, lead.ID); got.Instructions != "OLD LEAD TEXT\nline two" {
			t.Fatalf("stored instructions = %q, want unchanged: a malformed take writes nothing", got.Instructions)
		}
		entries, err := os.ReadDir(rolePackBackupRoot())
		if err != nil || len(entries) != 0 {
			t.Fatalf("backups under %s = %v, %v: a malformed take writes nothing", rolePackBackupRoot(), entries, err)
		}
	}
}
