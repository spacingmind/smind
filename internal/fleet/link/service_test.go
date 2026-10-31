package link

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsapi"
)

// pairCallee is a fakeCallee that offers a pairing and answers
// fleet.pair.request with a SAS (wrongSAS replaces it when set).
type pairCallee struct {
	*fakeCallee
	self     *identity.Key // the caller's key, for the SAS
	wrongSAS bool
	nonceA   []byte
	pairReq  map[string]string
}

func (p *pairCallee) Call(ctx context.Context, method string, params, out any) error {
	switch method {
	case hello.MethodHello:
		p.nonceA = params.(hello.Hello).NonceA
	case hello.MethodPairRequest:
		p.pairReq = params.(map[string]string)
		sas := pairing.SAS(p.self.Public(), p.key.Public(), p.nonceC, p.nonceA)
		if p.wrongSAS {
			sas = "000000"
			if pairing.SAS(p.self.Public(), p.key.Public(), p.nonceC, p.nonceA) == sas {
				sas = "000001"
			}
		}
		b, _ := json.Marshal(pairResult{HostID: p.self.HostID(), Alias: "me", State: "pending", SAS: sas})
		return json.Unmarshal(b, out)
	}
	return p.fakeCallee.Call(ctx, method, params, out)
}

type svcEnv struct {
	svc     *Service
	reg     *registry.Registry
	mgr     *Manager
	secrets *Secrets
	home    string
	self    *identity.Key
	other   *identity.Key
	window  *pairing.Window

	mu        sync.Mutex
	pairDials int
	pc        *pairCallee
	wrong     bool
}

func newSvcEnv(t *testing.T) *svcEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &svcEnv{home: t.TempDir(), self: newKey(t), other: newKey(t), window: pairing.NewWindow(nil)}
	e.reg = registry.New(st, 10)
	e.secrets = OpenSecrets(e.home)
	// The manager's own dials reach a callee that never approves.
	e.mgr = NewManager(ManagerOptions{
		Key: e.self, Registry: e.reg, Secrets: e.secrets, Version: "test",
		BackoffMin: time.Millisecond, BackoffMax: tinyMax, ApprovalPoll: time.Hour,
		Dial: func(context.Context, Transport, string) (Conn, error) {
			f := newCallee(e.other)
			f.state = "pending"
			return f, nil
		},
	})
	t.Cleanup(e.mgr.Close)
	e.svc = NewService(ServiceOptions{
		Key: e.self, Registry: e.reg, Secrets: e.secrets, Manager: e.mgr, Window: e.window,
		Version:  "test",
		Hostname: func() string { return "My Laptop.local" },
		Dial: func(context.Context, Transport, string) (Conn, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.pairDials++
			f := newCallee(e.other)
			f.state = "pairing"
			e.pc = &pairCallee{fakeCallee: f, self: e.self, wrongSAS: e.wrong}
			return e.pc, nil
		},
	})
	return e
}

