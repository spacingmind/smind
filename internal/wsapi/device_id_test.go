package wsapi

import (
	"context"
	"encoding/json"
	"testing"
)

func registerWhoami(api *API) {
	api.hs["test.whoami"] = func(ctx context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		return map[string]any{"deviceId": rc.deviceID, "local": rc.local}, nil
	}
}

type whoami struct {
	DeviceID string `json:"deviceId"`
	Local    bool   `json:"local"`
}

func decodeWhoami(t *testing.T, env envelope) whoami {
	t.Helper()
	if env.Error != nil {
		t.Fatalf("test.whoami error: %+v", env.Error)
	}
	var w whoami
	if err := json.Unmarshal(env.Result, &w); err != nil {
		t.Fatalf("decode %s: %v", env.Result, err)
	}
	return w
}

func whoamiOverTransport(t *testing.T, serve func(api *API, ctx context.Context, tr Transport)) whoami {
	t.Helper()
	api, _ := newRelayDeviceAPI(t, newFakeAdmin())
	registerWhoami(api)
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serve(api, ctx, server)
	defer client.Close()

	data, _ := json.Marshal(envelope{ID: "w", Method: "test.whoami"})
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
	return decodeWhoami(t, env)
}

func TestServeDevicePassesVerifiedDeviceID(t *testing.T) {
	t.Parallel()
	w := whoamiOverTransport(t, func(api *API, ctx context.Context, tr Transport) {
		api.ServeDevice(ctx, tr, "dev-abc")
	})
	if w.DeviceID != "dev-abc" || w.Local {
		t.Fatalf("whoami = %+v, want deviceId dev-abc, local false", w)
	}
}

func TestServeTransportHasNoDeviceID(t *testing.T) {
	t.Parallel()
	w := whoamiOverTransport(t, func(api *API, ctx context.Context, tr Transport) {
		api.ServeTransport(ctx, tr)
	})
	if w.DeviceID != "" || w.Local {
		t.Fatalf("whoami = %+v, want empty deviceId, local false", w)
	}
}

func TestLocalWSHasNoDeviceID(t *testing.T) {
	t.Parallel()
	api, srv := newRelayDeviceAPI(t, newFakeAdmin())
	registerWhoami(api)
	ws := dialWS(t, srv, "tok")
	w := decodeWhoami(t, callWS(t, ws, "test.whoami", nil))
	if w.DeviceID != "" || !w.Local {
		t.Fatalf("whoami = %+v, want empty deviceId, local true", w)
	}
}
