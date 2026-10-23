package wsapi

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

func boolp(b bool) *bool { return &b }

// admEnv is an identEnv whose seat run was launched with a profile.
type admEnv struct {
	*identEnv
	profs *profiles.Registry
	seat  store.AgentProfile
}

// newAdmEnv builds a daemon, creates the seat profile (via mk, which may
// reference profiles created through e.mkProfile), launches the seat run with
// it and returns the env. mk receives the env so it can create peers first.
func newAdmEnv(t *testing.T, mk func(e *admEnv) store.AgentProfile) *admEnv {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	admin := dialWS(t, srv, "tok")
	e := &admEnv{
		identEnv: &identEnv{api: api, srv: srv, wm: wm, db: db, seatTask: task, admin: admin},
		profs:    profiles.New(db),
	}
	e.seat = mk(e)
	started := startRun(t, admin, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "seat", "profileId": e.seat.ID})
	row, err := db.GetRun(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	e.runID, e.chatID = started.RunID, row.ChatID
	return e
}

func (e *admEnv) mkProfile(t *testing.T, p store.AgentProfile) store.AgentProfile {
	t.Helper()
	if p.Provider == "" {
		p.Provider = "glm"
	}
	out, err := e.profs.Create(p)
	if err != nil {
		t.Fatalf("create profile %q: %v", p.Name, err)
	}
	return out
}

// peer is a leaf profile: orchestrationTools false.
func (e *admEnv) peer(t *testing.T, name string) store.AgentProfile {
	return e.mkProfile(t, store.AgentProfile{Name: name, OrchestrationTools: boolp(false), Instructions: "SECRET-" + name})
}

func seatProfile(name, policy string, ids ...int64) store.AgentProfile {
	return store.AgentProfile{Name: name, OrchestrationTools: boolp(true), ChildPolicy: policy, ChildProfileIDs: ids, Instructions: "SECRET-" + name}
}

