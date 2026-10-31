package wsapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// fakeFleetLinks records the dial-side calls the admin methods make.
type fakeFleetLinks struct {
	mu          sync.Mutex
	notified    []string // "<host>:<on>"
	peerChanged []string // "<host>:<on>"
	forgotten   []string
	notifyErr   error
	routes      []FleetRouteParams
}

func (f *fakeFleetLinks) Invite(context.Context, FleetInviteParams) (FleetInviteResult, error) {
	return FleetInviteResult{Invite: "smind://fake"}, nil
}

func (f *fakeFleetLinks) Add(context.Context, FleetAddParams) (FleetAddResult, error) {
	return FleetAddResult{}, nil
}

func (f *fakeFleetLinks) Test(context.Context, string) (FleetTestResult, error) {
	return FleetTestResult{OK: true}, nil
}

func (f *fakeFleetLinks) NotifyReverse(_ context.Context, hostID string, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notified = append(f.notified, hostID+":"+boolStr(on))
	return f.notifyErr
}

func (f *fakeFleetLinks) PeerReverseChanged(hostID string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.peerChanged = append(f.peerChanged, hostID+":"+boolStr(on))
}

func (f *fakeFleetLinks) SetRoute(_ context.Context, p FleetRouteParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes = append(f.routes, p)
	return nil
}

func (f *fakeFleetLinks) LinkState(string) (string, string) { return "down", "" }

func (f *fakeFleetLinks) Forget(hostID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgotten = append(f.forgotten, hostID)
}

func (f *fakeFleetLinks) snapshot() (notified, peerChanged []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.notified), slices.Clone(f.peerChanged)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type fleetEnv struct {
	api    *API
	srv    *httptest.Server
	reg    *registry.Registry
	key    *identity.Key
	window *pairing.Window
	links  *fakeFleetLinks
}

func newFleetEnv(t *testing.T) *fleetEnv {
	t.Helper()
	api, srv := newCloseTestAPI(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &fleetEnv{
		api:    api,
		srv:    srv,
		reg:    registry.New(st, 8),
		key:    key,
		window: pairing.NewWindow(nil),
		links:  &fakeFleetLinks{},
	}
	api.SetFleet(FleetOptions{Key: key, Registry: e.reg, Window: e.window, Links: e.links, Version: "test"})
	return e
}

func (e *fleetEnv) dial(t *testing.T) *wsclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, err := wsclient.Dial(ctx, e.srv.Listener.Addr().String(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	return cl
}

func callCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel // bounded by the timeout; tests are short-lived
	return ctx
}

func newCallerKey(t *testing.T) *identity.Key {
	t.Helper()
	k, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

type helloResult struct {
	ok     hello.HelloOK
	nonceC []byte
	nonceA []byte
	ts     int64
}

// doHello runs the challenge/hello exchange as caller on cl.
func (e *fleetEnv) doHello(t *testing.T, cl *wsclient.Client, caller *identity.Key) (helloResult, error) {
	t.Helper()
	var ch hello.Challenge
	if err := cl.Call(callCtx(), hello.MethodChallenge, nil, &ch); err != nil {
		t.Fatalf("fleet.challenge: %v", err)
	}
	nonceA, err := hello.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Unix()
	h := hello.SignHello(caller, ch.HostID, ch.NonceC, nonceA, ts, "caller", hello.Caps())
	var ok hello.HelloOK
	if err := cl.Call(callCtx(), hello.MethodHello, h, &ok); err != nil {
		return helloResult{}, err
	}
	return helloResult{ok: ok, nonceC: ch.NonceC, nonceA: nonceA, ts: ts}, nil
}

func rpcCode(t *testing.T, err error) string {
	t.Helper()
	var re *wsclient.RPCError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v, want *wsclient.RPCError", err)
	}
	return re.Code
}

func wantFleetCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %q", code)
	}
	if got := rpcCode(t, err); got != code {
		t.Fatalf("error = %q (code %q), want code %q", err, got, code)
	}
}

