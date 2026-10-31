package wsapi

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/store"
)

var policyReadMethods = []string{"policy.get", "policy.test", "policy.audit"}

func TestPolicyRPCs_ReadOnlyAndRelayCanRead(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	if _, err := e.db.AppendPolicyAudit(store.PolicyAuditRow{Decision: "ask", Tool: "Bash", Target: "make", TaskID: e.seatTask.ID}); err != nil {
		t.Fatal(err)
	}
	relay := &requestContext{local: false}
	raw, err := budgetCall(t, e.api, relay, "policy.get", map[string]any{})
	if err != nil || !strings.Contains(string(raw), `"valid":true`) || !strings.Contains(string(raw), `"rules":[]`) {
		t.Fatalf("relay policy.get = %s, %v", raw, err)
	}
	raw, err = budgetCall(t, e.api, relay, "policy.test", map[string]any{"tool": "Bash", "command": "git stash"})
	if err != nil || !strings.Contains(string(raw), `"action":"deny"`) || !strings.Contains(string(raw), "no-stash") {
		t.Fatalf("relay policy.test = %s, %v", raw, err)
	}
	raw, err = budgetCall(t, e.api, relay, "policy.audit", map[string]any{"taskId": e.seatTask.ID, "decision": "ask"})
	if err != nil || !strings.Contains(string(raw), `"target":"make"`) {
		t.Fatalf("relay policy.audit = %s, %v", raw, err)
	}
	_, err = budgetCall(t, e.api, relay, "policy.audit", map[string]any{"decision": "maybe"})
	wantErrCode(t, err, codeInvalid)
	_, err = budgetCall(t, e.api, relay, "policy.test", map[string]any{"tool": "Bash", "command": strings.Repeat("a", 8<<10+1)})
	wantErrCode(t, err, codeInvalid)

	agent := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	for _, m := range policyReadMethods {
		_, err := budgetCall(t, e.api, agent, m, map[string]any{})
		wantErrCode(t, err, codeForbidden)
	}
	ws := e.identified(t)
	for _, m := range policyReadMethods {
		wantForbidden(t, callWS(t, ws, m, map[string]any{}), "not available to agents")
	}
}

func TestNoPolicyWriteRPCOrMCPTool(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	var got []string
	for name := range e.api.hs {
		if strings.HasPrefix(name, "policy.") {
			got = append(got, name)
		}
		if strings.Contains(name, "steward.policy") {
			t.Errorf("unexpected handler %q", name)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "policy.audit,policy.get,policy.test" {
		t.Fatalf("policy handlers = %v", got)
	}
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "policy.") || strings.Contains(m, "steward.policy") {
			t.Errorf("%s must not be run-scoped", m)
		}
	}
}

func TestAudit_AppendOnlyNoMutatingRPC(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	bad := regexp.MustCompile(`(?i)^(policy|audit)\..*(set|write|edit|update|delete|clear|purge)`)
	for name := range e.api.hs {
		if bad.MatchString(name) {
			t.Errorf("mutating policy/audit RPC %q", name)
		}
	}
}

func TestPolicyTest_ExplainsEachSegmentNoSideEffects(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	old := permpolicy.HostGOOS
	permpolicy.HostGOOS = "linux"
	t.Cleanup(func() { permpolicy.HostGOOS = old })
	engine := permpolicy.NewEngine(permpolicy.Options{
		Path: filepath.Join(t.TempDir(), "policy.yaml"), Enabled: func() bool { return true }, Audit: db,
		Home: t.TempDir(), SmindHome: t.TempDir(),
	})
	h := policyHandlers(engine, db)["policy.test"]
	raw, _ := json.Marshal(map[string]any{"tool": "Bash", "command": "go test ./... && git stash"})
	res, err := h(t.Context(), &requestContext{local: true}, raw)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(res)
	var got struct {
		Decision struct {
			Action   string `json:"action"`
			Segments []struct {
				Text    string `json:"text"`
				Action  string `json:"action"`
				FloorID string `json:"floorId"`
			} `json:"segments"`
		} `json:"decision"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision.Action != "deny" || len(got.Decision.Segments) != 2 {
		t.Fatalf("decision = %s", out)
	}
	if s := got.Decision.Segments[1]; s.Action != "deny" || s.FloorID != "no-stash" {
		t.Fatalf("segment 2 = %+v", s)
	}
	rows, err := db.ListPolicyAudit(store.PolicyAuditFilter{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("audit rows = %d, %v; want 0", len(rows), err)
	}
}

// Not parallel: SMIND_HOME must be set before the API (and so its engine) is
// built. policy.test is a dry run, so it explains the floor whether or not
// the engine's config switch is on.
func TestPolicyEngine_FloorProtectsDaemonHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	e := newIdentEnv(t, "hang")
	local := &requestContext{local: true}

	floor := func(dir string) string {
		raw, err := budgetCall(t, e.api, local, "policy.test", map[string]any{"tool": "Bash", "command": "cat " + filepath.Join(dir, "token")})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Decision struct {
				Action  string `json:"action"`
				FloorID string `json:"floorId"`
			} `json:"decision"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Decision.FloorID != "" && got.Decision.Action != "deny" {
			t.Fatalf("floor hit %q with action %q", got.Decision.FloorID, got.Decision.Action)
		}
		return got.Decision.FloorID
	}
	if id := floor(home); id != "no-install-state" {
		t.Fatalf("floor for the daemon home = %q, want no-install-state", id)
	}
	if id := floor(t.TempDir()); id == "no-install-state" {
		t.Fatalf("a path outside the daemon home hit %q", id)
	}
}

// Not parallel: HostGOOS is a global the harness engine reads. The gate is
// set after the harness is built, mirroring a daemon that starts on POSIX and
// keeps its engine off on Windows.
func TestPolicyRPC_TestRefusedOnWindows(t *testing.T) {
	e := newIdentEnv(t, "hang")
	old := permpolicy.HostGOOS
	permpolicy.HostGOOS = "windows"
	t.Cleanup(func() { permpolicy.HostGOOS = old })
	local := &requestContext{local: true}
	_, err := budgetCall(t, e.api, local, "policy.test", map[string]any{"command": "git status"})
	r := wantErrCode(t, err, codeInvalid)
	if !strings.Contains(r.Message, "not supported on Windows yet") {
		t.Fatalf("policy.test error = %q", r.Message)
	}
	raw, err := budgetCall(t, e.api, local, "policy.get", map[string]any{})
	if err != nil {
		t.Fatalf("policy.get error = %v", err)
	}
	var got struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatalf("policy.get enabled = true, want false when the engine cannot run")
	}
}
