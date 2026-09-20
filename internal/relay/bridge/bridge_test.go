package bridge

// bridge_test.go is the plan's Item 1 integration test
// (docs/plans/active/mobile-app-milestone-1.md): a real in-process relay
// (server.Run, exactly as internal/relay/client's own integration tests use
// it) plus a real wsapi.API bridged in via bridge.Run, then a fake "mobile"
// client.OpenData(role=Mobile) completing the E2EE handshake and getting a
// real workspace.list response back through the bridge -- proving a
// mobile-originated RPC reaches the same dispatch table a browser tab's
// WebSocket connection would.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// startTestRelay starts a real relay (server.Run) on an ephemeral port and
// enrolls one workspace, returning everything bridge.Config needs.
func startTestRelay(t *testing.T, workspaceID string) (cfg Config, stop func()) {
	t.Helper()
	dir := t.TempDir()
	secret, err := server.EnrollWorkspace(dir, workspaceID)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()

	cert, err := server.LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	fingerprint := server.CertFingerprint(cert)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, server.Config{Listener: lis, DataDir: dir}) }()

	stop = func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("relay Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("relay did not stop")
		}
	}
	return Config{
		RelayAddress: addr,
		WorkspaceID:  workspaceID,
		SecretHex:    hex.EncodeToString(secret),
		Fingerprint:  fingerprint,
	}, stop
}

// newTestAPI builds a real wsapi.API backed by a fresh store/workspace
// manager, exactly like cmd/smind's daemon does, so ServeTransport (as
// bridge.Run calls it) dispatches against real handlers.
func newTestAPI(t *testing.T) *wsapi.API {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	acctDB, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatalf("store.Open(accounts): %v", err)
	}
	t.Cleanup(func() { _ = acctDB.Close() })

	wm := workspace.New(db)
	api, err := wsapi.New(wm, accounts.New(acctDB), nil, db, "test-token")
	if err != nil {
		t.Fatalf("wsapi.New: %v", err)
	}
	return api
}

// dialDeviceDataConn dials the relay as an independent "mobile" client:
// its own admission (over the shared workspace secret), then OpenData on
// the same (workspace, DefaultSessionID, DefaultDeviceID) route the daemon
// bridge uses, so the two land on the same relay-side route.
func dialDeviceDataConn(t *testing.T, ctx context.Context, cfg Config, deviceKP *e2ee.KeyPair) *client.DataConn {
	t.Helper()
	conn, err := client.Dial(cfg.RelayAddress, cfg.Fingerprint)
	if err != nil {
		t.Fatalf("device dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	rc := relaypb.NewRelayClient(conn)

	secret, err := hex.DecodeString(cfg.SecretHex)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	admissionID, err := client.Admit(ctx, rc, cfg.WorkspaceID, "test-mobile-device", secret)
	if err != nil {
		t.Fatalf("device admit: %v", err)
	}
	dc, err := client.OpenData(ctx, rc, admissionID, cfg.WorkspaceID, DefaultSessionID, DefaultDeviceID, deviceKP, e2ee.RoleMobile)
	if err != nil {
		t.Fatalf("device OpenData: %v", err)
	}
	return dc
}

func TestIntegration_BridgeServesWorkspaceListToMobileRole(t *testing.T) {
	const workspaceID = "ws-bridge-int"
	cfg, stopRelay := startTestRelay(t, workspaceID)
	defer stopRelay()

	api := newTestAPI(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	daemonKP, err := e2ee.LoadOrCreateKeyPair(t.TempDir())
	if err != nil {
		t.Fatalf("daemon keypair: %v", err)
	}
	go func() {
		if err := Run(ctx, cfg, daemonKP, api); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	deviceKP, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatalf("device keypair: %v", err)
	}

	admitCtx, admitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer admitCancel()
	device := dialDeviceDataConn(t, admitCtx, cfg, deviceKP)

	hsCtx, hsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer hsCancel()
	if err := device.Handshake(hsCtx); err != nil {
		t.Fatalf("device handshake: %v", err)
	}

	req, err := json.Marshal(map[string]any{"id": "1", "method": "workspace.list"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := device.Send(req); err != nil {
		t.Fatalf("device send: %v", err)
	}

	respCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		data, err := device.Receive()
		if err != nil {
			errCh <- err
			return
		}
		respCh <- data
	}()

	select {
	case data := <-respCh:
		var env struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("unmarshal response: %v (raw %s)", err, data)
		}
		if env.ID != "1" {
			t.Fatalf("response id = %q, want %q", env.ID, "1")
		}
		if env.Error != nil {
			t.Fatalf("workspace.list error = %v", env.Error.Message)
		}
		if got := string(env.Result); got != "[]" {
			t.Fatalf("workspace.list result = %s, want []", got)
		}
	case err := <-errCh:
		t.Fatalf("device receive: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for workspace.list response through the bridge")
	}
}

// tcpProxy is a transparent byte-forwarding TCP proxy used only to give a
// test a way to sever the DAEMON's transport to the relay independently of
// any other connection (the device's, in particular) -- since the daemon
// dials cfg.RelayAddress in its own background goroutine with no handle the
// test can reach directly. It forwards raw bytes both ways, so the TLS
// handshake (and cert, for fingerprint pinning) is exactly the real
// relay's; the proxy itself is not a TLS endpoint.
type tcpProxy struct {
	ln     net.Listener
	target string

	mu    sync.Mutex
	conns []net.Conn
}

func newTCPProxy(t *testing.T, target string) *tcpProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &tcpProxy{ln: ln, target: target}
	go p.acceptLoop()
	t.Cleanup(func() { ln.Close() })
	return p
}

func (p *tcpProxy) addr() string { return p.ln.Addr().String() }

func (p *tcpProxy) acceptLoop() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *tcpProxy) handle(c net.Conn) {
	upstream, err := net.Dial("tcp", p.target)
	if err != nil {
		c.Close()
		return
	}
	p.mu.Lock()
	p.conns = append(p.conns, c)
	p.mu.Unlock()
	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, c); done <- struct{}{} }()
	go func() { io.Copy(c, upstream); done <- struct{}{} }()
	<-done
	upstream.Close()
	c.Close()
}