// launch is the run.start params that match profile p exactly.
func launch(task int64, p store.AgentProfile, extra map[string]any) map[string]any {
	m := map[string]any{
		"taskId": task, "provider": p.Provider, "permissionMode": p.PermissionMode,
		"autoAccept": p.AutoAccept, "prompt": "go", "profileId": p.ID, "source": "orchestrator",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func wantErr(t *testing.T, resp envelope, substr string) {
	t.Helper()
	if resp.Error == nil || !strings.Contains(resp.Error.Message, substr) {
		t.Fatalf("response = %s / %+v, want error containing %q", resp.Result, resp.Error, substr)
	}
}

func (e *admEnv) taskUnder(t *testing.T, parent int64, scenario bool) store.Task {
	t.Helper()
	c, err := e.wm.CreateTask(e.seatTask.WorkspaceID, nil, "T", workspace.WithParentTask(parent))
	if err != nil {
		t.Fatal(err)
	}
	if scenario {
		writeScenario(t, c)
	}
	return c
}

// sibling is another root task in the seat's workspace (a sibling Lead's).
func (e *admEnv) sibling(t *testing.T) store.Task {
	t.Helper()
	c, err := e.wm.CreateTask(e.seatTask.WorkspaceID, nil, "Sibling")
	if err != nil {
		t.Fatal(err)
	}
	writeScenario(t, c)
	return c
}

func writeScenario(t *testing.T, c store.Task) {
	t.Helper()
	if err := os.WriteFile(*c.WorktreePath+"/scenario", []byte("hang"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdmission_PolicyNoneCallerRejected(t *testing.T) {
	t.Parallel()
	var peer store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		return e.mkProfile(t, seatProfile("none-seat", "none"))
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	const msg = "your seat may not launch or message other agents (childPolicy none)"
	wantErr(t, createChild(t, ws, e.identEnv, nil), msg)
	wantErr(t, callWS(t, ws, "run.start", launch(child.ID, peer, nil)), msg)
	wantErr(t, callWS(t, ws, "chat.create", map[string]any{"taskId": child.ID, "title": "c"}), msg)
}

func TestAdmission_LeadLeafOnlyLaunchesPeerProfiles(t *testing.T) {
	t.Parallel()
	var peer, lead2, legacy store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		lead2 = e.mkProfile(t, seatProfile("other-lead", "none"))
		legacy = e.mkProfile(t, store.AgentProfile{Name: "legacy"})
		return e.mkProfile(t, seatProfile("lead", "leaf"))
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	wantOK(t, callWS(t, ws, "run.start", launch(child.ID, peer, nil)), "leaf launching a peer")
	child2 := childOfSeat(t, e.identEnv)
	wantErr(t, callWS(t, ws, "run.start", launch(child2.ID, lead2, nil)), "not one of your child profiles")
	wantErr(t, callWS(t, ws, "run.start", launch(child2.ID, legacy, nil)), "not one of your child profiles")
}

func TestAdmission_ProfilesPolicyAllowsOnlyListedChildren(t *testing.T) {
	t.Parallel()
	var a, b, c store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		a, b, c = e.peer(t, "a"), e.peer(t, "b"), e.peer(t, "c")
		return e.mkProfile(t, seatProfile("lead", "profiles", a.ID, b.ID))
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	wantOK(t, callWS(t, ws, "run.start", launch(child.ID, a, nil)), "listed profile a")
	child2 := childOfSeat(t, e.identEnv)
	want := "profile " + itoa(c.ID) + " is not one of your child profiles (allowed: " + itoa(a.ID) + ", " + itoa(b.ID) + ")"
	wantErr(t, callWS(t, ws, "run.start", launch(child2.ID, c, nil)), want)
}

func TestAdmission_RunStartRequiresProfileId(t *testing.T) {
	t.Parallel()
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		return e.mkProfile(t, seatProfile("lead", "leaf"))
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	wantErr(t, callWS(t, ws, "run.start", map[string]any{"taskId": child.ID, "provider": "glm", "prompt": "x"}),
		"a seat caller must pass profileId of a profile it may launch; raw provider, permissionMode and model are not allowed")
}

func TestAdmission_RawProviderModeModelRejected(t *testing.T) {
	t.Parallel()
	var peer store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		return e.mkProfile(t, seatProfile("lead", "leaf"))
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	want := "a seat caller may launch profile " + itoa(peer.ID) + " only as configured"
	for name, extra := range map[string]map[string]any{
		"provider":       {"provider": "claude"},
		"permissionMode": {"permissionMode": "bypassPermissions"},
		"autoAccept":     {"autoAccept": true},
		"model":          {"model": "some-other-model"},
	} {
		resp := callWS(t, ws, "run.start", launch(child.ID, peer, extra))
		if resp.Error == nil || !strings.Contains(resp.Error.Message, want) {
			t.Fatalf("%s: response = %s / %+v, want %q", name, resp.Result, resp.Error, want)
		}
	}
	// An empty model is the profile's own.
	wantOK(t, callWS(t, ws, "run.start", launch(child.ID, peer, map[string]any{"model": ""})), "empty model")
}

func TestAdmission_OtherSubtreeRejected(t *testing.T) {
	t.Parallel()
	var peer store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		return e.mkProfile(t, seatProfile("lead", "leaf"))
	})
	ws := e.identified(t)
	// A sibling lead's tree: a root, a child, and a run on the child.
	sibling := e.sibling(t)
	foreign := e.taskUnder(t, sibling.ID, true)
	fr := startRun(t, e.admin, map[string]any{"taskId": foreign.ID, "provider": "glm", "prompt": "theirs"})

	notChild := "task " + itoa(foreign.ID) + " is not one of your child tasks"
	wantErr(t, callWS(t, ws, "run.start", launch(foreign.ID, peer, nil)), notChild)
	wantErr(t, callWS(t, ws, "run.stop", map[string]any{"runId": fr.RunID}), notChild)
	wantErr(t, callWS(t, ws, "task.archive", map[string]any{"id": foreign.ID}), notChild)
	wantErr(t, callWS(t, ws, "chat.create", map[string]any{"taskId": foreign.ID, "title": "c"}), notChild)
	wantErr(t, callWS(t, ws, "run.setPermissionMode", map[string]any{"runId": fr.RunID, "autoAccept": true}), notChild)
	wantErr(t, callWS(t, ws, "run.setConfigOption", map[string]any{"runId": fr.RunID, "configId": "x", "value": "y"}), notChild)
	// The foreign run is untouched.
	if st, err := e.api.Runs.State(fr.RunID); err != nil || st.Status != "running" {
		t.Fatalf("foreign run = %+v, %v; want still running", st, err)
	}

	// Its own child is fine, including a grandchild.
	own := childOfSeat(t, e.identEnv)
	wantOK(t, callWS(t, ws, "run.start", launch(own.ID, peer, nil)), "own child run.start")
	grand := e.taskUnder(t, own.ID, false)
	wantOK(t, callWS(t, ws, "chat.create", map[string]any{"taskId": grand.ID, "title": "c"}), "grandchild chat.create")
	wantOK(t, callWS(t, ws, "task.archive", map[string]any{"id": grand.ID}), "grandchild task.archive")
}

func TestAdmission_OwnTaskNotInItsSubtree(t *testing.T) {
	t.Parallel()
	var peer store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		return e.mkProfile(t, seatProfile("lead", "leaf"))
	})
	ws := e.identified(t)
	own := "task " + itoa(e.seatTask.ID) + " is not one of your child tasks"
	wantErr(t, callWS(t, ws, "run.start", launch(e.seatTask.ID, peer, nil)), own)
	wantErr(t, callWS(t, ws, "task.archive", map[string]any{"id": e.seatTask.ID}), own)
	wantErr(t, callWS(t, ws, "chat.create", map[string]any{"taskId": e.seatTask.ID, "title": "c"}), own)
	wantErr(t, callWS(t, ws, "run.stop", map[string]any{"runId": e.runID}), own)
}

func TestAdmission_ReadOnlyMethodsAllowedAnywhere(t *testing.T) {
	t.Parallel()
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		return e.mkProfile(t, seatProfile("lead", "none"))
	})
	ws := e.identified(t)
	sibling := e.sibling(t)
	foreign := e.taskUnder(t, sibling.ID, true)
	fr := startRun(t, e.admin, map[string]any{"taskId": foreign.ID, "provider": "glm", "prompt": "theirs"})
	for method, params := range map[string]any{
		"task.get":       map[string]any{"id": foreign.ID},
		"task.list":      map[string]any{"workspaceId": e.seatTask.WorkspaceID},
		"task.tree":      map[string]any{"taskId": foreign.ID},
		"task.diff":      map[string]any{"taskId": foreign.ID},
		"run.list":       map[string]any{"taskId": foreign.ID},
		"run.logs":       map[string]any{"runId": fr.RunID},
		"chat.list":      map[string]any{"taskId": foreign.ID},
		"chat.queueList": map[string]any{"chatId": 1},
		"workspace.list": map[string]any{},
		"provider.list":  map[string]any{},
		"usage.summary":  map[string]any{},
		"account.usage":  map[string]any{},
	} {
		if resp := callWS(t, ws, method, params); resp.Error != nil && strings.Contains(resp.Error.Message, "not one of your child") {
			t.Fatalf("%s refused by admission: %+v", method, resp.Error)
		}
	}
}

