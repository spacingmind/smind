package wsapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/selector"
	"github.com/spacingmind/smind/internal/store"
)

// selectEnv is a fleet env with named approved peers whose link state and
// outbound flag the test controls.
type selectEnv struct {
	*fleetEnv
	ids       map[string]string // alias -> host id
	connected map[string]bool   // host id -> live link
	origin    string            // host the calling run arrived from
}

func newSelectEnv(t *testing.T, aliases ...string) *selectEnv {
	t.Helper()
	e := &selectEnv{fleetEnv: newFleetEnv(t), ids: map[string]string{}, connected: map[string]bool{}}
	for _, a := range aliases {
		k := newCallerKey(t)
		e.pairApproved(t, k, a)
		e.ids[a] = k.HostID()
		e.connected[k.HostID()] = true
		if err := e.reg.SetOutboundEnabled(k.HostID(), true); err != nil {
			t.Fatal(err)
		}
	}
	e.api.SetFleetRemote(FleetRemoteOptions{
		Conn: func(id string) (client.Caller, client.LinkState) {
			if e.connected[id] {
				return &fakeRemoteCaller{}, client.LinkConnected
			}
			return nil, client.LinkOther
		},
		RunOrigin: func(string, int64) (string, error) { return e.origin, nil },
	})
	return e
}

func (e *selectEnv) label(t *testing.T, alias string, m map[string]string) {
	t.Helper()
	if err := e.reg.SetLabels(e.ids[alias], m); err != nil {
		t.Fatal(err)
	}
}

func (e *selectEnv) call(t *testing.T, rc *requestContext, params any) (json.RawMessage, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := e.api.hs["remote.select"](context.Background(), rc, raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out)
	return b, nil
}

func winLabels() map[string]string { return map[string]string{"os": "windows"} }

func TestFleetRemoteSelect_ExactOne(t *testing.T) {
	e := newSelectEnv(t, "win", "mac")
	e.label(t, "win", map[string]string{"os": "windows", "gpu": "yes"})
	e.label(t, "mac", map[string]string{"os": "mac"})
	raw, err := e.call(t, remoteLocalRC, RemoteSelectParams{Labels: winLabels()})
	if err != nil {
		t.Fatal(err)
	}
	var res RemoteSelectResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Host != "win" || res.HostID != e.ids["win"] {
		t.Fatalf("result = %+v", res)
	}
}

func TestFleetRemoteSelect_ZeroListsNothingLeaky(t *testing.T) {
	e := newSelectEnv(t, "mac")
	e.label(t, "mac", map[string]string{"os": "mac"})
	_, err := e.call(t, remoteLocalRC, RemoteSelectParams{Labels: winLabels()})
	wantRemoteCode(t, err, selector.CodeNoMatch)
	if strings.Contains(err.Error(), "mac") {
		t.Fatalf("message names a host that does not match: %v", err)
	}
}

func TestFleetRemoteSelect_AmbiguousNamesCandidates(t *testing.T) {
	e := newSelectEnv(t, "win1", "win2")
	e.label(t, "win1", winLabels())
	e.label(t, "win2", winLabels())
	_, err := e.call(t, remoteLocalRC, RemoteSelectParams{Labels: winLabels()})
	wantRemoteCode(t, err, selector.CodeAmbiguous)
	if !strings.Contains(err.Error(), "win1") || !strings.Contains(err.Error(), "win2") {
		t.Fatalf("message = %v", err)
	}
}

func TestFleetRemoteSelect_AnyPicksLeastLive(t *testing.T) {
	e := newSelectEnv(t, "win1", "win2")
	e.label(t, "win1", winLabels())
	e.label(t, "win2", winLabels())
	_, err := e.fleetStore().PutFleetRemoteTaskAndWatch(store.FleetRemoteTask{
		HostID: e.ids["win1"], RemoteTaskID: "rt-1", Title: "busy", State: "running",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := e.call(t, remoteLocalRC, RemoteSelectParams{Labels: winLabels(), Any: true})
	if err != nil {
		t.Fatal(err)
	}
	var res RemoteSelectResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Host != "win2" {
		t.Fatalf("picked %q, want win2 (win1 has a running task)", res.Host)
	}
}

func TestFleetRemoteSelect_DisconnectedAndDisabledSkipped(t *testing.T) {
	e := newSelectEnv(t, "off", "down")
	e.label(t, "off", winLabels())
	e.label(t, "down", winLabels())
	if err := e.reg.SetOutboundEnabled(e.ids["off"], false); err != nil {
		t.Fatal(err)
	}
	e.connected[e.ids["down"]] = false
	_, err := e.call(t, remoteLocalRC, RemoteSelectParams{Labels: winLabels()})
	wantRemoteCode(t, err, selector.CodeNoMatch)
	if !strings.Contains(err.Error(), "off") || !strings.Contains(err.Error(), "down") || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("message = %v", err)
	}
}

func TestFleetRemoteSelect_RunScopedPeerRefused(t *testing.T) {
	e := newSelectEnv(t, "win")
	e.label(t, "win", winLabels())
	e.origin = "someHost"
	rc := &requestContext{local: true, runScoped: true, ident: runIdentity{runID: "r1", taskID: 7}}
	_, err := e.call(t, rc, RemoteSelectParams{Labels: winLabels()})
	wantRemoteCode(t, err, "hop_limit")
}

func TestFleetRemoteSelect_ResultHasNoLabels(t *testing.T) {
	e := newSelectEnv(t, "win")
	e.label(t, "win", map[string]string{"os": "windows", "team": "zebra-secret"})
	raw, err := e.call(t, remoteLocalRC, RemoteSelectParams{Labels: winLabels()})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["labels"]; ok || strings.Contains(string(raw), "zebra-secret") || strings.Contains(string(raw), "windows") {
		t.Fatalf("result leaks labels: %s", raw)
	}
}