func (e *svcEnv) inviteFrom(t *testing.T, addrs ...string) string {
	t.Helper()
	inv := pairing.Invite{
		V: pairing.InviteVersion, HostID: e.other.HostID(), Pub: e.other.Public(),
		Addresses: addrs, Expires: time.Now().Add(time.Minute), Nonce: make([]byte, 16),
	}
	s, err := inv.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServiceInviteFragmentOnlyNoPassword(t *testing.T) {
	e := newSvcEnv(t)
	res, err := e.svc.Invite(context.Background(), wsapi.FleetInviteParams{Addresses: []string{"wss://box.example:7777"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Invite, "smind-host://v1#") || strings.Contains(res.Invite, "?") {
		t.Fatalf("invite = %q, want payload in the fragment only", res.Invite)
	}
	inv, err := pairing.Decode(res.Invite)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(inv)
	if strings.Contains(strings.ToLower(string(raw)), "password") || res.HostID != e.self.HostID() {
		t.Fatalf("invite payload %s / host %s", raw, res.HostID)
	}
	if open, exp, rem := e.window.Status(); !open || rem != 1 || !exp.Equal(res.ExpiresAt) {
		t.Fatalf("window open=%v exp=%v rem=%d", open, exp, rem)
	}
	_, err = e.svc.Invite(context.Background(), wsapi.FleetInviteParams{Via: "relay", Addresses: []string{"wss://x:1"}})
	if err == nil || !strings.Contains(err.Error(), "relay invites are not available yet") {
		t.Fatalf("relay invite error = %v", err)
	}
}

func TestServiceInviteNeedsAddress(t *testing.T) {
	e := newSvcEnv(t)
	_, err := e.svc.Invite(context.Background(), wsapi.FleetInviteParams{})
	if err == nil || !strings.Contains(err.Error(), "pass --addr") {
		t.Fatalf("error = %v", err)
	}
	if open, _, _ := e.window.Status(); open {
		t.Fatal("a failed invite left the window open")
	}
	e.svc.o.DefaultAddresses = func() []string { return []string{"ws://127.0.0.1:7777"} }
	if _, err := e.svc.Invite(context.Background(), wsapi.FleetInviteParams{}); err != nil {
		t.Fatalf("default address: %v", err)
	}
}

func TestServiceAddSASMismatchRecordsNothing(t *testing.T) {
	e := newSvcEnv(t)
	e.wrong = true
	_, err := e.svc.Add(context.Background(), wsapi.FleetAddParams{
		Invite: e.inviteFrom(t, "ws://127.0.0.1:1"), Alias: "bee", Password: "pw",
	})
	if err == nil || !strings.Contains(err.Error(), "SAS differs") {
		t.Fatalf("error = %v", err)
	}
	hosts, _ := e.reg.List()
	if len(hosts) != 0 {
		t.Fatalf("hosts = %+v, want none", hosts)
	}
	if _, err := os.Stat(filepath.Join(e.home, SecretsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("secrets file exists after an aborted pairing: %v", err)
	}
	if st := e.mgr.Status(e.other.HostID()); st.Detail != "not linked" {
		t.Fatalf("a link loop exists: %+v", st)
	}
}

func TestServiceAddRefusesCleartextOutsideOverlay(t *testing.T) {
	e := newSvcEnv(t)
	_, err := e.svc.Add(context.Background(), wsapi.FleetAddParams{Invite: e.inviteFrom(t, "ws://192.168.1.5:1")})
	if !errors.Is(err, ErrCleartext) {
		t.Fatalf("error = %v, want ErrCleartext", err)
	}
	if e.pairDials != 0 {
		t.Fatalf("dialed %d times", e.pairDials)
	}
}

func TestServiceAddRecordsPendingInitiatorOneEnabledLink(t *testing.T) {
	e := newSvcEnv(t)
	res, err := e.svc.Add(context.Background(), wsapi.FleetAddParams{
		Invite: e.inviteFrom(t, "ws://127.0.0.1:1"), Alias: "bee", Password: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" || res.HostID != e.other.HostID() || len(res.SAS) != 6 || res.Alias != "bee" {
		t.Fatalf("result = %+v", res)
	}
	if got := e.pc.pairReq["alias"]; got != "my-laptop-local" || e.pc.pairReq["label"] != "My Laptop.local" {
		t.Fatalf("pair request = %v", e.pc.pairReq)
	}
	h, err := e.reg.Get(res.HostID)
	if err != nil || h.State != registry.StatePending {
		t.Fatalf("host = %+v, %v", h, err)
	}
	out, in, err := e.reg.Links(res.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || in.Enabled {
		t.Fatalf("outbound enabled=%v inbound enabled=%v, want true/false", out.Enabled, in.Enabled)
	}
	tr, err := ParseTransport(out.TransportJSON)
	if err != nil || tr.URL != "ws://127.0.0.1:1" || tr.Kind != KindDirect {
		t.Fatalf("transport = %+v, %v", tr, err)
	}
	if ent, ok, err := e.secrets.Get(res.HostID); err != nil || !ok || ent.Password != "s3cret" {
		t.Fatalf("secrets = %v %v %v", ent, ok, err)
	}
	if e.mgr.Status(res.HostID).Detail == "not linked" {
		t.Fatal("manager has no loop for the host")
	}
	_, err = e.svc.Add(context.Background(), wsapi.FleetAddParams{Invite: e.inviteFrom(t, "ws://127.0.0.1:1"), Alias: "other"})
	if err == nil || !strings.Contains(err.Error(), "already paired") {
		t.Fatalf("second add error = %v", err)
	}
}

func TestServiceNotifyReverseWithoutLiveLink(t *testing.T) {
	e := newSvcEnv(t)
	err := e.svc.NotifyReverse(context.Background(), e.other.HostID(), true)
	if err == nil || !strings.Contains(err.Error(), "no live link") {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceSetRouteStoresTransportAndSecret(t *testing.T) {
	e := newSvcEnv(t)
	h, err := e.reg.AcceptPairRequest(e.other.Public(), "bee", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.svc.SetRoute(ctx, wsapi.FleetRouteParams{HostID: h.HostID, Address: "ws://192.168.1.5:1", Password: "pw"}); !errors.Is(err, ErrCleartext) {
		t.Fatalf("cleartext route error = %v", err)
	}
	err = e.svc.SetRoute(ctx, wsapi.FleetRouteParams{HostID: h.HostID, Address: "ws://192.168.1.5:1", Password: "pw", AllowCleartext: true})
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := e.reg.Links(h.HostID)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ParseTransport(out.TransportJSON)
	if err != nil || tr.URL != "ws://192.168.1.5:1" || !tr.AllowCleartext {
		t.Fatalf("transport = %+v, %v", tr, err)
	}
	if out.Enabled {
		t.Fatal("SetRoute enabled the outbound link")
	}
	if ent, _, _ := e.secrets.Get(h.HostID); ent.Password != "pw" {
		t.Fatalf("secret not stored")
	}
	if e.mgr.Status(h.HostID).Detail != "not linked" {
		t.Fatal("a disabled link was started")
	}
}