// pairApproved makes caller an approved host with inbound enabled.
func (e *fleetEnv) pairApproved(t *testing.T, caller *identity.Key, alias string) {
	t.Helper()
	if _, err := e.reg.AcceptPairRequest(caller.Public(), alias, ""); err != nil {
		t.Fatal(err)
	}
	g := registry.DefaultForwardGrant()
	g.WorkspacesWrite = []int64{1}
	if err := e.reg.Approve(caller.HostID(), g); err != nil {
		t.Fatal(err)
	}
}

func TestHelloUnapprovedRefused(t *testing.T) {
	e := newFleetEnv(t)
	caller := newCallerKey(t)

	// (a) unknown host, no window.
	_, err := e.doHello(t, e.dial(t), caller)
	wantFleetCode(t, err, hello.CodeUnknownHost)

	// (b) pending.
	if _, err := e.reg.AcceptPairRequest(caller.Public(), "peer", ""); err != nil {
		t.Fatal(err)
	}
	_, err = e.doHello(t, e.dial(t), caller)
	wantFleetCode(t, err, hello.CodeAwaitingApproval)

	// (d) approved, inbound link dark.
	g := registry.DefaultForwardGrant()
	g.WorkspacesWrite = []int64{1}
	if err := e.reg.Approve(caller.HostID(), g); err != nil {
		t.Fatal(err)
	}
	if err := e.reg.DisableReverse(caller.HostID()); err != nil {
		t.Fatal(err)
	}
	_, err = e.doHello(t, e.dial(t), caller)
	wantFleetCode(t, err, hello.CodeNotGranted)

	// (e) approved, inbound enabled.
	if err := e.reg.EnableReverse(caller.HostID()); err != nil {
		t.Fatal(err)
	}
	res, err := e.doHello(t, e.dial(t), caller)
	if err != nil {
		t.Fatalf("approved hello: %v", err)
	}
	if res.ok.State != "active" {
		t.Fatalf("state = %q, want active", res.ok.State)
	}
	if err := hello.VerifyOK(res.ok, e.key.Public(), caller.HostID(), res.nonceC, res.nonceA, res.ts); err != nil {
		t.Fatalf("VerifyOK: %v", err)
	}

	// (c) revoked.
	if err := e.reg.Revoke(caller.HostID()); err != nil {
		t.Fatal(err)
	}
	_, err = e.doHello(t, e.dial(t), caller)
	wantFleetCode(t, err, hello.CodeRevoked)
}

