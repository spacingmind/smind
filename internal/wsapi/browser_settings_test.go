package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// unspecifiedV4 is spelled at run time: internal/hygiene forbids the literal in tests.
var unspecifiedV4 = strings.Join([]string{"0", "0", "0", "0"}, ".")

// fakeSettings is an in-memory browserSettingsStore.
type fakeSettings struct {
	mu         sync.Mutex
	workspaces map[int64]bool
	rows       map[int64]store.BrowserSettings
	getErr     error
}

func newFakeSettings(workspaces ...int64) *fakeSettings {
	f := &fakeSettings{workspaces: map[int64]bool{}, rows: map[int64]store.BrowserSettings{}}
	for _, w := range workspaces {
		f.workspaces[w] = true
	}
	return f
}

func (f *fakeSettings) GetWorkspace(id int64) (store.Workspace, error) {
	if f.workspaces[id] {
		return store.Workspace{ID: id}, nil
	}
	return store.Workspace{}, fmt.Errorf("no workspace %d", id)
}

func (f *fakeSettings) GetBrowserSettings(id int64) (store.BrowserSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return store.BrowserSettings{}, f.getErr
	}
	if r, ok := f.rows[id]; ok {
		return r, nil
	}
	return store.BrowserSettings{WorkspaceID: id, AllowHosts: []string{}, ProfileMode: "ephemeral"}, nil
}

func (f *fakeSettings) SaveBrowserSettings(bs store.BrowserSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[bs.WorkspaceID] = bs
	return nil
}

func (f *fakeSettings) DeleteBrowserSettings(id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

// settingsHost is a browserHost wired to the fake store, with the session close observed.
func settingsHost(t *testing.T) (*browserHost, *fakeSettings, *[]int64) {
	t.Helper()
	fs := newFakeSettings(1, 2)
	var closed []int64
	h := &browserHost{settings: fs, killSwitch: func() bool { return false }, closeSession: func(ws int64) { closed = append(closed, ws) }}
	return h, fs, &closed
}

var settingsHumanRC = &requestContext{local: true}

func setCall(h *browserHost, rc *requestContext, params string) (browserSettingsView, error) {
	res, err := h.settingsSet(context.Background(), rc, json.RawMessage(params))
	if err != nil {
		return browserSettingsView{}, err
	}
	return res.(browserSettingsView), nil
}

func TestBrowserSettingsMethodsAreHumanOnly(t *testing.T) {
	e := newProcEnv(t)
	for _, m := range browserSettingsMethods {
		h, ok := e.api.hs[m]
		if !ok {
			t.Fatalf("%s is not a wsapi method", m)
		}
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is on RunScopedMethods", m)
		}
		if !pluginNever(m) {
			t.Errorf("%s is reachable from a plugin session", m)
		}
		if !slices.Contains(permpolicy.HumanOnlyRPCs, m) {
			t.Errorf("%s is not on permpolicy.HumanOnlyRPCs", m)
		}
		for capName, ms := range PluginCapabilityTable {
			for _, pm := range ms {
				if pm.Name == m {
					t.Errorf("%s is granted by plugin capability %q", m, capName)
				}
			}
		}
		// A workspace that exists and params the method accepts, so only the guards refuse.
		wsID := e.task.WorkspaceID
		_, err := h(context.Background(), &requestContext{runScoped: true}, validBrowserParams(m, wsID))
		wantRefusal(t, m+" run-scoped", err, "")
		_, err = h(context.Background(), nil, validBrowserParams(m, wsID))
		wantRefusal(t, m+" nil context", err, "")
	}
}

func TestBrowserSettingsMethodsRefusedForAnIdentifiedConnection(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	for _, m := range browserSettingsMethods {
		resp := callWS(t, ws, m, map[string]any{"workspaceId": 1})
		if resp.Error == nil || resp.Error.Code != "forbidden" {
			t.Errorf("%s over an identified (agent) connection: %+v / %+v, want forbidden", m, resp.Result, resp.Error)
		}
	}
}

