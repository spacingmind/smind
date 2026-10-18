package wsapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/relay/devices"
)

// fakeDeviceAdmin is an in-memory RelayDeviceAdmin that records its calls.
type fakeDeviceAdmin struct {
	mu      sync.Mutex
	devs    []devices.Info
	window  *devices.Window
	calls   []string
	winArgs [2]int64 // duration, maxNew of the last OpenPairingWindow
}

func (f *fakeDeviceAdmin) record(c string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeDeviceAdmin) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeDeviceAdmin) Devices() []devices.Info {
	f.record("devices")
	return f.devs
}

func (f *fakeDeviceAdmin) ResolveDevice(prefix string) (string, error) {
	f.record("resolve:" + prefix)
	var match []string
	for _, d := range f.devs {
		if strings.HasPrefix(d.ID, strings.ToLower(prefix)) {
			match = append(match, d.ID)
		}
	}
	switch len(match) {
	case 0:
		return "", devices.ErrNotFound
	case 1:
		return match[0], nil
	}
	return "", devices.ErrAmbiguous
}

func (f *fakeDeviceAdmin) find(id string) *devices.Info {
	for i := range f.devs {
		if f.devs[i].ID == id {
			return &f.devs[i]
		}
	}
	return nil
}

func (f *fakeDeviceAdmin) ApproveDevice(id string) (devices.Device, error) {
	f.record("approve:" + id)
	d := f.find(id)
	d.State = devices.StateApproved
	return d.Device, nil
}

func (f *fakeDeviceAdmin) RevokeDevice(id string) (devices.Device, error) {
	f.record("revoke:" + id)
	d := f.find(id)
	d.State = devices.StateRevoked
	return d.Device, nil
}

func (f *fakeDeviceAdmin) RenameDevice(id, label string) (devices.Device, error) {
	f.record("rename:" + id + ":" + label)
	d := f.find(id)
	d.Label = label
	return d.Device, nil
}

func (f *fakeDeviceAdmin) OpenPairingWindow(d time.Duration, maxNew int) error {
	f.record("window")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.winArgs = [2]int64{int64(d), int64(maxNew)}
	if d <= 0 {
		d = 10 * time.Minute
	}
	if maxNew <= 0 {
		maxNew = 1
	}
	f.window = &devices.Window{ExpiresAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(d), Remaining: maxNew}
	return nil
}

func (f *fakeDeviceAdmin) PairingWindow() (devices.Window, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.window == nil {
		return devices.Window{}, false
	}
	return *f.window, true
}

func newFakeAdmin() *fakeDeviceAdmin {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, label string, st devices.State, conn bool) devices.Info {
		return devices.Info{Device: devices.Device{ID: id, Label: label, FirstSeen: t0, LastSeen: t0, State: st}, Connected: conn}
	}
	return &fakeDeviceAdmin{devs: []devices.Info{
		mk("aaaa1111", "phone", devices.StatePending, false),
		mk("aaaa2222", "tablet", devices.StateApproved, true),
		mk("bbbb3333", "laptop", devices.StateApproved, false),
	}}
}

func newRelayDeviceAPI(t *testing.T, admin RelayDeviceAdmin) (*API, *httptest.Server) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	api.SetRelayDevices(admin)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	return api, srv
}

func callWS(t *testing.T, ws *websocket.Conn, method string, params any) envelope {
	t.Helper()
	sendRequest(t, ws, method, method, params)
	return readEnvelopeFor(t, ws, method, 5*time.Second)
}

func TestRelayDeviceWSMethodsRequireToken(t *testing.T) {
	t.Parallel()
	admin := newFakeAdmin()
	api, srv := newRelayDeviceAPI(t, admin)

	for _, tok := range []string{"", "wrong"} {
		resp, err := http.Get(srv.URL + "/ws?token=" + tok)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q: status = %d, want 401", tok, resp.StatusCode)
		}
	}

	// Over the relay-served path every method is forbidden and the admin is
	// never touched.
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go api.ServeTransport(ctx, server)
	defer client.Close()

	methods := map[string]any{
		"relay.device.list":    nil,
		"relay.device.approve": map[string]any{"id": "aaaa1111"},
		"relay.device.revoke":  map[string]any{"id": "aaaa2222"},
		"relay.device.rename":  map[string]any{"id": "aaaa2222", "label": "x"},
		"relay.device.window":  map[string]any{"duration_seconds": 60},
	}
	for m, params := range methods {
		data, _ := json.Marshal(envelope{ID: m, Method: m, Params: marshalOrNull(params)})
		if err := client.Send(data); err != nil {
			t.Fatal(err)
		}
		raw, err := client.Receive()
		if err != nil {
			t.Fatal(err)
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if env.ID != m || env.Error == nil || env.Error.Code != "forbidden" {
			t.Fatalf("%s over relay = %+v, want forbidden", m, env)
		}
		if env.Error.Message != "relay.device.* is not available over the relay" {
			t.Fatalf("%s message = %q", m, env.Error.Message)
		}
	}
	if calls := admin.callList(); len(calls) != 0 {
		t.Fatalf("admin touched over the relay: %v", calls)
	}

	// The same methods work with the token on /ws.
	ws := dialWS(t, srv, "tok")
	if resp := callWS(t, ws, "relay.device.list", nil); resp.Error != nil {
		t.Fatalf("list on /ws: %v", resp.Error)
	}
}