func profileIDs(t *testing.T, resp envelope) []int64 {
	t.Helper()
	wantOK(t, resp, "profile.list")
	var out []struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, p := range out {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestAdmission_ProfileListShowsOnlyAllowedChildren(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"none", "leaf", "profiles"} {
		t.Run(policy, func(t *testing.T) {
			t.Parallel()
			var peerA, peerB, lead store.AgentProfile
			e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
				peerA, peerB = e.peer(t, "a"), e.peer(t, "b")
				lead = e.mkProfile(t, seatProfile("orchestrator", "none"))
				e.mkProfile(t, store.AgentProfile{Name: "legacy"})
				if policy == "profiles" {
					return e.mkProfile(t, seatProfile("seat", policy, peerB.ID))
				}
				return e.mkProfile(t, seatProfile("seat", policy))
			})
			_ = lead
			got := profileIDs(t, callWS(t, e.identified(t), "profile.list", nil))
			want := map[string][]int64{"none": {}, "leaf": {peerA.ID, peerB.ID}, "profiles": {peerB.ID}}[policy]
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("policy %s: profile.list = %v, want %v", policy, got, want)
			}
			// The unidentified admin still sees everything.
			if all := profileIDs(t, callWS(t, e.admin, "profile.list", nil)); len(all) != 5 {
				t.Fatalf("admin sees %v, want 5 profiles", all)
			}
		})
	}
}