func TestFleetPairingFlowPendingThenApprove(t *testing.T) {
	e := newFleetEnv(t)
	e.window.Open(0, 0)
	first, second := newCallerKey(t), newCallerKey(t)
	c1, c2 := e.dial(t), e.dial(t)

	r1, err := e.doHello(t, c1, first)
	if err != nil || r1.ok.State != "pairing" {
		t.Fatalf("first hello = %+v, %v; want state pairing", r1.ok, err)
	}
	r2, err := e.doHello(t, c2, second)
	if err != nil || r2.ok.State != "pairing" {
		t.Fatalf("second hello = %+v, %v; want state pairing", r2.ok, err)
	}

	var pr fleetPairRequestResult
	if err := c1.Call(callCtx(), hello.MethodPairRequest, fleetPairRequestParams{Alias: "laptop", Label: "Laptop"}, &pr); err != nil {
		t.Fatalf("pair.request: %v", err)
	}
	if pr.State != registry.StatePending || pr.HostID != first.HostID() {
		t.Fatalf("pair.request = %+v", pr)
	}
	want := pairing.SAS(first.Public(), e.key.Public(), r1.nonceC, r1.nonceA)
	if len(pr.SAS) != 6 || pr.SAS != want {
		t.Fatalf("sas = %q, want %q (6 digits)", pr.SAS, want)
	}

	// The single slot is gone.
	err = c2.Call(callCtx(), hello.MethodPairRequest, fleetPairRequestParams{Alias: "other"}, &fleetPairRequestResult{})
	wantFleetCode(t, err, hello.CodeNotGranted)

	h, err := e.reg.Get(first.HostID())
	if err != nil || h.State != registry.StatePending {
		t.Fatalf("host = %+v, %v; want pending", h, err)
	}
	if _, inbound, err := e.reg.Links(first.HostID()); err != nil || inbound.Enabled {
		t.Fatalf("inbound = %+v, %v; want disabled", inbound, err)
	}

	admin := e.dial(t)
	var list fleetHostListResult
	if err := admin.Call(callCtx(), "fleet.host.list", nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Hosts) != 1 || list.Hosts[0].SAS != want {
		t.Fatalf("list = %+v, want one host showing the SAS", list)
	}

	err = admin.Call(callCtx(), "fleet.host.approve", fleetHostApproveParams{Host: "laptop", WriteWorkspaces: []int64{}}, &FleetHostView{})
	if err == nil {
		t.Fatal("approve with no write workspaces succeeded, want an error")
	}
	var view FleetHostView
	if err := admin.Call(callCtx(), "fleet.host.approve", fleetHostApproveParams{Host: "laptop", WriteWorkspaces: []int64{1}}, &view); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if view.State != registry.StateApproved || !view.Inbound.Enabled || view.SAS != "" {
		t.Fatalf("view = %+v, want approved, inbound enabled, no SAS", view)
	}
	res, err := e.doHello(t, e.dial(t), first)
	if err != nil || res.ok.State != "active" {
		t.Fatalf("later hello = %+v, %v; want active", res.ok, err)
	}
}

func TestHostPrincipalOnlyFleetMethods(t *testing.T) {
	e := newFleetEnv(t)
	approved := newCallerKey(t)
	e.pairApproved(t, approved, "approved")

	// Approved principal: everything but link.state is forbidden.
	cl := e.dial(t)
	if _, err := e.doHello(t, cl, approved); err != nil {
		t.Fatal(err)
	}
	methods := []string{"no.such.method"}
	for k := range e.api.hs {
		methods = append(methods, k)
	}
	for _, m := range methods {
		if m == hello.MethodLinkState {
			continue
		}
		wantFleetCode(t, cl.Call(callCtx(), m, map[string]any{}, nil), codeForbidden)
	}
	err := cl.Call(callCtx(), hello.MethodLinkState, fleetLinkStateParams{State: "bogus"}, nil)
	wantFleetCode(t, err, codeInvalid)

	// Pairing principal: only fleet.pair.request.
	e.window.Open(0, 0)
	pc := e.dial(t)
	if _, err := e.doHello(t, pc, newCallerKey(t)); err != nil {
		t.Fatal(err)
	}
	for _, m := range methods {
		if m == hello.MethodPairRequest {
			continue
		}
		wantFleetCode(t, pc.Call(callCtx(), m, map[string]any{}, nil), codeForbidden)
	}
	if err := pc.Call(callCtx(), hello.MethodPairRequest, fleetPairRequestParams{Alias: "cand"}, &fleetPairRequestResult{}); err != nil {
		t.Fatalf("pair.request: %v", err)
	}

	// The same connection is now pending: nothing at all.
	for _, m := range append(methods, hello.MethodPairRequest) {
		wantFleetCode(t, pc.Call(callCtx(), m, map[string]any{}, nil), codeForbidden)
	}
}

func TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin(t *testing.T) {
	e := newFleetEnv(t)

	var adminMethods []string
	for k := range e.api.hs {
		if strings.HasPrefix(k, "fleet.host.") {
			adminMethods = append(adminMethods, k)
		}
	}
	if len(adminMethods) != 12 {
		t.Fatalf("fleet.host.* methods = %v, want 12", adminMethods)
	}

	// (1) not run-scoped, not in any plugin capability group.
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "fleet.") {
			t.Errorf("RunScopedMethods contains %q", m)
		}
	}
	for group, ms := range PluginCapabilityTable {
		for _, m := range ms {
			if strings.HasPrefix(m.Name, "fleet.") {
				t.Errorf("PluginCapabilityTable[%s] contains %q", group, m.Name)
			}
		}
	}

	// (2) refused on a non-local connection, calling the handler directly.
	for _, m := range adminMethods {
		_, err := e.api.hs[m](context.Background(), &requestContext{local: false}, []byte(`{}`))
		var ce *rpcCodeError
		if !errors.As(err, &ce) || ce.code != codeForbidden {
			t.Errorf("%s on a non-local conn: error = %v, want code %s", m, err, codeForbidden)
		}
	}

	// (3) a run-scoped connection never gets past the gate.
	c := &conn{runScoped: true}
	for _, m := range adminMethods {
		err := c.gate(m)
		var ce *rpcCodeError
		if !errors.As(err, &ce) || ce.code != codeForbidden || ce.msg != "not available to agents" {
			t.Errorf("%s on a run-scoped conn: gate = %v, want forbidden \"not available to agents\"", m, err)
		}
	}
}

func TestFleetParamsHaveNoGrantOrSecretFields(t *testing.T) {
	banned := []string{"grant", "ops", "workspace", "secret", "password", "token", "env", "policy", "enabled", "cost", "budget", "cwd", "mode"}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(hello.Hello{}),
		reflect.TypeOf(fleetPairRequestParams{}),
		reflect.TypeOf(fleetLinkStateParams{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			names := []string{strings.ToLower(f.Name), strings.ToLower(f.Tag.Get("json"))}
			for _, n := range names {
				for _, b := range banned {
					if strings.Contains(n, b) {
						t.Errorf("%s.%s (%q) contains banned word %q", typ.Name(), f.Name, n, b)
					}
				}
			}
		}
	}
}