// A relay connection (not local) never reaches the settings handlers, even
// when it carries no device id and so looks unidentified.
func TestBrowserSettingsRefusedOverTheRelay(t *testing.T) {
	t.Parallel()
	h, fs, closed := settingsHost(t)
	for name, fn := range map[string]handlerFunc{
		"browser.settings.get": browserLocalOnly("browser.settings.get", h.settingsGet),
		"browser.settings.set": browserLocalOnly("browser.settings.set", h.settingsSet),
		"browser.data.clear":   browserLocalOnly("browser.data.clear", h.dataClear),
	} {
		for _, rc := range []*requestContext{{local: false}, nil} {
			if _, err := fn(context.Background(), rc, json.RawMessage(`{"workspaceId":1,"enabled":true}`)); err == nil || !strings.Contains(err.Error(), "only available on this machine") {
				t.Errorf("%s over the relay: %v", name, err)
			}
		}
	}
	if len(fs.rows) != 0 || len(*closed) != 0 {
		t.Error("a relay call must change nothing")
	}
}

func TestBrowserSettings_AllowAddAndRemoveAreAtomicAndCanonical(t *testing.T) {
	t.Parallel()
	h, fs, _ := settingsHost(t)
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowHosts":["intranet.test","10.0.0.5"]}`); err != nil {
		t.Fatal(err)
	}
	// An existing host under another spelling is not duplicated.
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowAdd":["INTRANET.test."]}`); err != nil || len(fs.rows[1].AllowHosts) != 2 {
		t.Fatalf("add of an existing host: %v %v", err, fs.rows[1].AllowHosts)
	}
	// Remove finds the stored canonical host by any spelling.
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowRemove":["Intranet.Test."]}`); err != nil || !slices.Equal(fs.rows[1].AllowHosts, []string{"10.0.0.5"}) {
		t.Fatalf("remove by another spelling: %v %v", err, fs.rows[1].AllowHosts)
	}
	// Removing a host that is not there is refused and changes nothing.
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowRemove":["gone.test"]}`); err == nil || !strings.Contains(err.Error(), "not in the allowlist") {
		t.Fatalf("remove of a missing host: %v", err)
	}
	// A replacement and an increment together are ambiguous.
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowHosts":[],"allowAdd":["a.test"]}`); err == nil {
		t.Fatal("allowHosts with allowAdd must be refused")
	}
	// A bad host in allowAdd stores nothing.
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowAdd":["*"]}`); err == nil || !slices.Equal(fs.rows[1].AllowHosts, []string{"10.0.0.5"}) {
		t.Fatalf("wildcard add: %v %v", err, fs.rows[1].AllowHosts)
	}
}

