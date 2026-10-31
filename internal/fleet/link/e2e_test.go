package link

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The two-daemon end-to-end tests: two real servers in one process, each
// with its own store and home, wired the way cmd/smind's newFleet wires a
// daemon, talking over loopback ws (direct transport).

type e2eQuotaFetcher struct{}

func (e2eQuotaFetcher) Fetch(context.Context, accounts.Account) (quota.Report, error) {
	panic("quota fetch in a fleet e2e test")
}

type e2eDaemon struct {
	name    string
	key     *identity.Key
	reg     *registry.Registry
	secrets *Secrets
	mgr     *Manager
	svc     *Service
	wsURL   string
	db      *store.Store
	wm      *workspace.Manager
	admin   *wsclient.Client
	raw     [][]byte // every fleet.host.list result, for the leak check
}

func newE2EDaemon(t *testing.T, name, password string) *e2eDaemon {
	t.Helper()
	return newE2EDaemonWith(t, name, password, nil)
}

// newE2EDaemonWith is newE2EDaemon with a hook that may adjust the service
// options (the relay e2e adds the relay offer functions there) once the
// server exists.
func newE2EDaemonWith(t *testing.T, name, password string, wire func(srv *server.Server, home string, so *ServiceOptions)) *e2eDaemon {
	t.Helper()
	home := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	areg := accounts.New(db)
	poller := quota.New(db, areg, e2eQuotaFetcher{}, quota.WithEnabled(false))
	t.Cleanup(poller.Close)
	wm := workspace.New(db)
	cfg := config.Default()
	cfg.Fleet.Enabled = true
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PasswordHash = hash
	token := "token-" + name
	srv, err := server.New(cfg, areg, routing.New(db, areg, poller), wm, taskrunner.New(wm), db, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	d := &e2eDaemon{name: name, db: db, wm: wm, wsURL: "ws://" + ts.Listener.Addr().String()}
	if d.key, err = identity.LoadOrCreate(home); err != nil {
		t.Fatal(err)
	}
	d.reg = registry.New(db, cfg.Fleet.MaxHosts)
	d.secrets = OpenSecrets(home)
	d.mgr = NewManager(ManagerOptions{
		Key: d.key, Registry: d.reg, Secrets: d.secrets, Version: "e2e-" + name,
		BackoffMin: 50 * time.Millisecond, BackoffMax: 200 * time.Millisecond, ApprovalPoll: 100 * time.Millisecond,
	})
	t.Cleanup(d.mgr.Close)
	window := pairing.NewWindow(nil)
	so := ServiceOptions{
		Key: d.key, Registry: d.reg, Secrets: d.secrets, Manager: d.mgr, Window: window,
		Version: "e2e-" + name, Hostname: func() string { return "daemon-" + name },
	}
	if wire != nil {
		wire(srv, home, &so)
	}
	d.svc = NewService(so)
	srv.API().SetFleet(wsapi.FleetOptions{Key: d.key, Registry: d.reg, Window: window, Links: d.svc, Store: db, Workspaces: wm, Version: "e2e-" + name})
	d.svc.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if d.admin, err = wsclient.Dial(ctx, ts.Listener.Addr().String(), token); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.admin.Close() })
	return d
}

func (d *e2eDaemon) call(t *testing.T, method string, params, out any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.admin.Call(ctx, method, params, out); err != nil {
		t.Fatalf("%s %s: %v", d.name, method, err)
	}
}

type e2eHostView struct {
	HostID          string `json:"hostId"`
	Alias           string `json:"alias"`
	State           string `json:"state"`
	SAS             string `json:"sas"`
	PendingApproval bool   `json:"pendingApproval"`
	OutboundEnabled bool   `json:"outboundEnabled"`
	Link            string `json:"link"`
}

