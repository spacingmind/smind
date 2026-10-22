package wsapi

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/push"
)

// newTestRPCAPI builds an API with a real registry and notifier holding one
// registration per id in ids.
func newTestRPCAPI(t *testing.T, enabled bool, ids ...string) (*API, *tapSender, *push.Registry, func(string) envelope) {
	t.Helper()
	reg, err := push.OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if err := reg.Put(push.Registration{
			DeviceID: id, Provider: push.ProviderExpo, Token: "ExponentPushToken[tok" + id + "]", Platform: push.PlatformAndroid,
			Key: pushKeyBytes(32), Kinds: push.AllKinds, RegisteredAt: time.Now().Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	s := &tapSender{called: make(chan struct{}, 16)}
	n := push.NewNotifier(push.Deps{
		Registry: reg,
		Sender:   s,
		Task:     func(int64) (push.TaskInfo, bool) { return push.TaskInfo{}, false },
	})
	t.Cleanup(n.Close)
	api, srv := newRelayDeviceAPI(t, newFakeAdmin())
	api.SetPush(PushOptions{Enabled: enabled, Registry: reg, Notifier: n})
	callLocal := func(params string) envelope {
		ws := dialWS(t, srv, "tok")
		defer ws.Close()
		var p any
		if params != "" {
			p = json.RawMessage(params)
		}
		return callWS(t, ws, "push.test", p)
	}
	return api, s, reg, callLocal
}

func decodeTestResults(t *testing.T, env envelope) []push.TestResult {
	t.Helper()
	if env.Error != nil {
		t.Fatalf("error: %+v", env.Error)
	}
	var res struct {
		Results []push.TestResult `json:"results"`
	}
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatal(err)
	}
	if res.Results == nil {
		t.Fatalf("results is null: %s", env.Result)
	}
	return res.Results
}

func TestPushTestRPCSendsToAllRegisteredDevices(t *testing.T) {
	t.Parallel()
	_, s, _, call := newTestRPCAPI(t, true, "dev-aaaa", "dev-bbbb")
	res := decodeTestResults(t, call(""))
	if len(res) != 2 || !res[0].Sent || !res[1].Sent {
		t.Fatalf("results = %+v", res)
	}
	got := []string{res[0].DeviceID, res[1].DeviceID}
	sort.Strings(got)
	if got[0] != "dev-aaaa" || got[1] != "dev-bbbb" {
		t.Fatalf("devices = %v", got)
	}
	if s.count() != 2 {
		t.Fatalf("messages = %d", s.count())
	}
}

func TestPushTestRPCPrefixSelectsOneDevice(t *testing.T) {
	t.Parallel()
	_, s, _, call := newTestRPCAPI(t, true, "abcd1111", "wxyz2222")
	res := decodeTestResults(t, call(`{"deviceId":"WXYZ"}`))
	if len(res) != 1 || res[0].DeviceID != "wxyz2222" || !res[0].Sent {
		t.Fatalf("results = %+v", res)
	}
	if s.count() != 1 {
		t.Fatalf("messages = %d", s.count())
	}
}

func TestPushTestRPCPrefixErrors(t *testing.T) {
	t.Parallel()
	_, s, _, call := newTestRPCAPI(t, true, "abcd1111", "abcd2222")
	wantCode(t, call(`{"deviceId":"abc"}`), codeInvalid)
	wantCode(t, call(`{"deviceId":"zzzz"}`), codeNotFound)
	wantCode(t, call(`{"deviceId":"abcd"}`), codeAmbiguous)
	if s.count() != 0 {
		t.Fatalf("messages sent on errors: %d", s.count())
	}
	// An exact id wins over longer ids sharing its prefix.
	_, _, _, call2 := newTestRPCAPI(t, true, "abcd", "abcd2222")
	res := decodeTestResults(t, call2(`{"deviceId":"abcd"}`))
	if len(res) != 1 || res[0].DeviceID != "abcd" {
		t.Fatalf("exact match results = %+v", res)
	}
}

func TestPushTestRPCForbiddenOverTheRelay(t *testing.T) {
	t.Parallel()
	api, s, _, _ := newTestRPCAPI(t, true, "dev-aaaa")
	env := callDevice(t, api, "dev-aaaa", "push.test", nil)
	wantCode(t, env, codeForbidden)
	if s.count() != 0 {
		t.Fatal("sender called")
	}
}

func TestPushTestRPCDisabled(t *testing.T) {
	t.Parallel()
	_, s, _, call := newTestRPCAPI(t, false, "dev-aaaa")
	wantCode(t, call(""), codePushDisabled)
	if s.count() != 0 {
		t.Fatal("sender called")
	}
}

func TestPushTestRPCNoRegistrationsReturnsEmpty(t *testing.T) {
	t.Parallel()
	_, s, _, call := newTestRPCAPI(t, true)
	res := decodeTestResults(t, call(""))
	if len(res) != 0 || s.count() != 0 {
		t.Fatalf("results = %+v, sent = %d", res, s.count())
	}
}