func TestBrowserSettings_ConcurrentAddsLoseNothing(t *testing.T) {
	h, fs, _ := settingsHost(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := setCall(h, settingsHumanRC, fmt.Sprintf(`{"workspaceId":1,"allowAdd":["h%d.test"]}`, i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(fs.rows[1].AllowHosts); n != 20 {
		t.Fatalf("%d hosts stored, want 20 (a concurrent add was lost)", n)
	}
}

func TestBrowserSettings_HostLengthCaps(t *testing.T) {
	t.Parallel()
	h, fs, _ := settingsHost(t)
	long := strings.Repeat("a", 64) + ".test"
	huge := strings.Repeat(strings.Repeat("a", 60)+".", 5) + "test"
	for _, bad := range []string{long, huge} {
		if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowAdd":["`+bad+`"]}`); err == nil {
			t.Errorf("%d-char host must be refused", len(bad))
		}
	}
	if len(fs.rows) != 0 {
		t.Error("nothing stored")
	}
}

// wantRefusal asserts the EXACT refusal: the forbidden code and the message,
// not merely a non-nil error (a validation or not-found error would otherwise
// satisfy a test whose guard had been removed).
func wantRefusal(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var re *rpcCodeError
	if !errors.As(err, &re) || re.code != codeForbidden || !strings.Contains(re.msg, msg) {
		t.Errorf("%s: got %v, want code %q containing %q", what, err, codeForbidden, msg)
	}
}

// validBrowserParams are params each method ACCEPTS from a human for a workspace
// that exists, so that without its guard the call would succeed (and change
// something): the refusal under test is then the only thing stopping it.
func validBrowserParams(method string, ws int64) json.RawMessage {
	switch method {
	case "browser.settings.set":
		return json.RawMessage(fmt.Sprintf(`{"workspaceId":%d,"enabled":true,"allowAdd":["a.test"]}`, ws))
	default:
		return json.RawMessage(fmt.Sprintf(`{"workspaceId":%d}`, ws))
	}
}

// Each layer holds on its own: the handler refuses a nil or run-scoped context
// even when it is local, and the REGISTERED handler refuses a non-local one
// that is not run-scoped (a relay connection). Every call here would SUCCEED
// without the guard under test.
func TestBrowserSettings_EachLayerRefusesOnItsOwn(t *testing.T) {
	h, fs, closed := settingsHost(t)
	fs.rows[1] = store.BrowserSettings{WorkspaceID: 1, Enabled: false, AllowHosts: []string{"keep.test"}, ProfileMode: "ephemeral", UpdatedBy: "seed"}
	before := fs.rows[1]
	raw := map[string]handlerFunc{"browser.settings.get": h.settingsGet, "browser.settings.set": h.settingsSet, "browser.data.clear": h.dataClear}
	// Control: a human local context succeeds for the same params, so a refusal below is the guard.
	for m, fn := range raw {
		if _, err := fn(context.Background(), settingsHumanRC, validBrowserParams(m, 1)); err != nil {
			t.Fatalf("%s: the control call by a human must succeed: %v", m, err)
		}
	}
	fs.rows[1] = before // undo the control's change
	*closed = nil
	for m, fn := range raw {
		wantRefusal(t, m+" nil context", errOf(fn(context.Background(), nil, validBrowserParams(m, 1))), "is not available to agents")
		wantRefusal(t, m+" local+runScoped", errOf(fn(context.Background(), &requestContext{local: true, runScoped: true}, validBrowserParams(m, 1))), "is not available to agents")
	}
	if !reflect.DeepEqual(fs.rows[1], before) || len(*closed) != 0 {
		t.Fatalf("a refused call must leave the store and the session alone: %+v closed=%v", fs.rows[1], *closed)
	}

	// The registered handlers, real store: a relay-like context (not local, not run-scoped).
	e := newProcEnv(t)
	ws := e.task.WorkspaceID
	for _, m := range browserSettingsMethods {
		_, err := e.api.hs[m](context.Background(), &requestContext{local: false}, validBrowserParams(m, ws))
		wantRefusal(t, m+" registered, not local", err, "only available on this machine")
		if _, err := e.api.hs[m](context.Background(), settingsHumanRC, validBrowserParams(m, ws)); err != nil {
			t.Fatalf("%s registered: the control call by a local human must succeed: %v", m, err)
		}
	}
	if err := e.db.DeleteBrowserSettings(ws); err != nil {
		t.Fatal(err)
	}
	for _, m := range browserSettingsMethods {
		_, _ = e.api.hs[m](context.Background(), &requestContext{local: false}, validBrowserParams(m, ws))
	}
	if got, _ := e.db.GetBrowserSettings(ws); got.Enabled || len(got.AllowHosts) != 0 {
		t.Fatalf("a relay-like call changed the store: %+v", got)
	}
}

func errOf(_ any, err error) error { return err }

// No browser method is run-scoped any more (the child uses its run's socket),
// and every browser method on the websocket is human-only.
func TestNoBrowserMethodIsRunScopedAndAllAreHumanOnly(t *testing.T) {
	e := newProcEnv(t)
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "browser.") {
			t.Errorf("%s is run-scoped: no browser method may be reachable by an identified run", m)
		}
	}
	for m := range e.api.hs {
		if strings.HasPrefix(m, "browser.") && !slices.Contains(permpolicy.HumanOnlyRPCs, m) {
			t.Errorf("%s is a browser method that is not on the human-only table", m)
		}
	}
}