// hostView returns d's view of hostID, or nil when d does not list it.
func (d *e2eDaemon) hostView(t *testing.T, hostID string) *e2eHostView {
	t.Helper()
	var raw json.RawMessage
	d.call(t, "fleet.host.list", nil, &raw)
	d.raw = append(d.raw, raw)
	var res struct {
		Hosts []e2eHostView `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	for i := range res.Hosts {
		if res.Hosts[i].HostID == hostID {
			return &res.Hosts[i]
		}
	}
	return nil
}

func poll(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// pairE2E runs the whole pairing: B invites, A adds, B approves, and A's
// link comes up. A is the initiator, B the responder.
func pairE2E(t *testing.T) (a, b *e2eDaemon) {
	t.Helper()
	a = newE2EDaemon(t, "a", "pw-alpha-0123456")
	b = newE2EDaemon(t, "b", "pw-bravo-0123456")

	var inv wsapi.FleetInviteResult
	b.call(t, "fleet.host.invite", wsapi.FleetInviteParams{Addresses: []string{b.wsURL}}, &inv)
	if inv.HostID != b.key.HostID() {
		t.Fatalf("invite host = %s, want %s", inv.HostID, b.key.HostID())
	}

	var added wsapi.FleetAddResult
	a.call(t, "fleet.host.add", wsapi.FleetAddParams{Invite: inv.Invite, Alias: "bee", Password: "pw-bravo-0123456", AllowCleartext: true}, &added)
	if added.State != "pending" || len(added.SAS) != 6 || added.HostID != b.key.HostID() {
		t.Fatalf("add result = %+v", added)
	}

	bv := b.hostView(t, a.key.HostID())
	if bv == nil || bv.State != "pending" || bv.SAS != added.SAS {
		t.Fatalf("B's view of A = %+v, want pending with SAS %s", bv, added.SAS)
	}
	poll(t, "A's link to B awaiting approval", func() bool {
		v := a.hostView(t, b.key.HostID())
		return v != nil && v.Link == "connecting"
	})

	var approved json.RawMessage
	b.call(t, "fleet.host.approve", map[string]any{"hostId": a.key.HostID(), "sas": added.SAS, "writeWorkspaces": []int64{1}}, &approved)
	poll(t, "A's link to B connected and approved", func() bool {
		v := a.hostView(t, b.key.HostID())
		return v != nil && v.Alias == "bee" && v.State == registry.StateApproved && v.Link == "active"
	})
	return a, b
}

func TestFleetPairDirectE2E(t *testing.T) {
	a, b := pairE2E(t)

	aOut, aIn, err := a.reg.Links(b.key.HostID())
	if err != nil {
		t.Fatal(err)
	}
	if !aOut.Enabled || aIn.Enabled {
		t.Fatalf("A registry: outbound=%v inbound=%v, want true/false", aOut.Enabled, aIn.Enabled)
	}
	bOut, bIn, err := b.reg.Links(a.key.HostID())
	if err != nil {
		t.Fatal(err)
	}
	if bOut.Enabled || !bIn.Enabled {
		t.Fatalf("B registry: outbound=%v inbound=%v, want false/true", bOut.Enabled, bIn.Enabled)
	}
	g, err := registry.ParseGrant(bIn.GrantJSON)
	if err != nil || len(g.WorkspacesWrite) != 1 || g.WorkspacesWrite[0] != 1 {
		t.Fatalf("B's grant for A = %+v, %v; want write workspace 1", g, err)
	}

	for _, d := range []*e2eDaemon{a, b} {
		d.hostView(t, "") // one more list for the leak check
		for _, needle := range [][]byte{
			[]byte("pw-alpha-0123456"), []byte("pw-bravo-0123456"),
			[]byte(base64.StdEncoding.EncodeToString(a.key.Public())),
			[]byte(base64.StdEncoding.EncodeToString(b.key.Public())),
		} {
			for _, raw := range d.raw {
				if bytes.Contains(raw, needle) {
					t.Fatalf("%s: fleet.host.list leaks %q: %s", d.name, needle, raw)
				}
			}
		}
	}
}

func TestFleetReverseDisabledByDefault(t *testing.T) {
	a, b := pairE2E(t)
	aID := a.key.HostID()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// B holds no route to A and its outbound link is dark. Give it a route
	// and light the flag by hand to force one dial: A has not enabled its
	// reverse direction, so it must refuse with not_granted.
	var ok struct {
		OK bool `json:"ok"`
	}
	b.call(t, "fleet.host.route", map[string]any{"host": aID, "address": a.wsURL, "password": "pw-alpha-0123456", "allowCleartext": true}, &ok)
	if err := b.reg.SetOutboundEnabled(aID, true); err != nil {
		t.Fatal(err)
	}
	res, err := b.svc.Test(ctx, aID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.State != string(StatePeerDisabled) || res.Code != "not_granted" {
		t.Fatalf("B->A test before enable_reverse = %+v, want peer_disabled/not_granted", res)
	}
	if err := b.reg.SetOutboundEnabled(aID, false); err != nil {
		t.Fatal(err)
	}

	// A enables reverse: the notice flips B's outbound flag and B starts
	// dialing on the route it already has.
	a.call(t, "fleet.host.enable_reverse", map[string]any{"host": "bee"}, &json.RawMessage{})
	poll(t, "B's link to A connected", func() bool {
		v := b.hostView(t, aID)
		return b.mgr.Status(aID).State == StateConnected && v != nil && v.OutboundEnabled && v.Link == "active"
	})

	// A host principal can only speak fleet: a human method is forbidden.
	c, live := b.mgr.Conn(aID)
	if !live {
		t.Fatal("no live conn B->A")
	}
	err = c.Call(ctx, "task.list", nil, nil)
	var re *wsclient.RPCError
	if !errors.As(err, &re) || re.Code != "forbidden" {
		t.Fatalf("task.list as a host principal: %v, want code forbidden", err)
	}

	// A disables reverse: B's link leaves connected, and a fresh dial is
	// refused again.
	a.call(t, "fleet.host.disable_reverse", map[string]any{"host": "bee"}, &json.RawMessage{})
	poll(t, "B's link to A to leave connected", func() bool {
		return b.mgr.Status(aID).State != StateConnected
	})
	if err := b.reg.SetOutboundEnabled(aID, true); err != nil { // the notice darkened it; force one dial
		t.Fatal(err)
	}
	res, err = b.svc.Test(ctx, aID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.State != string(StatePeerDisabled) || res.Code != "not_granted" {
		t.Fatalf("B->A test after disable_reverse = %+v, want peer_disabled/not_granted", res)
	}
}

// A reverse notice missed while the link was down is re-sent when it comes up.
func TestFleetReverseNoticeSentOnReconnect(t *testing.T) {
	a, b := pairE2E(t)
	aID, bID := a.key.HostID(), b.key.HostID()

	// Simulate a missed notice: A enables its direction to B without telling B.
	if err := a.reg.EnableReverse(bID); err != nil {
		t.Fatal(err)
	}
	if v := b.hostView(t, aID); v == nil || v.OutboundEnabled {
		t.Fatalf("B's outbound to A = %+v before reconnect, want disabled", v)
	}

	a.mgr.Drop(bID)
	a.mgr.Ensure(bID)
	poll(t, "B learned A enabled reverse", func() bool {
		v := b.hostView(t, aID)
		return v != nil && v.OutboundEnabled
	})
}
