package wsapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/push"
)

const testPushToken = "ExponentPushToken[abcDEF123]"

func newPushAPI(t *testing.T, o PushOptions) (*API, *push.Registry) {
	t.Helper()
	reg, err := push.OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	api, _ := newRelayDeviceAPI(t, newFakeAdmin())
	o.Registry = reg
	api.SetPush(o)
	return api, reg
}

func pushKeyBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}

func pushParams(over map[string]any) map[string]any {
	p := map[string]any{
		"v":        1,
		"provider": "expo",
		"token":    testPushToken,
		"platform": "android",
		"key":      base64.StdEncoding.EncodeToString(pushKeyBytes(32)),
	}
	for k, v := range over {
		if v == nil {
			delete(p, k)
		} else {
			p[k] = v
		}
	}
	return p
}

// callDevice sends one request over ServeDevice(deviceID) and returns the
// response envelope.
func callDevice(t *testing.T, api *API, deviceID, method string, params json.RawMessage) envelope {
	t.Helper()
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if deviceID == "" {
		go api.ServeTransport(ctx, server)
	} else {
		go api.ServeDevice(ctx, server, deviceID)
	}
	defer client.Close()
	data, err := json.Marshal(envelope{ID: "p", Method: method, Params: params})
	if err != nil {
		t.Fatal(err)
	}
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
	return env
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func wantCode(t *testing.T, env envelope, code string) {
	t.Helper()
	if env.Error == nil || env.Error.Code != code {
		t.Fatalf("response = %+v (error %+v), want code %q", env, env.Error, code)
	}
}

func registerOK(t *testing.T, api *API, dev string, over map[string]any) envelope {
	t.Helper()
	env := callDevice(t, api, dev, "push.register", mustJSON(t, pushParams(over)))
	if env.Error != nil {
		t.Fatalf("push.register as %s: %+v", dev, env.Error)
	}
	return env
}

func TestPushRegisterStoresUnderSessionDeviceID(t *testing.T) {
	t.Parallel()
	api, reg := newPushAPI(t, PushOptions{Enabled: true})
	env := registerOK(t, api, "dev-abc", map[string]any{"deviceId": "evil", "platform": "ios"})

	var res pushRegisterResult
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Registered || res.V != 1 || !reflect.DeepEqual(res.Kinds, pushKinds) {
		t.Fatalf("result = %+v", res)
	}
	got, ok := reg.Get("dev-abc")
	if !ok {
		t.Fatal("dev-abc not registered")
	}
	if got.Token != testPushToken || got.Platform != "ios" || got.Provider != "expo" ||
		!bytes.Equal(got.Key, pushKeyBytes(32)) || !reflect.DeepEqual(got.Kinds, pushKinds) {
		t.Fatalf("stored = %+v", got)
	}
	if _, ok := reg.Get("evil"); ok {
		t.Fatal("registration stored under params deviceId")
	}
}

func TestPushRegisterForbiddenOnLocalWS(t *testing.T) {
	t.Parallel()
	reg, err := push.OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	api, srv := newRelayDeviceAPI(t, newFakeAdmin())
	api.SetPush(PushOptions{Enabled: true, Registry: reg})
	ws := dialWS(t, srv, "tok")
	env := callWS(t, ws, "push.register", pushParams(nil))
	wantCode(t, env, codeForbidden)
	if _, ok := reg.Get(""); ok {
		t.Fatal("registry not empty")
	}
}

func TestPushRegisterForbiddenForLegacyRelayClient(t *testing.T) {
	t.Parallel()
	api, _ := newPushAPI(t, PushOptions{Enabled: true})
	env := callDevice(t, api, "", "push.register", mustJSON(t, pushParams(nil)))
	wantCode(t, env, codeForbidden)
}

func TestPushRegisterForbiddenWhenNotApproved(t *testing.T) {
	t.Parallel()
	api, reg := newPushAPI(t, PushOptions{Enabled: true, Approved: func(string) bool { return false }})
	env := callDevice(t, api, "dev-abc", "push.register", mustJSON(t, pushParams(nil)))
	wantCode(t, env, codeForbidden)
	if _, ok := reg.Get("dev-abc"); ok {
		t.Fatal("stored despite not approved")
	}
}

func TestPushRegisterValidation(t *testing.T) {
	t.Parallel()
	key := func(n int) string { return base64.StdEncoding.EncodeToString(pushKeyBytes(n)) }
	cases := []struct {
		name   string
		params json.RawMessage
		code   string
	}{
		{"bad json", json.RawMessage(`"not an object"`), codeInvalid},
		{"v2", mustJSON(t, pushParams(map[string]any{"v": 2})), codeInvalid},
		{"provider fcm", mustJSON(t, pushParams(map[string]any{"provider": "fcm"})), codeUnsupportedProvider},
		{"platform windows", mustJSON(t, pushParams(map[string]any{"platform": "windows"})), codeInvalid},
		{"token abc", mustJSON(t, pushParams(map[string]any{"token": "abc"})), codeInvalidToken},
		{"token empty", mustJSON(t, pushParams(map[string]any{"token": "ExponentPushToken[]"})), codeInvalidToken},
		{"token space", mustJSON(t, pushParams(map[string]any{"token": "ExponentPushToken[a b]"})), codeInvalidToken},
		{"key not base64", mustJSON(t, pushParams(map[string]any{"key": "!!!notbase64"})), codeInvalidKey},
		{"key 31", mustJSON(t, pushParams(map[string]any{"key": key(31)})), codeInvalidKey},
		{"key 33", mustJSON(t, pushParams(map[string]any{"key": key(33)})), codeInvalidKey},
		{"kinds bogus", mustJSON(t, pushParams(map[string]any{"kinds": []string{"bogus"}})), codeInvalidKinds},
		{"kinds test", mustJSON(t, pushParams(map[string]any{"kinds": []string{"test"}})), codeInvalidKinds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api, reg := newPushAPI(t, PushOptions{Enabled: true})
			env := callDevice(t, api, "dev-abc", "push.register", tc.params)
			if tc.code == "" {
				if env.Error == nil {
					t.Fatal("want an error for bad json")
				}
			} else {
				wantCode(t, env, tc.code)
			}
			if _, ok := reg.Get("dev-abc"); ok {
				t.Fatal("registry not empty after rejected register")
			}
		})
	}

	t.Run("kinds omitted", func(t *testing.T) {
		t.Parallel()
		api, reg := newPushAPI(t, PushOptions{Enabled: true})
		registerOK(t, api, "dev-abc", nil)
		got, _ := reg.Get("dev-abc")
		if !reflect.DeepEqual(got.Kinds, pushKinds) {
			t.Fatalf("kinds = %v", got.Kinds)
		}
	})
	t.Run("kinds dedup order", func(t *testing.T) {
		t.Parallel()
		api, reg := newPushAPI(t, PushOptions{Enabled: true})
		registerOK(t, api, "dev-abc", map[string]any{"kinds": []string{"done", "done", "permission"}})
		got, _ := reg.Get("dev-abc")
		if want := []string{"permission", "done"}; !reflect.DeepEqual(got.Kinds, want) {
			t.Fatalf("kinds = %v, want %v", got.Kinds, want)
		}
	})
}