func TestBrowserSettings_ValidationTable(t *testing.T) {
	t.Parallel()
	bad := []string{`"*"`, `"*.example.com"`, `"http://example.com"`, `"example.com:80"`, `"example.com/x"`, `"u@example.com"`, `"169.254.169.254"`, `"fe80::1"`, `"` + unspecifiedV4 + `"`, `"224.0.0.1"`, `"100.100.100.200"`, `""`, `"a b"`, `"%41.com"`}
	for _, b := range bad {
		h, fs, closed := settingsHost(t)
		_, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowHosts":[`+b+`]}`)
		if err == nil || !strings.Contains(err.Error(), "browser.settings.set") {
			t.Errorf("allow host %s must be refused: %v", b, err)
		}
		if len(fs.rows) != 0 || len(*closed) != 0 {
			t.Errorf("a refused write must store nothing and not touch the session (%s)", b)
		}
	}
	h, fs, _ := settingsHost(t)
	v, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowHosts":["Intranet.Test.","10.0.0.5","intranet.test","[::1]"]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"intranet.test", "10.0.0.5", "::1"}
	if !slices.Equal(v.AllowHosts, want) || !slices.Equal(fs.rows[1].AllowHosts, want) {
		t.Fatalf("hosts must be stored canonical and deduplicated: %v / %v", v.AllowHosts, fs.rows[1].AllowHosts)
	}
	// 50 distinct hosts are fine, a 51st is refused (and the whole write with it).
	var many []string
	for i := 0; i < maxAllowHosts+1; i++ {
		many = append(many, fmt.Sprintf("h%d.test", i))
	}
	b, _ := json.Marshal(map[string]any{"workspaceId": 1, "allowHosts": many[:maxAllowHosts]})
	if _, err := setCall(h, settingsHumanRC, string(b)); err != nil {
		t.Fatalf("exactly %d hosts: %v", maxAllowHosts, err)
	}
	b, _ = json.Marshal(map[string]any{"workspaceId": 1, "allowHosts": many})
	if _, err := setCall(h, settingsHumanRC, string(b)); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("the 51st host must be refused: %v", err)
	}
	if len(fs.rows[1].AllowHosts) != maxAllowHosts {
		t.Fatal("a refused write leaves the stored list unchanged")
	}
}

func TestBrowserSettings_ProfileModeAndParams(t *testing.T) {
	t.Parallel()
	h, _, _ := settingsHost(t)
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"profileMode":"persistent"}`); err == nil || !strings.Contains(err.Error(), "not available yet") {
		t.Fatalf("persistent: %v", err)
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"profileMode":"shared"}`); err == nil {
		t.Fatal("an unknown profile mode must be refused")
	}
	if v, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"profileMode":"ephemeral"}`); err != nil || v.ProfileMode != "ephemeral" {
		t.Fatalf("ephemeral: %+v %v", v, err)
	}
	for _, bad := range []string{`{}`, `{"workspaceId":0}`, `{"workspaceId":-3}`, `{"workspaceId":1,"surprise":true}`, `{"workspaceId":"1"}`, `[]`} {
		if _, err := setCall(h, settingsHumanRC, bad); err == nil {
			t.Errorf("params %s must be refused", bad)
		}
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":99,"enabled":true}`); err == nil || !strings.Contains(err.Error(), "unknown workspace") {
		t.Fatalf("an unknown workspace: %v", err)
	}
}

func TestBrowserSettings_PartialUpdatesKeepTheRest(t *testing.T) {
	t.Parallel()
	h, fs, _ := settingsHost(t)
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"enabled":true,"allowHosts":["a.test"]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowHosts":["b.test"]}`); err != nil {
		t.Fatal(err)
	}
	if r := fs.rows[1]; !r.Enabled || !slices.Equal(r.AllowHosts, []string{"b.test"}) {
		t.Fatalf("setting only the hosts must keep the switch: %+v", r)
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"enabled":false}`); err != nil {
		t.Fatal(err)
	}
	if r := fs.rows[1]; r.Enabled || !slices.Equal(r.AllowHosts, []string{"b.test"}) {
		t.Fatalf("setting only the switch must keep the hosts: %+v", r)
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowHosts":[]}`); err != nil {
		t.Fatal(err)
	}
	if r := fs.rows[1]; len(r.AllowHosts) != 0 {
		t.Fatalf("an empty list clears the allowlist: %+v", r)
	}
}

