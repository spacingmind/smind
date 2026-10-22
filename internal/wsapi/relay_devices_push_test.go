package wsapi

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/push"
)

func listDevicesJSON(t *testing.T, env envelope) []map[string]any {
	t.Helper()
	if env.Error != nil {
		t.Fatalf("error: %+v", env.Error)
	}
	var res struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatal(err)
	}
	return res.Devices
}

func TestRelayDeviceListShowsPushRegistration(t *testing.T) {
	t.Parallel()
	admin := newFakeAdmin()
	reg, err := push.OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Put(push.Registration{
		DeviceID: "aaaa2222", Provider: push.ProviderExpo, Token: testPushToken, Platform: push.PlatformAndroid,
		Key: pushKeyBytes(32), Kinds: push.AllKinds, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	api, srv := newRelayDeviceAPI(t, admin)
	api.SetPush(PushOptions{Enabled: true, Registry: reg})
	ws := dialWS(t, srv, "tok")
	defer ws.Close()
	devs := listDevicesJSON(t, callWS(t, ws, "relay.device.list", nil))
	if len(devs) != 3 {
		t.Fatalf("devices = %v", devs)
	}
	for _, d := range devs {
		want := d["device_id"] == "aaaa2222"
		if got, ok := d["push"].(bool); !ok || got != want {
			t.Errorf("device %v push = %v, want %v", d["device_id"], d["push"], want)
		}
	}
}

func TestRelayDeviceListWithoutPushHasNoPushField(t *testing.T) {
	t.Parallel()
	for name, wire := range map[string]func(*API){
		"not called": func(*API) {},
		"disabled":   func(a *API) { a.SetPush(PushOptions{Enabled: false}) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api, srv := newRelayDeviceAPI(t, newFakeAdmin())
			wire(api)
			ws := dialWS(t, srv, "tok")
			defer ws.Close()
			for _, d := range listDevicesJSON(t, callWS(t, ws, "relay.device.list", nil)) {
				if _, has := d["push"]; has {
					t.Fatalf("device %v has a push key", d["device_id"])
				}
			}
		})
	}
}

func TestRelayDeviceListDoesNotMutateAdminData(t *testing.T) {
	t.Parallel()
	admin := newFakeAdmin()
	reg, err := push.OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	api, srv := newRelayDeviceAPI(t, admin)
	api.SetPush(PushOptions{Enabled: true, Registry: reg})
	ws := dialWS(t, srv, "tok")
	defer ws.Close()
	for i := 0; i < 2; i++ {
		callWS(t, ws, "relay.device.list", nil)
	}
	for _, d := range admin.devs {
		if d.Push != nil {
			t.Fatalf("admin device %s was mutated", d.ID)
		}
	}
}
