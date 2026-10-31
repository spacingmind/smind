package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/wsclient"
)

// fakeRemoteCaller records every call and answers with a canned result or error.
type fakeRemoteCaller struct {
	mu      sync.Mutex
	methods []string
	params  []json.RawMessage
	result  string
	err     error
}

func (f *fakeRemoteCaller) Call(_ context.Context, method string, params, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(params)
	f.methods = append(f.methods, method)
	f.params = append(f.params, raw)
	if f.err != nil {
		return f.err
	}
	if out != nil && f.result != "" {
		return json.Unmarshal([]byte(f.result), out)
	}
	return nil
}

func (f *fakeRemoteCaller) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.methods)
}

// remoteEnv is a fleet env with one approved peer ("peer") and the remote
// bridge wired to a fake caller.
type remoteEnv struct {
	*fleetEnv
	peerID string
	caller *fakeRemoteCaller
	origin string
}

func newRemoteEnv(t *testing.T) *remoteEnv {
	t.Helper()
	e := &remoteEnv{fleetEnv: newFleetEnv(t), caller: &fakeRemoteCaller{}}
	peer := newCallerKey(t)
	e.pairApproved(t, peer, "peer")
	e.peerID = peer.HostID()
	e.api.SetFleetRemote(FleetRemoteOptions{
		Conn:      func(string) (client.Caller, client.LinkState) { return e.caller, client.LinkConnected },
		RunOrigin: func(string) string { return e.origin },
	})
	return e
}

func (e *remoteEnv) call(t *testing.T, rc *requestContext, method string, params any) (json.RawMessage, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.api.hs[method](context.Background(), rc, raw)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return b, nil
}

func wantRemoteCode(t *testing.T, err error, code string) {
	t.Helper()
	var ce *rpcCodeError
	if !errors.As(err, &ce) || ce.code != code {
		t.Fatalf("err = %v, want rpcCodeError %q", err, code)
	}
}

var remoteLocalRC = &requestContext{local: true}

func TestRemoteReadRefusesRemoteOriginRunHopLimit(t *testing.T) {
	e := newRemoteEnv(t)
	rc := &requestContext{local: true, runScoped: true, ident: runIdentity{runID: "r1", taskID: 7}}
	p := RemoteReadParams{Host: "peer", Op: "info"}

	e.origin = "someHost"
	_, err := e.call(t, rc, "remote.read", p)
	wantRemoteCode(t, err, wire.CodeHopLimit)
	if n := e.caller.calls(); n != 0 {
		t.Fatalf("caller called %d times for a remote-origin run", n)
	}

	e.origin = ""
	e.caller.result = `{}`
	if _, err := e.call(t, rc, "remote.read", p); err != nil {
		t.Fatalf("local run: %v", err)
	}
	if n := e.caller.calls(); n != 1 {
		t.Fatalf("caller calls = %d, want 1", n)
	}
	var sent struct {
		Prov struct {
			OriginTask string `json:"originTask"`
		} `json:"prov"`
	}
	if err := json.Unmarshal(e.caller.params[0], &sent); err != nil || sent.Prov.OriginTask != "7" {
		t.Fatalf("originTask = %q (%v), want 7", sent.Prov.OriginTask, err)
	}
}

func TestRemoteReadTypedErrorsPassThrough(t *testing.T) {
	e := newRemoteEnv(t)
	for _, code := range []string{"not_granted", "cycle", "fleet_version_unsupported", "hop_limit"} {
		e.caller.err = &wsclient.RPCError{Code: code, Message: "nope"}
		_, err := e.call(t, remoteLocalRC, "remote.read", RemoteReadParams{Host: "peer", Op: "info"})
		wantRemoteCode(t, err, code)
	}
	e.caller.err = errors.New("boom")
	_, err := e.call(t, remoteLocalRC, "remote.read", RemoteReadParams{Host: "peer", Op: "info"})
	wantRemoteCode(t, err, client.CodeHostUnavailable)
	if !strings.Contains(err.Error(), "peer: ") {
		t.Fatalf("message %q should name the alias", err)
	}
}