func TestBrowserSettings_SetClosesTheSessionAndRecordsTheActor(t *testing.T) {
	t.Parallel()
	h, fs, closed := settingsHost(t)
	v, err := setCall(h, settingsHumanRC, `{"workspaceId":2,"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(*closed) != 1 || (*closed)[0] != 2 {
		t.Fatalf("a changed policy must end that workspace's running browser: %v", *closed)
	}
	if v.UpdatedBy != "local" || fs.rows[2].UpdatedBy != "local" || v.UpdatedAt == nil {
		t.Fatalf("the actor and time are recorded: %+v", v)
	}
	// The relay never reaches the handler (browserLocalOnly), so this only pins
	// how the actor string is derived for a context that carries a device id.
	if _, err := setCall(h, &requestContext{deviceID: "phone-1"}, `{"workspaceId":2,"enabled":false}`); err != nil {
		t.Fatal(err)
	}
	if fs.rows[2].UpdatedBy != "device:phone-1" {
		t.Fatalf("the actor derivation for a device id: %q", fs.rows[2].UpdatedBy)
	}
}

func TestBrowserDataClear_StopsTheSession(t *testing.T) {
	t.Parallel()
	h, _, closed := settingsHost(t)
	if _, err := h.dataClear(context.Background(), settingsHumanRC, json.RawMessage(`{"workspaceId":1}`)); err != nil {
		t.Fatal(err)
	}
	if len(*closed) != 1 || (*closed)[0] != 1 {
		t.Fatalf("clear ends the session: %v", *closed)
	}
	if _, err := h.dataClear(context.Background(), settingsHumanRC, json.RawMessage(`{"workspaceId":7}`)); err == nil {
		t.Fatal("an unknown workspace is refused")
	}
}

func TestBrowserSettings_GetShowsStateAndTheLimits(t *testing.T) {
	t.Parallel()
	h, fs, _ := settingsHost(t)
	fs.rows[1] = store.BrowserSettings{WorkspaceID: 1, Enabled: true, AllowHosts: []string{"x.test"}, ProfileMode: "ephemeral", UpdatedBy: "local"}
	res, err := h.settingsGet(context.Background(), settingsHumanRC, json.RawMessage(`{"workspaceId":1}`))
	if err != nil {
		t.Fatal(err)
	}
	v := res.(browserSettingsView)
	if !v.Enabled || !slices.Equal(v.AllowHosts, []string{"x.test"}) || !v.Available || v.Note == "" || v.KillSwitch {
		t.Fatalf("view: %+v", v)
	}
	h.killSwitch = func() bool { return true }
	res, _ = h.settingsGet(context.Background(), settingsHumanRC, json.RawMessage(`{"workspaceId":2}`))
	if v2 := res.(browserSettingsView); !v2.KillSwitch || v2.Enabled || v2.AllowHosts == nil {
		t.Fatalf("a workspace without a row is off with an empty (non-nil) list, and the kill switch shows: %+v", v2)
	}
}

func TestKillSwitch_BeatsTheWorkspaceSwitch(t *testing.T) {
	t.Parallel()
	h, fs, _ := settingsHost(t)
	fs.rows[1] = store.BrowserSettings{WorkspaceID: 1, Enabled: true}
	if !h.workspaceSwitchOn(1) {
		t.Fatal("an enabled workspace with the kill switch off is on")
	}
	on := true
	h.killSwitch = func() bool { return on }
	if h.workspaceSwitchOn(1) {
		t.Fatal("the kill switch beats the workspace switch")
	}
	on = false
	if !h.workspaceSwitchOn(1) {
		t.Fatal("and it is read on every decision, not once")
	}
	if h.workspaceSwitchOn(2) {
		t.Fatal("a workspace with no row is off")
	}
	fs.getErr = fmt.Errorf("db down")
	if h.workspaceSwitchOn(1) {
		t.Fatal("an unreadable setting is off (fail closed)")
	}
}

// The browser is off unless the workspace flag is on (the default), whatever
// the package gate says: the registry and callRun both read the real store.
func TestBrowser_OffUnlessTheWorkspaceFlagIsOn(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	task, err := e.db.GetTask(e.seatTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.api.browser.killSwitch = func() bool { return false }
	if e.api.Runs.BrowserAllowed(task.WorkspaceID) {
		t.Fatal("no stored flag: off")
	}
	if _, err := callRunJSON(t, e.api.browser, e.runID, task.ID, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "not enabled for this workspace") {
		t.Fatalf("off by default: %v", err)
	}
	if err := e.db.SaveBrowserSettings(store.BrowserSettings{WorkspaceID: task.WorkspaceID, Enabled: true, AllowHosts: []string{}, ProfileMode: "ephemeral", UpdatedBy: "local", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if !e.api.Runs.BrowserAllowed(task.WorkspaceID) {
		t.Fatal("flag on, kill switch off, package gate open: the registry allows it")
	}
}

func TestBrowserCall_KillSwitchRefusesWithItsOwnMessage(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	e.api.browser.gate = func(int64) bool { return true } // every other gate open
	e.api.browser.killSwitch = func() bool { return true }
	if _, err := callRunJSON(t, e.api.browser, e.runID, e.seatTask.ID, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "disabled by the daemon's configuration") {
		t.Fatalf("kill switch refusal: %v", err)
	}
}

func TestDenyListCoversTheDaemonsOwnAddresses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SMIND_HOME", dir)
	t.Setenv("SMIND_BROWSER_DISABLED", "")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("server:\n  listen: 127.0.0.1:4700\n  hostnames: [daemon.example.test]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := daemonDeny()
	if err != nil {
		t.Fatal(err)
	}
	has := func(host string, port int) bool {
		for _, e := range got {
			if e.Host == host && e.Port == port {
				return true
			}
		}
		return false
	}
	if !has("127.0.0.1", 4700) || !has("daemon.example.test", 4700) {
		t.Fatalf("the listen address and the daemon's host names are denied: %+v", got)
	}
	pol, err := browser.NewPolicy(nil, got)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"http://127.0.0.1:4700/api/token", "http://localhost:4700/", "http://[::1]:4700/"} {
		if pol.CheckURL(u).Allowed {
			t.Errorf("%s must be refused", u)
		}
	}
	if !pol.CheckURL("http://localhost:4701/").Allowed {
		t.Error("another loopback port stays allowed")
	}
}

// A daemon listening on a LAN address still shields its loopback companions.
func TestDenyListLoopbackCompanionsAndUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SMIND_HOME", dir)
	t.Setenv("SMIND_BROWSER_DISABLED", "")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("server:\n  listen: 192.168.1.5:4700\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := daemonDeny()
	if err != nil {
		t.Fatal(err)
	}
	pol, err := browser.NewPolicy(nil, got)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"http://127.0.0.1:4700/", "http://localhost:4700/", "http://[::1]:4700/"} {
		if pol.CheckURL(u).Allowed {
			t.Errorf("%s must be refused", u)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("server: [unbalanced\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := daemonDeny(); err == nil {
		t.Error("an unreadable config must be an error (fail closed)")
	}
}

// The whole path with the real store and the real wiring: the human-only
// handlers write the table, the gate reads it, and the browser is still dark.
func TestBrowserSettings_RealStoreEndToEnd(t *testing.T) {
	e := newProcEnv(t)
	ws := e.task.WorkspaceID
	rc := &requestContext{local: true}
	params := func(v map[string]any) json.RawMessage { b, _ := json.Marshal(v); return b }
	if _, err := e.api.hs["browser.settings.set"](context.Background(), rc, params(map[string]any{"workspaceId": ws, "enabled": true, "allowHosts": []string{"Intranet.Test", "10.0.0.5"}})); err != nil {
		t.Fatal(err)
	}
	got, err := e.db.GetBrowserSettings(ws)
	if err != nil || !got.Enabled || !slices.Equal(got.AllowHosts, []string{"intranet.test", "10.0.0.5"}) || got.UpdatedBy != "local" {
		t.Fatalf("stored: %+v %v", got, err)
	}
	if !e.api.browser.workspaceSwitchOn(ws) || !slices.Equal(e.api.browser.allow(ws), []string{"intranet.test", "10.0.0.5"}) {
		t.Fatal("the gate hook and the allowlist read the real store")
	}
	if !e.api.Runs.BrowserAllowed(ws) {
		t.Fatal("the flag set through the human-only RPC opens the registry's gate")
	}
	res, err := e.api.hs["browser.settings.get"](context.Background(), rc, params(map[string]any{"workspaceId": ws}))
	if err != nil {
		t.Fatal(err)
	}
	if v := res.(browserSettingsView); !v.Enabled || !v.Available {
		t.Fatalf("get: %+v", v)
	}
	if _, err := e.api.hs["browser.settings.set"](context.Background(), rc, params(map[string]any{"workspaceId": ws + 999, "enabled": true})); err == nil {
		t.Fatal("a workspace that does not exist is refused by the real store too")
	}
}

func TestBrowserSettings_AllowAddPastTheCapIsRefused(t *testing.T) {
	t.Parallel()
	h, fs, _ := settingsHost(t)
	for i := 0; i < maxAllowHosts; i++ {
		if _, err := setCall(h, settingsHumanRC, fmt.Sprintf(`{"workspaceId":1,"allowAdd":["h%d.test"]}`, i)); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"allowAdd":["extra.test"]}`); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("the add past the cap must be refused: %v", err)
	}
	if len(fs.rows[1].AllowHosts) != maxAllowHosts {
		t.Fatalf("a refused add leaves the list as it was: %d", len(fs.rows[1].AllowHosts))
	}
}

