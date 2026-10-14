package bridge

// hub_fixture_test.go — the harness the device-hub tests share: a real relay
// (relaytest.StartRelay), a real Hub with a real device registry and either a
// real wsapi.API or a stub, and device stubs that do what a P13 client does
// (admit, open a route with their own device id, E2EE handshake, device_hello,
// then JSON requests) over native gRPC or grpc-web.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/relaytest"
)

const fixtureTimeout = 15 * time.Second

type hubFixture struct {
	t      *testing.T
	relay  *relaytest.Relay
	cfg    Config
	kp     *e2ee.KeyPair
	reg    *devices.Registry
	api    Server
	opts   Options
	logs   *logSink
	cancel context.CancelFunc
	done   chan struct{}
	hub    *Hub
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logSink) contains(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if containsStr(s, sub) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// newHubFixture starts a relay and prepares (but does not start) a hub.
// Options.Registry/Logf are filled in; the caller may preset the rest.
func newHubFixture(t *testing.T, opts Options, relayOpts ...relaytest.Option) *hubFixture {
	t.Helper()
	relay := relaytest.StartRelay(t, relayOpts...)
	kp, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := devices.Open(filepath.Join(t.TempDir(), "relay-devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &hubFixture{
		t: t, relay: relay, kp: kp, reg: reg, logs: &logSink{},
		cfg: Config{
			RelayAddress: relay.NativeAddr,
			WorkspaceID:  relay.HostID,
			SecretHex:    hex.EncodeToString(relay.Secret),
			Fingerprint:  relay.Fingerprint,
		},
	}
	if opts.Registry == nil {
		opts.Registry = reg
	} else {
		f.reg = opts.Registry
	}
	opts.Logf = f.logs.logf
	f.opts = opts
	return f
}

// start runs a hub (a fresh one each call, sharing the registry) serving api
// (a real wsapi.API when nil).
func (f *hubFixture) start(api Server) *Hub {
	f.t.Helper()
	if api == nil {
		api = newTestAPI(f.t)
	}
	f.api = api
	f.hub = NewHub(f.opts)
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.done = make(chan struct{})
	go func() {
		defer close(f.done)
		_ = f.hub.Run(ctx, f.cfg, f.kp, api)
	}()
	f.t.Cleanup(f.stop)
	return f.hub
}

func (f *hubFixture) stop() {
	if f.cancel == nil {
		return
	}
	f.cancel()
	select {
	case <-f.done:
	case <-time.After(fixtureTimeout):
		f.t.Error("hub did not stop")
	}
	f.cancel = nil
}

// device is a P13-style client: persistent Ed25519 key, fresh ephemeral
// E2EE key per session.
type device struct {
	t     *testing.T
	f     *hubFixture
	web   bool
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	id    string
	label string

	ctx    context.Context
	cancel context.CancelFunc
	dc     *client.DataConn
	eph    *e2ee.KeyPair // this session's ephemeral handshake key
	msgs   chan []byte
	rerr   chan error

	session string
	nextID  int
}

func (f *hubFixture) newDevice(web bool, label string) *device {
	f.t.Helper()
	pub, priv, err := devices.GenerateKey()
	if err != nil {
		f.t.Fatal(err)
	}
	d := &device{t: f.t, f: f, web: web, pub: pub, priv: priv, id: devices.DeviceID(pub), label: label}
	d.ctx, d.cancel = context.WithCancel(context.Background())
	f.t.Cleanup(d.cancel)
	return d
}

func (d *device) rc() relaypb.RelayClient {
	if d.web {
		return relaytest.WebClient(d.t, d.f.relay)
	}
	return relaytest.NativeClient(d.t, d.f.relay)
}

// open admits, opens a route (a fresh session id unless one is given) and
// completes the E2EE handshake, then starts the receive loop.
func (d *device) open(session string) error {
	rc := d.rc()
	actx, cancel := context.WithTimeout(d.ctx, fixtureTimeout)
	defer cancel()
	adm, err := client.Admit(actx, rc, d.f.relay.HostID, "test-device", d.f.relay.Secret)
	if err != nil {
		return fmt.Errorf("admit: %w", err)
	}
	if session == "" {
		d.nextID++
		session = fmt.Sprintf("sess-%s-%d", d.id[:6], time.Now().UnixNano())
	}
	d.session = session
	d.eph, err = e2ee.GenerateKeyPair()
	if err != nil {
		return err
	}
	d.dc, err = client.OpenData(d.ctx, rc, adm, d.f.relay.HostID, session, d.id, d.eph, e2ee.RoleMobile,
		client.WithExpectedPeerKey(d.f.kp.Public()))
	if err != nil {
		return fmt.Errorf("open data: %w", err)
	}
	hctx, hcancel := context.WithTimeout(d.ctx, fixtureTimeout)
	defer hcancel()
	if err := d.dc.Handshake(hctx); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	d.msgs = make(chan []byte, 64)
	d.rerr = make(chan error, 1)
	dc := d.dc
	go func() {
		for {
			m, err := dc.Receive()
			if err != nil {
				d.rerr <- err
				close(d.msgs)
				return
			}
			d.msgs <- m
		}
	}()
	return nil
}

type rpcResp struct {
	Result json.RawMessage
	Err    *rpcErr
}

type rpcErr struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// request sends one request and waits for its response (events are skipped).
func (d *device) request(method string, params any) (rpcResp, error) {
	d.nextID++
	id := fmt.Sprintf("r%d", d.nextID)
	body := map[string]any{"id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, _ := json.Marshal(body)
	if err := d.dc.Send(raw); err != nil {
		return rpcResp{}, err
	}
	return d.await(id)
}

func (d *device) await(id string) (rpcResp, error) {
	deadline := time.After(fixtureTimeout)
	for {
		select {
		case m, ok := <-d.msgs:
			if !ok {
				return rpcResp{}, <-d.rerrOr()
			}
			var env struct {
				ID     string          `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *rpcErr         `json:"error"`
			}
			if json.Unmarshal(m, &env) != nil || env.ID != id || (env.Result == nil && env.Error == nil) {
				continue
			}
			return rpcResp{Result: env.Result, Err: env.Error}, nil
		case <-deadline:
			return rpcResp{}, errors.New("timed out waiting for response")
		}
	}
}

func (d *device) rerrOr() chan error {
	out := make(chan error, 1)
	select {
	case e := <-d.rerr:
		out <- e
	default:
		out <- errors.New("stream ended")
	}
	return out
}

// hello sends device_hello and returns the daemon's answer.
func (d *device) hello() (rpcResp, error) {
	h := devices.SignHello(d.priv, d.label, d.f.kp.Public(), d.eph.Public())
	return d.request(devices.HelloMethod, h)
}

// connect = open + hello, requiring approval.
func (d *device) connect() {
	d.t.Helper()
	if err := d.open(""); err != nil {
		d.t.Fatalf("device %s open: %v", d.label, err)
	}
	r, err := d.hello()
	if err != nil {
		d.t.Fatalf("device %s hello: %v", d.label, err)
	}
	if r.Err != nil {
		d.t.Fatalf("device %s hello refused: %+v", d.label, r.Err)
	}
}

func (d *device) listWorkspaces() ([]json.RawMessage, error) {
	r, err := d.request("workspace.list", nil)
	if err != nil {
		return nil, err
	}
	if r.Err != nil {
		return nil, fmt.Errorf("workspace.list: %s", r.Err.Message)
	}
	var out []json.RawMessage
	if err := json.Unmarshal(r.Result, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *device) mustList() []json.RawMessage {
	d.t.Helper()
	out, err := d.listWorkspaces()
	if err != nil {
		d.t.Fatalf("device %s: %v", d.label, err)
	}
	return out
}

func relaytestMaxDevices(n int) relaytest.Option { return relaytest.WithMaxDevicesPerHost(n) }