func TestAdmission_ProfileGetOfNonChildRefused(t *testing.T) {
	t.Parallel()
	var peerA, peerB store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peerA, peerB = e.peer(t, "a"), e.peer(t, "b")
		return e.mkProfile(t, seatProfile("seat", "profiles", peerA.ID))
	})
	ws := e.identified(t)
	wantOK(t, callWS(t, ws, "profile.get", map[string]any{"id": peerA.ID}), "profile.get child")
	wantErr(t, callWS(t, ws, "profile.get", map[string]any{"id": peerB.ID}), "profile "+itoa(peerB.ID)+" is not one of your child profiles")
	wantErr(t, callWS(t, ws, "profile.get", map[string]any{"id": 9999}), "profile 9999 is not one of your child profiles")
	wantOK(t, callWS(t, e.admin, "profile.get", map[string]any{"id": peerB.ID}), "unidentified profile.get")
}

func TestAdmission_UnmanagedProfileCallerUnchanged(t *testing.T) {
	t.Parallel()
	var legacy store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		legacy = e.mkProfile(t, store.AgentProfile{Name: "legacy"})
		return legacy
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	// Raw settings, no profileId, own task and a foreign one: all as S2b left it.
	wantOK(t, createChild(t, ws, e.identEnv, nil), "task.create")
	startRun(t, ws, map[string]any{"taskId": child.ID, "provider": "glm", "prompt": "raw", "source": "orchestrator"})
	foreign := newTestTask(t, e.wm, "hang")
	wantOK(t, callWS(t, ws, "chat.create", map[string]any{"taskId": foreign.ID, "title": "c"}), "foreign chat.create")
	if ids := profileIDs(t, callWS(t, ws, "profile.list", nil)); len(ids) != 1 {
		t.Fatalf("profile.list = %v, want all profiles", ids)
	}
	wantOK(t, callWS(t, ws, "profile.get", map[string]any{"id": legacy.ID}), "profile.get")
}

func TestAdmission_UnidentifiedConnUnchanged(t *testing.T) {
	t.Parallel()
	var peer store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		return e.mkProfile(t, seatProfile("seat", "none"))
	})
	foreign := newTestTask(t, e.wm, "hang")
	// No profileId, raw settings, any task.
	startRun(t, e.admin, map[string]any{"taskId": foreign.ID, "provider": "glm", "prompt": "raw"})
	wantOK(t, createChild(t, e.admin, e.identEnv, nil), "task.create")
	wantOK(t, callWS(t, e.admin, "chat.create", map[string]any{"taskId": foreign.ID, "title": "c"}), "chat.create")
	if ids := profileIDs(t, callWS(t, e.admin, "profile.list", nil)); len(ids) != 2 {
		t.Fatalf("profile.list = %v", ids)
	}
	wantOK(t, callWS(t, e.admin, "profile.get", map[string]any{"id": peer.ID}), "profile.get")
}

func TestAdmission_ErrorsDoNotLeakInstructions(t *testing.T) {
	t.Parallel()
	var peer, other store.AgentProfile
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		peer = e.peer(t, "peer")
		other = e.mkProfile(t, seatProfile("boss", "none"))
		return e.mkProfile(t, seatProfile("lead", "profiles", peer.ID))
	})
	ws := e.identified(t)
	child := childOfSeat(t, e.identEnv)
	foreign := newTestTask(t, e.wm, "hang")
	for name, resp := range map[string]envelope{
		"other profile": callWS(t, ws, "run.start", launch(child.ID, other, nil)),
		"as configured": callWS(t, ws, "run.start", launch(child.ID, peer, map[string]any{"provider": "claude"})),
		"profile.get":   callWS(t, ws, "profile.get", map[string]any{"id": other.ID}),
		"foreign task":  callWS(t, ws, "chat.create", map[string]any{"taskId": foreign.ID}),
		"no profile id": callWS(t, ws, "run.start", map[string]any{"taskId": child.ID, "provider": "glm"}),
	} {
		if resp.Error == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if strings.Contains(resp.Error.Message, "SECRET") {
			t.Fatalf("%s leaked instructions: %q", name, resp.Error.Message)
		}
	}
	// profile.list never carries the other profile's text either.
	if raw := callWS(t, ws, "profile.list", nil).Result; strings.Contains(string(raw), "SECRET-boss") {
		t.Fatalf("profile.list leaked a non-child profile: %s", raw)
	}
}