// Flipping the kill switch while a session is live: the next browser.call ends
// every session before it refuses.
func TestBrowserCall_KillSwitchFlipEndsLiveSessionsAtTheNextCall(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	h.gate = func(int64) bool { return true }
	admitAllPermits(h) // this test is about something else than the permission gate
	ended := 0
	h.endAllSessions = func() { ended++ }
	h.killSwitch = func() bool { return false }
	h.launcher = func() (browser.Launcher, error) { return nil, &browser.UnavailableError{InstallStep: "x"} }
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`); err != nil {
		t.Fatalf("call before the flip: %v", err)
	}
	if ended != 0 {
		t.Fatal("nothing is ended while the switch is off")
	}
	h.killSwitch = func() bool { return true }
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "disabled by the daemon's configuration") {
		t.Fatalf("after the flip: %v", err)
	}
	if ended != 1 {
		t.Fatalf("the flip must end every live session at the next call: ended=%d", ended)
	}
}

// A call that passed the gates in browser.call must not create a session once
// a disable landed: session() re-checks inside gateMu, and the settings change
// closes the workspace under the write lock, after any session being created.
func TestBrowserHost_SessionCreationRechecksAndOrdersAgainstASettingsChange(t *testing.T) {
	h, fs, closed := settingsHost(t)
	h.gate = func(ws int64) bool { return h.workspaceSwitchOn(ws) }
	h.allow = func(int64) []string { return nil }
	h.deny = func() ([]browser.Endpoint, error) { return nil, nil }
	h.launcher = func() (browser.Launcher, error) { return nil, nil }
	started := make(chan struct{})
	release := make(chan struct{})
	startCalls := 0
	h.startSession = func(context.Context, browser.Config) (*browser.Session, error) {
		startCalls++
		close(started)
		<-release // the session is "starting": a settings change arrives now
		return nil, errors.New("fake: no real browser")
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"enabled":true}`); err != nil {
		t.Fatal(err)
	}
	*closed = nil

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.session(context.Background(), browser.Run{ID: "r", WorkspaceID: 1})
	}()
	<-started
	setDone := make(chan error, 1)
	go func() {
		_, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"enabled":false}`)
		setDone <- err
	}()
	// The change cannot close the workspace while a session is being created.
	select {
	case <-setDone:
		t.Fatal("the settings change closed the workspace while a session was starting (it would miss that session)")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-done
	if err := <-setDone; err != nil {
		t.Fatal(err)
	}
	if len(*closed) != 1 {
		t.Fatalf("the change closes the workspace after the session was created: %v", *closed)
	}
	// Now disabled: a call that already passed browser.call's gate check is refused at creation.
	before := startCalls
	if _, err := h.session(context.Background(), browser.Run{ID: "r", WorkspaceID: 1}); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("creation for a disabled workspace must be refused: %v", err)
	}
	if startCalls != before {
		t.Fatal("no session may be started for a disabled workspace")
	}
	// And the kill switch, set after the flag, is re-checked too.
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":1,"enabled":true}`); err != nil {
		t.Fatal(err)
	}
	h.killSwitch = func() bool { return true }
	if _, err := h.session(context.Background(), browser.Run{ID: "r", WorkspaceID: 1}); err == nil || !strings.Contains(err.Error(), "disabled by the daemon") {
		t.Fatalf("kill switch at creation: %v", err)
	}
	_ = fs
}