func TestRemoteReadTasksCarryGlobalID(t *testing.T) {
	e := newRemoteEnv(t)
	e.caller.result = `{"tasks":[{"id":4,"title":"a"},{"id":9,"title":"b"}],"nextCursor":9}`
	raw, err := e.call(t, remoteLocalRC, "remote.read", RemoteReadParams{Host: "peer", Op: "tasks", WorkspaceID: 1})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Host, HostID, Op string
		Data             struct {
			Tasks []map[string]any `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Host != "peer" || res.HostID != e.peerID || res.Op != "tasks" || len(res.Data.Tasks) != 2 {
		t.Fatalf("result = %s", raw)
	}
	for i, id := range []string{"peer:4", "peer:9"} {
		if tk := res.Data.Tasks[i]; tk["host"] != "peer" || tk["globalId"] != id {
			t.Fatalf("task %d = %v", i, tk)
		}
	}

	e.caller.result = `{"id":4,"title":"a"}`
	raw, err = e.call(t, remoteLocalRC, "remote.read", RemoteReadParams{Host: "peer", Op: "task", ID: 4})
	if err != nil {
		t.Fatal(err)
	}
	var one struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatal(err)
	}
	if one.Data["host"] != "peer" || one.Data["globalId"] != "peer:4" || one.Data["title"] != "a" {
		t.Fatalf("task data = %v", one.Data)
	}

	for _, pr := range e.caller.params {
		var sent struct {
			Prov struct {
				Origin string   `json:"origin"`
				Hops   []string `json:"hops"`
			} `json:"prov"`
		}
		if err := json.Unmarshal(pr, &sent); err != nil {
			t.Fatal(err)
		}
		prov := sent.Prov
		if prov.Origin != e.key.HostID() || !slices.Equal(prov.Hops, []string{e.key.HostID()}) {
			t.Fatalf("prov = %s", pr)
		}
	}
}

func TestRemoteReadStatusUnknownAfterThreeFailures(t *testing.T) {
	e := newRemoteEnv(t)
	e.caller.err = errors.New("boom")
	p := RemoteReadParams{Host: "peer", Op: "status", RunID: "r9", TaskID: 3}
	for i := 0; i < 2; i++ {
		if _, err := e.call(t, remoteLocalRC, "remote.read", p); err == nil {
			t.Fatalf("failure %d returned no error", i+1)
		}
	}
	raw, err := e.call(t, remoteLocalRC, "remote.read", p)
	if err != nil {
		t.Fatalf("third failure: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res["statusUnknown"] != true || res["data"] != nil || res["failures"] != float64(3) {
		t.Fatalf("result = %s", raw)
	}
	if s := string(raw); strings.Contains(s, "finished") || strings.Contains(s, "done") {
		t.Fatalf("unknown status must not claim an end state: %s", s)
	}
}

func TestRemoteHostsListsNoSecrets(t *testing.T) {
	e := newRemoteEnv(t)
	raw, err := e.call(t, remoteLocalRC, "remote.hosts", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Hosts) != 1 {
		t.Fatalf("hosts = %s", raw)
	}
	var keys []string
	for k := range res.Hosts[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"alias", "hostId", "link", "state"}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if res.Hosts[0]["alias"] != "peer" || res.Hosts[0]["hostId"] != e.peerID || res.Hosts[0]["state"] != "approved" {
		t.Fatalf("host = %v", res.Hosts[0])
	}
}

func TestRemoteMethodsNotOverRelay(t *testing.T) {
	e := newRemoteEnv(t)
	rc := &requestContext{local: false}
	for _, m := range []string{"remote.hosts", "remote.read"} {
		_, err := e.call(t, rc, m, RemoteReadParams{Host: "peer", Op: "info"})
		wantRemoteCode(t, err, codeForbidden)
	}
	if n := e.caller.calls(); n != 0 {
		t.Fatalf("caller called %d times over the relay", n)
	}
}

func TestRemoteMethodsNotForHostPrincipal(t *testing.T) {
	newRemoteEnv(t)
	for _, kind := range []string{"pairing", "pending", "approved"} {
		c := &conn{host: &hostPrincipal{hostID: "h", kind: kind}}
		for _, m := range []string{"remote.read", "remote.hosts"} {
			wantRemoteCode(t, c.gate(m), codeForbidden)
		}
	}
}

func TestRemoteMethodsAbsentWhenFleetOff(t *testing.T) {
	api, _ := newCloseTestAPI(t)
	for _, m := range []string{"remote.read", "remote.hosts"} {
		if _, ok := api.hs[m]; ok {
			t.Fatalf("%s registered with fleet off", m)
		}
	}
}