func TestRelayDeviceListAndWindow(t *testing.T) {
	t.Parallel()
	admin := newFakeAdmin()
	_, srv := newRelayDeviceAPI(t, admin)
	ws := dialWS(t, srv, "tok")

	resp := callWS(t, ws, "relay.device.list", nil)
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	var list struct {
		Devices []map[string]any `json:"devices"`
		Window  json.RawMessage  `json:"window"`
	}
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Devices) != 3 || string(list.Window) != "null" {
		t.Fatalf("list = %s", resp.Result)
	}
	if list.Devices[1]["device_id"] != "aaaa2222" || list.Devices[1]["connected"] != true || list.Devices[1]["state"] != "approved" {
		t.Fatalf("device row = %v", list.Devices[1])
	}

	// No params: the admin gets 0,0 (the registry applies 10m / 1).
	resp = callWS(t, ws, "relay.device.window", nil)
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if admin.winArgs != [2]int64{0, 0} {
		t.Fatalf("admin window args = %v, want 0,0", admin.winArgs)
	}
	var w struct {
		ExpiresAt time.Time `json:"expires_at"`
		Remaining int       `json:"remaining"`
	}
	if err := json.Unmarshal(resp.Result, &w); err != nil {
		t.Fatal(err)
	}
	if w.Remaining != 1 || !w.ExpiresAt.Equal(time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)) {
		t.Fatalf("window = %s", resp.Result)
	}

	resp = callWS(t, ws, "relay.device.window", map[string]any{"duration_seconds": 90, "max_devices": 3})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if admin.winArgs != [2]int64{int64(90 * time.Second), 3} {
		t.Fatalf("admin window args = %v", admin.winArgs)
	}

	// list now reports the open window.
	resp = callWS(t, ws, "relay.device.list", nil)
	_ = json.Unmarshal(resp.Result, &list)
	if string(list.Window) == "null" || !strings.Contains(string(list.Window), `"remaining":3`) {
		t.Fatalf("list window = %s", list.Window)
	}

	if resp := callWS(t, ws, "relay.device.window", map[string]any{"duration_seconds": -1}); resp.Error == nil {
		t.Fatal("negative duration accepted")
	}
}

func TestRelayDeviceApproveRevokeRenameByPrefix(t *testing.T) {
	t.Parallel()
	admin := newFakeAdmin()
	_, srv := newRelayDeviceAPI(t, admin)
	ws := dialWS(t, srv, "tok")

	state := func(resp envelope) (string, string) {
		t.Helper()
		if resp.Error != nil {
			t.Fatal(resp.Error)
		}
		var d map[string]any
		if err := json.Unmarshal(resp.Result, &d); err != nil {
			t.Fatal(err)
		}
		return d["device_id"].(string), d["state"].(string)
	}

	if id, st := state(callWS(t, ws, "relay.device.approve", map[string]any{"id": "aaaa1"})); id != "aaaa1111" || st != "approved" {
		t.Fatalf("approve = %s %s", id, st)
	}
	if id, st := state(callWS(t, ws, "relay.device.revoke", map[string]any{"id": "BBBB"})); id != "bbbb3333" || st != "revoked" {
		t.Fatalf("revoke = %s %s", id, st)
	}
	resp := callWS(t, ws, "relay.device.rename", map[string]any{"id": "aaaa2", "label": "kitchen"})
	var d devices.Device
	if resp.Error != nil || json.Unmarshal(resp.Result, &d) != nil || d.ID != "aaaa2222" || d.Label != "kitchen" {
		t.Fatalf("rename = %+v / %v", resp.Result, resp.Error)
	}

	for _, tc := range []struct{ id, code string }{{"zzzz", "not_found"}, {"aaaa", "ambiguous"}} {
		for _, m := range []string{"relay.device.approve", "relay.device.revoke", "relay.device.rename"} {
			resp := callWS(t, ws, m, map[string]any{"id": tc.id, "label": "x"})
			if resp.Error == nil || resp.Error.Code != tc.code {
				t.Fatalf("%s %q = %+v, want code %s", m, tc.id, resp.Error, tc.code)
			}
		}
	}
	if resp := callWS(t, ws, "relay.device.approve", map[string]any{}); resp.Error == nil || !strings.Contains(resp.Error.Message, "id is required") {
		t.Fatalf("missing id = %+v", resp.Error)
	}
	// Failed lookups never reached an action.
	for _, c := range admin.callList() {
		if strings.HasPrefix(c, "approve:") || strings.HasPrefix(c, "revoke:") || strings.HasPrefix(c, "rename:") {
			if c != "approve:aaaa1111" && c != "revoke:bbbb3333" && c != "rename:aaaa2222:kitchen" {
				t.Fatalf("unexpected action %s", c)
			}
		}
	}
}