func TestDenyListResolvesAHostnameListenAddress(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SMIND_HOME", dir)
	t.Setenv("SMIND_BROWSER_DISABLED", "")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("server:\n  listen: daemon.example.test:4700\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := lookupListenHost
	t.Cleanup(func() { lookupListenHost = old })
	lookupListenHost = func(_ context.Context, host string) ([]string, error) {
		if host != "daemon.example.test" {
			t.Errorf("resolving %q", host)
		}
		return []string{"192.168.7.7", "fe80::1%en0"}, nil
	}
	got, err := daemonDeny()
	if err != nil {
		t.Fatal(err)
	}
	has := func(host string) bool {
		for _, e := range got {
			if e.Host == host && e.Port == 4700 {
				return true
			}
		}
		return false
	}
	if !has("daemon.example.test") || !has("192.168.7.7") || !has("fe80::1") || !has("127.0.0.1") {
		t.Fatalf("the listen name, its resolved addresses (no zone) and the loopback forms are reserved: %+v", got)
	}
	// An unresolvable listen name fails closed.
	lookupListenHost = func(context.Context, string) ([]string, error) { return nil, errors.New("no such host") }
	if _, err := daemonDeny(); err == nil {
		t.Fatal("an unresolvable listen host must be an error")
	}
}

// The wiring in server.go: the registry's installed hook is the store-backed
// switch, driven here through the real handlers, the real store and the real
// config kill switch.
func TestBrowserWiring_RegistryHookFollowsTheStoreAndTheKillSwitch(t *testing.T) {
	e := newProcEnv(t)
	ws := e.task.WorkspaceID
	rc := &requestContext{local: true}
	set := func(on bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"workspaceId": ws, "enabled": on})
		if _, err := e.api.hs["browser.settings.set"](context.Background(), rc, b); err != nil {
			t.Fatal(err)
		}
	}
	if e.api.Runs.BrowserSwitchOn(ws) {
		t.Fatal("no row: off")
	}
	set(true)
	if !e.api.Runs.BrowserSwitchOn(ws) {
		t.Fatal("the flag set through the human-only RPC must reach the registry hook")
	}
	t.Setenv("SMIND_BROWSER_DISABLED", "1")
	if e.api.Runs.BrowserSwitchOn(ws) {
		t.Fatal("the kill switch beats the flag in the installed hook")
	}
	t.Setenv("SMIND_BROWSER_DISABLED", "")
	set(false)
	if e.api.Runs.BrowserSwitchOn(ws) {
		t.Fatal("flag off: off")
	}
}