func sortedHandlerKeys(a *API) []string {
	keys := make([]string, 0, len(a.hs))
	for k := range a.hs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestWsapiMethodMapGoldenFleetOff(t *testing.T) {
	const golden = "testdata/methods.golden"
	api, _ := newCloseTestAPI(t)
	before := sortedHandlerKeys(api)

	t.Run("fleet off", func(t *testing.T) {
		if os.Getenv("SMIND_UPDATE_GOLDEN") != "" {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, []byte(strings.Join(before, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden (SMIND_UPDATE_GOLDEN=1 regenerates it): %v", err)
		}
		want := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		if !slices.Equal(before, want) {
			t.Fatalf("handler map changed with fleet off:\n got %v\nwant %v", before, want)
		}
	})

	t.Run("fleet on adds exactly the fleet methods", func(t *testing.T) {
		e := newFleetEnv(t)
		have := map[string]bool{}
		for _, k := range before {
			have[k] = true
		}
		var added []string
		for _, k := range sortedHandlerKeys(e.api) {
			if !have[k] {
				added = append(added, k)
			}
		}
		want := []string{
			"fleet.challenge", "fleet.hello", "fleet.pair.request", "fleet.link.state",
			"fleet.host.list", "fleet.host.invite", "fleet.host.add", "fleet.host.approve",
			"fleet.host.reject", "fleet.host.rename", "fleet.host.grant", "fleet.host.rm",
			"fleet.host.enable_reverse", "fleet.host.disable_reverse", "fleet.host.test", "fleet.host.route",
		}
		slices.Sort(want)
		if !slices.Equal(added, want) {
			t.Fatalf("added = %v, want %v", added, want)
		}
	})
}

func TestFleetLinkStateFlipsOutbound(t *testing.T) {
	e := newFleetEnv(t)
	caller := newCallerKey(t)
	e.pairApproved(t, caller, "peer")
	cl := e.dial(t)
	if _, err := e.doHello(t, cl, caller); err != nil {
		t.Fatal(err)
	}
	outbound := func() bool {
		o, _, err := e.reg.Links(caller.HostID())
		if err != nil {
			t.Fatal(err)
		}
		return o.Enabled
	}

	var res fleetLinkStateResult
	if err := cl.Call(callCtx(), hello.MethodLinkState, fleetLinkStateParams{State: "reverse_enabled"}, &res); err != nil || !res.OK {
		t.Fatalf("reverse_enabled = %+v, %v", res, err)
	}
	if !outbound() {
		t.Fatal("outbound link not enabled after reverse_enabled")
	}
	if err := cl.Call(callCtx(), hello.MethodLinkState, fleetLinkStateParams{State: "reverse_disabled"}, &res); err != nil {
		t.Fatal(err)
	}
	if outbound() {
		t.Fatal("outbound link still enabled after reverse_disabled")
	}
	_, peer := e.links.snapshot()
	id := caller.HostID()
	if want := []string{id + ":true", id + ":false"}; !slices.Equal(peer, want) {
		t.Fatalf("PeerReverseChanged calls = %v, want %v", peer, want)
	}
}

func TestFleetEnableDisableReverseClosesConnection(t *testing.T) {
	e := newFleetEnv(t)
	caller := newCallerKey(t)
	e.pairApproved(t, caller, "peer")
	hc := e.dial(t)
	if _, err := e.doHello(t, hc, caller); err != nil {
		t.Fatal(err)
	}
	admin := e.dial(t)

	if err := admin.Call(callCtx(), "fleet.host.disable_reverse", fleetHostParams{Host: "peer"}, &fleetHostActionResult{}); err != nil {
		t.Fatalf("disable_reverse: %v", err)
	}
	select {
	case <-hc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("host connection still open 2s after disable_reverse")
	}
	_, err := e.doHello(t, e.dial(t), caller)
	wantFleetCode(t, err, hello.CodeNotGranted)

	var res fleetHostActionResult
	if err := admin.Call(callCtx(), "fleet.host.enable_reverse", fleetHostParams{Host: "peer"}, &res); err != nil {
		t.Fatalf("enable_reverse: %v", err)
	}
	notified, _ := e.links.snapshot()
	id := caller.HostID()
	if want := []string{id + ":false", id + ":true"}; !slices.Equal(notified, want) {
		t.Fatalf("NotifyReverse calls = %v, want %v", notified, want)
	}
	if _, err := e.doHello(t, e.dial(t), caller); err != nil {
		t.Fatalf("hello after enable_reverse: %v", err)
	}
}

func TestFleetHostRouteIsLocalOnlyAndResolvesHost(t *testing.T) {
	e := newFleetEnv(t)
	peer := newCallerKey(t)
	e.pairApproved(t, peer, "peer")

	_, err := e.api.hs["fleet.host.route"](context.Background(), &requestContext{local: false}, []byte(`{}`))
	var ce *rpcCodeError
	if !errors.As(err, &ce) || ce.code != codeForbidden {
		t.Fatalf("non-local route: error = %v, want code %s", err, codeForbidden)
	}

	cl := e.dial(t)
	var res fleetOKResult
	p := map[string]any{"host": "peer", "address": "ws://127.0.0.1:9", "password": "pw", "allowCleartext": true}
	if err := cl.Call(callCtx(), "fleet.host.route", p, &res); err != nil || !res.OK {
		t.Fatalf("route = %+v, %v", res, err)
	}
	e.links.mu.Lock()
	got := slices.Clone(e.links.routes)
	e.links.mu.Unlock()
	want := FleetRouteParams{HostID: peer.HostID(), Address: "ws://127.0.0.1:9", Password: "pw", AllowCleartext: true}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("SetRoute calls = %+v, want [%+v]", got, want)
	}
	p["host"] = "nobody"
	wantFleetCode(t, cl.Call(callCtx(), "fleet.host.route", p, &res), codeNotFound)
}