// killAll severs every connection this proxy has forwarded so far, without
// stopping the proxy itself or touching the real relay -- the model of a
// network drop on the daemon's side only.
func (p *tcpProxy) killAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = nil
}

// TestIntegration_BridgeReconnectsAfterDaemonTransportDrop proves Item 1's
// "a relay connection dropping and reconnecting resumes cleanly... without
// requiring smind serve to restart" acceptance criterion: only the
// DAEMON's transport to the relay is severed (via tcpProxy, below) --
// the relay process and the mobile device's own connection are untouched
// -- and the daemon's bridge (internal to bridge.Run, running in the same
// process as the whole test) must reconnect and resume the session on its
// own, still answering the same device's next workspace.list call.
func TestIntegration_BridgeReconnectsAfterDaemonTransportDrop(t *testing.T) {
	const workspaceID = "ws-bridge-reconnect"
	relayCfg, stopRelay := startTestRelay(t, workspaceID)
	defer stopRelay()

	proxy := newTCPProxy(t, relayCfg.RelayAddress)
	daemonCfg := relayCfg
	daemonCfg.RelayAddress = proxy.addr() // the daemon dials the relay only through the proxy.

	api := newTestAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	daemonKP, err := e2ee.LoadOrCreateKeyPair(t.TempDir())
	if err != nil {
		t.Fatalf("daemon keypair: %v", err)
	}
	go func() {
		if err := Run(ctx, daemonCfg, daemonKP, api); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	deviceKP, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatalf("device keypair: %v", err)
	}
	device := dialDeviceDataConn(t, ctx, relayCfg, deviceKP)
	hsCtx, hsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer hsCancel()
	if err := device.Handshake(hsCtx); err != nil {
		t.Fatalf("device handshake: %v", err)
	}

	requestWorkspaceList(t, device, "1")

	// Sever only the daemon's transport. The relay process and the
	// device's own connection are untouched.
	proxy.killAll()

	// The daemon's bridge should reconnect (fresh dial+admit through the
	// still-open proxy) and Resume the existing E2EE session on its own,
	// with no restart of anything in this test process. Poll with the
	// same request until it succeeds or the deadline passes, since the
	// bridge's reconnect has its own internal backoff.
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := tryWorkspaceList(device, "2"); err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		return // success
	}
	t.Fatalf("bridge did not resume after daemon transport drop: %v", lastErr)
}

// requestWorkspaceList sends a workspace.list request over device and
// fails the test unless it gets back an empty-array success response.
func requestWorkspaceList(t *testing.T, device *client.DataConn, id string) {
	t.Helper()
	if err := tryWorkspaceList(device, id); err != nil {
		t.Fatal(err)
	}
}

// tryWorkspaceList sends and awaits one workspace.list round-trip, for use
// both in the single-shot check and the post-reconnect poll loop above.
func tryWorkspaceList(device *client.DataConn, id string) error {
	req, err := json.Marshal(map[string]any{"id": id, "method": "workspace.list"})
	if err != nil {
		return err
	}
	if err := device.Send(req); err != nil {
		return err
	}
	respCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		data, err := device.Receive()
		if err != nil {
			errCh <- err
			return
		}
		respCh <- data
	}()
	select {
	case data := <-respCh:
		var env struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			return fmt.Errorf("unmarshal response: %w (raw %s)", err, data)
		}
		if env.ID != id {
			return fmt.Errorf("response id = %q, want %q", env.ID, id)
		}
		if env.Error != nil {
			return fmt.Errorf("workspace.list error = %v", env.Error.Message)
		}
		if got := string(env.Result); got != "[]" {
			return fmt.Errorf("workspace.list result = %s, want []", got)
		}
		return nil
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timed out waiting for workspace.list response")
	}
}