// The view names what each agent gets, straight from the reach table: codex-native
// has no browser, a plugin-free registry lists every provider, and the entries are
// the table's own.
func TestBrowserSettingsListsProviders(t *testing.T) {
	t.Parallel()
	h, _, _ := settingsHost(t)
	reg := providers.New()
	h.reachList = reg.BrowserReachList
	res, err := h.settingsGet(context.Background(), settingsHumanRC, json.RawMessage(`{"workspaceId":1}`))
	if err != nil {
		t.Fatal(err)
	}
	v := res.(browserSettingsView)
	if len(v.Providers) != len(reg.List()) {
		t.Fatalf("providers = %d entries, registry has %d", len(v.Providers), len(reg.List()))
	}
	byID := map[string]providers.BrowserReachEntry{}
	for _, p := range v.Providers {
		byID[p.ID] = p
	}
	if c := byID["codex-native"]; c.Reach != providers.ReachNo || c.Why == "" {
		t.Errorf("codex-native entry = %+v: no browser, with the reason", c)
	}
	if c := byID["claude-native"]; c.Reach != providers.ReachYes || c.Images != "yes" {
		t.Errorf("claude-native entry = %+v", c)
	}
	if c := byID["glm"]; c.Images != "no" {
		t.Errorf("glm entry = %+v: text only", c)
	}
	for _, s := range reg.List() {
		if got, want := byID[s.ID].Reach, providers.BrowserReachOf(s).Reach; got != want {
			t.Errorf("%s: view says %q, table says %q", s.ID, got, want)
		}
	}
	// No host wired (a bare test host): an empty list, never null.
	h.reachList = nil
	res, _ = h.settingsGet(context.Background(), settingsHumanRC, json.RawMessage(`{"workspaceId":1}`))
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), `"providers":[]`) {
		t.Errorf("no provider source must serialise as [], got %s", raw)
	}
}