func TestPushRegisterDisabled(t *testing.T) {
	t.Parallel()
	api, reg := newPushAPI(t, PushOptions{Enabled: false})
	for _, m := range []string{"push.register", "push.unregister", "push.status"} {
		env := callDevice(t, api, "dev-abc", m, mustJSON(t, pushParams(nil)))
		wantCode(t, env, codePushDisabled)
	}
	if _, ok := reg.Get("dev-abc"); ok {
		t.Fatal("registry touched")
	}
}

func TestPushTokenTakeoverViaRPC(t *testing.T) {
	t.Parallel()
	api, reg := newPushAPI(t, PushOptions{Enabled: true})
	registerOK(t, api, "dev-a", nil)
	registerOK(t, api, "dev-b", nil)
	if _, ok := reg.Get("dev-a"); ok {
		t.Fatal("dev-a should lose the token")
	}
	if _, ok := reg.Get("dev-b"); !ok {
		t.Fatal("dev-b should hold the token")
	}
}

func TestPushUnregisterRemovesOwnRegistration(t *testing.T) {
	t.Parallel()
	api, reg := newPushAPI(t, PushOptions{Enabled: true})
	registerOK(t, api, "dev-a", nil)
	registerOK(t, api, "dev-b", map[string]any{"token": "ExponentPushToken[other]"})
	env := callDevice(t, api, "dev-a", "push.unregister", nil)
	if env.Error != nil {
		t.Fatalf("unregister: %+v", env.Error)
	}
	if string(env.Result) != `{"v":1,"registered":false}` {
		t.Fatalf("result = %s", env.Result)
	}
	if _, ok := reg.Get("dev-a"); ok {
		t.Fatal("dev-a still registered")
	}
	if _, ok := reg.Get("dev-b"); !ok {
		t.Fatal("dev-b removed")
	}
}

func TestPushStatusRedactsSecrets(t *testing.T) {
	t.Parallel()
	api, _ := newPushAPI(t, PushOptions{Enabled: true})

	env := callDevice(t, api, "dev-a", "push.status", nil)
	if env.Error != nil {
		t.Fatalf("status: %+v", env.Error)
	}
	var before map[string]any
	if err := json.Unmarshal(env.Result, &before); err != nil {
		t.Fatal(err)
	}
	if before["registered"] != false {
		t.Fatalf("before = %s", env.Result)
	}
	if _, ok := before["provider"]; ok {
		t.Fatalf("provider present before register: %s", env.Result)
	}

	registerOK(t, api, "dev-a", nil)
	env = callDevice(t, api, "dev-a", "push.status", nil)
	if env.Error != nil {
		t.Fatalf("status: %+v", env.Error)
	}
	text := string(env.Result)
	k := pushKeyBytes(32)
	for _, secret := range []string{testPushToken, "abcDEF123", base64.StdEncoding.EncodeToString(k), hex.EncodeToString(k)} {
		if strings.Contains(text, secret) {
			t.Fatalf("status leaks %q: %s", secret, text)
		}
	}
	var after map[string]any
	if err := json.Unmarshal(env.Result, &after); err != nil {
		t.Fatal(err)
	}
	if after["registered"] != true || after["provider"] != "expo" || after["platform"] != "android" || after["enabled"] != true {
		t.Fatalf("after = %s", text)
	}
}
