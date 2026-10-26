// Command devharness is a throwaway test helper, not part of smind's own CLI:
// it runs a real relay, a real multi-device daemon hub (real device registry,
// relay.legacyDevices=false) and Go "desktop" peers, controlled over stdin, so
// mobile/'s cross-language integration test can drive the real TypeScript
// client through ADR-0031 D9 (pairing window, pending/approve/revoke, relay and
// daemon restarts) end to end.
//
// Protocol: stdout line 1 "READY <pairing url>", line 2 "NATIVE <addr>". Then
// one command per line on stdin, one JSON line answering each on stdout:
// {"cmd":"<name>","ok":true,...} or {"cmd":"<name>","ok":false,"error":"..."}.
// Commands: list | approve <id|peer label> | revoke <id|peer label> | window <maxNew> [seconds] |
// peer-add <label> | peer-call <label> | peer-drop <label> | relay-restart |
// daemon-restart | quit. With -fake-agent also:
// seed-run <stopReason|end_turn> [title] | seed-live <scenario> [title] |
// set-scenario <taskId> <text...> | set-task-status <taskId> <status> | stop-run <runId> |
// queue-prompt <taskId> <queue|interrupt> <prompt...> |
// queue-list <chatId> | run-status <runId> |
// seed-tree <nested|permission|ratelimited|failed|big> (TEST ONLY: a fresh task tree
// under the seeded workspace; replies {rootId, nodes:{<name>:<taskId>}, ...}) |
// plugin-install <dir> | plugin-link <dir> | plugin-enable <id> | plugin-disable <id> | plugin-reload <id> | plugin-update <id> <dir>
// (TEST ONLY: plugin_cmds.go; needs SMIND_HOME with plugins.enabled: true) |
// peer-rpc <label> <method> [json params] (raw result of one daemon RPC). Diagnostics go to stderr, prefixed "devharness: ".
package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/pairing"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/server"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

const (
	harnessHostID = "ws-dev-harness"
	callTimeout   = 10 * time.Second
	stopTimeout   = 15 * time.Second
)

func main() {
	windowMax := flag.Int("window-max", 0, "open a 10 minute pairing window for this many new devices at start (0 = closed)")
	password := flag.String("password", "", "require this daemon password in every device_hello (ADR-0031 P13c); empty = none")
	outdated := flag.Bool("outdated-daemon", false, "run a daemon from before device_hello v2 instead of the hub: it completes the E2EE handshake on every device route and then says nothing (no device challenge)")
	fakeAgent := flag.String("fake-agent", "", "path of the compiled internal/taskrunner/fakeagent binary; enables the seed-run command")
	flag.Parse()
	if err := run(*windowMax, *outdated, *password, *fakeAgent); err != nil {
		fmt.Fprintln(os.Stderr, "devharness:", err)
		os.Exit(1)
	}
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "devharness: "+format+"\n", args...)
}

// relayProc is the running relay: its addresses, cert and enrolment survive a
// restart, its in-memory state does not.
type relayProc struct {
	dir             string
	nativeAddr, web string
	cancel          context.CancelFunc
	done            chan error
}

func (r *relayProc) start(nativeLis, webLis net.Listener) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	cfg := server.Config{DataDir: r.dir, Listener: nativeLis, GRPCWebListener: webLis}
	go func() { done <- server.Run(ctx, cfg) }()
	r.cancel, r.done = cancel, done
}

func (r *relayProc) stop() error {
	if r.cancel == nil {
		return nil
	}
	r.cancel()
	r.cancel = nil
	select {
	case err := <-r.done:
		return err
	case <-time.After(stopTimeout):
		return errors.New("relay did not stop")
	}
}

func (r *relayProc) restart() error {
	if err := r.stop(); err != nil {
		return err
	}
	var nativeLis, webLis net.Listener
	deadline := time.Now().Add(stopTimeout)
	for nativeLis == nil || webLis == nil {
		var err error
		if nativeLis == nil {
			if nativeLis, err = net.Listen("tcp", r.nativeAddr); err != nil {
				nativeLis = nil
			}
		}
		if webLis == nil {
			if webLis, err = net.Listen("tcp", r.web); err != nil {
				webLis = nil
			}
		}
		if nativeLis != nil && webLis != nil {
			break
		}
		if time.Now().After(deadline) {
			if nativeLis != nil {
				_ = nativeLis.Close()
			}
			if webLis != nil {
				_ = webLis.Close()
			}
			return errors.New("relay ports not rebindable")
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.start(nativeLis, webLis)
	return nil
}

// harness is the whole process state; commands run one at a time.
type harness struct {
	relay     *relayProc
	secret    []byte
	fp        string
	cfg       bridge.Config
	daemonKP  *e2ee.KeyPair
	reg       *devices.Registry
	api       *wsapi.API
	wm        *workspace.Manager
	runner    *taskrunner.Runner
	store     *store.Store
	fakeAgent string
	workDir   string
	plugins   *plugins.Manager // see plugin_cmds.go
	seedWS    *store.Workspace // the workspace seed-run puts its tasks in

	hub       *bridge.Hub
	hubCancel context.CancelFunc
	hubDone   chan struct{}
	peers     map[string]*peer
	peerKeys  map[string]ed25519.PrivateKey
	password  string // the daemon password devices must carry; "" = none
}

func run(windowMax int, outdated bool, password, fakeAgent string) error {
	relayDir, err := os.MkdirTemp("", "smind-devharness-relay-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(relayDir)
	workDir, err := os.MkdirTemp("", "smind-devharness-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	secret, err := server.EnrollHost(relayDir, harnessHostID)
	if err != nil {
		return err
	}
	// Create the cert BEFORE server.Run so the fingerprint we print is the one
	// Run serves (see the old harness / client integration_test startRelay).
	cert, err := server.LoadOrCreateCert(relayDir)
	if err != nil {
		return err
	}
	nativeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	webLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = nativeLis.Close()
		return err
	}
	h := &harness{
		password:  password,
		workDir:   workDir,
		fakeAgent: fakeAgent,
		secret:    secret,
		fp:        server.CertFingerprint(cert),
		relay:     &relayProc{dir: relayDir, nativeAddr: nativeLis.Addr().String(), web: webLis.Addr().String()},
		peers:     map[string]*peer{},
		peerKeys:  map[string]ed25519.PrivateKey{},
	}
	h.relay.start(nativeLis, webLis)
	defer func() { _ = h.relay.stop() }()

	db, err := store.Open(filepath.Join(workDir, "smind.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	h.store = db
	acctDB, err := store.Open(filepath.Join(workDir, "accounts.db"))
	if err != nil {
		return err
	}
	defer acctDB.Close()
	h.wm = workspace.New(db)
	if fakeAgent != "" {
		h.runner = taskrunner.New(h.wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeAgent}))
	}
	h.api, err = wsapi.New(h.wm, accounts.New(acctDB), h.runner, db, "devharness-token")
	if err != nil {
		return err
	}
	// plugin.updated on a flip of plugins.enabled in $SMIND_HOME/config.yaml (the daemon does the same).
	stopWatch := h.api.WatchPlugins(context.Background())
	defer stopWatch()
	h.api.SetAttachmentStore(attach.NewStore(filepath.Join(workDir, "attachments"))) // prompt attachments on, as cmd/smind wires it
	if h.daemonKP, err = e2ee.GenerateKeyPair(); err != nil {
		return err
	}
	if h.reg, err = devices.Open(filepath.Join(workDir, "relay-devices.json")); err != nil {
		return err
	}
	h.cfg = bridge.Config{
		RelayAddress: h.relay.nativeAddr,
		WorkspaceID:  harnessHostID,
		SecretHex:    hex.EncodeToString(secret),
		Fingerprint:  h.fp,
	}
	if outdated {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go outdatedDaemon(ctx, h)
	} else {
		h.startHub()
		defer h.stopHub()
		defer h.closePeers()
	}

	if windowMax > 0 && !outdated {
		if err := h.hub.OpenPairingWindow(10*time.Minute, windowMax); err != nil {
			return err
		}
	}

	offer := pairing.Offer{
		DaemonID:         bridge.DaemonKeyID(h.daemonKP),
		PublicKey:        h.daemonKP.Public(),
		Relay:            "https://" + h.relay.web,
		RelayFingerprint: h.fp,
		Secret:           secret,
		WorkspaceID:      harnessHostID,
	}
	url, err := offer.URL("")
	if err != nil {
		return err
	}
	fmt.Println("READY " + url)
	fmt.Println("NATIVE " + h.relay.nativeAddr)

	if outdated {
		// No hub: only quit works; the commands that need it answer an error.
		h.hub = bridge.NewHub(bridge.Options{Registry: h.reg})
	}
	return h.loop(os.Stdin, os.Stdout)
}

// outdatedDaemon stands in for a daemon from before device_hello v2 (ADR-0031
// D9): it learns of device routes from the relay's control stream, opens the
// daemon side of each, completes the E2EE handshake as the responder — and then
// says nothing, as such a daemon does until it receives a hello. A v2 client
// must give up on it (no challenge), never sending a hello of its own.
func outdatedDaemon(ctx context.Context, h *harness) {
	conn, err := client.Dial(h.relay.nativeAddr, h.fp)
	if err != nil {
		logf("outdated daemon: dial: %v", err)
		return
	}
	defer conn.Close()
	rc := relaypb.NewRelayClient(conn)
	adm, err := client.Admit(ctx, rc, harnessHostID, bridge.DaemonKeyID(h.daemonKP), h.secret)
	if err != nil {
		logf("outdated daemon: admit: %v", err)
		return
	}
	ctl, err := client.OpenControl(ctx, rc, adm, harnessHostID)
	if err != nil {
		logf("outdated daemon: control: %v", err)
		return
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	serve := func(device string, session []byte) {
		key := device + "|" + string(session)
		mu.Lock()
		dup := seen[key]
		seen[key] = true
		mu.Unlock()
		if dup {
			return
		}
		go func() {
			dc, err := client.OpenDaemonData(ctx, rc, adm, harnessHostID, string(session), device, h.daemonKP)
			if err != nil {
				return
			}
			hctx, hcancel := context.WithTimeout(ctx, stopTimeout)
			defer hcancel()
			if dc.Handshake(hctx) != nil {
				return
			}
			logf("outdated daemon: handshake done on a device route; staying silent")
			for { // read and ignore whatever the client sends
				if _, err := dc.Receive(); err != nil {
					return
				}
			}
		}()
	}
	for {
		cf, err := ctl.Recv()
		if err != nil {
			return
		}
		switch b := cf.GetBody().(type) {
		case *relaypb.ControlFrame_Sync_:
			for _, d := range b.Sync.GetDevices() {
				serve(d.GetDeviceId(), d.GetSessionId())
			}
		case *relaypb.ControlFrame_DeviceAttach_:
			serve(b.DeviceAttach.GetDeviceId(), b.DeviceAttach.GetSessionId())
		}
	}
}

func (h *harness) startHub() {
	h.hub = bridge.NewHub(bridge.Options{Registry: h.reg, LegacyDevices: false, Logf: logf, Password: bridge.StaticPassword(h.password)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	hub := h.hub
	go func() {
		defer close(done)
		if err := hub.Run(ctx, h.cfg, h.daemonKP, h.api); err != nil {
			logf("hub run: %v", err)
		}
	}()
	h.hubCancel, h.hubDone = cancel, done
}

func (h *harness) stopHub() {
	if h.hubCancel == nil {
		return
	}
	h.hubCancel()
	h.hubCancel = nil
	select {
	case <-h.hubDone:
	case <-time.After(stopTimeout):
		logf("hub did not stop")
	}
}

func (h *harness) closePeers() {
	for _, p := range h.peers {
		p.close()
	}
}

func (h *harness) loop(in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		cmd, args := fields[0], fields[1:]
		if cmd == "quit" {
			return enc.Encode(map[string]any{"cmd": cmd, "ok": true})
		}
		res, err := h.dispatch(cmd, args)
		if err != nil {
			res = map[string]any{"ok": false, "error": err.Error()}
		} else if _, set := res["ok"]; !set {
			res["ok"] = true
		}
		res["cmd"] = cmd
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
	return sc.Err()
}

func need(args []string, n int, usage string) error {
	if len(args) < n {
		return errors.New("usage: " + usage)
	}
	return nil
}

func (h *harness) dispatch(cmd string, args []string) (map[string]any, error) {
	switch cmd {
	case "list":
		list := []map[string]any{}
		for _, d := range h.hub.Devices() {
			list = append(list, map[string]any{"id": d.ID, "label": d.Label, "state": string(d.State), "connected": d.Connected})
		}
		return map[string]any{"devices": list}, nil
	case "approve", "revoke":
		if err := need(args, 1, cmd+" <id-or-prefix>"); err != nil {
			return nil, err
		}
		ref := args[0]
		if k := h.peerKeys[ref]; k != nil { // a peer label stands for its device id
			ref = devices.DeviceID(k.Public().(ed25519.PublicKey))
		}
		id, err := h.hub.ResolveDevice(ref)
		if err != nil {
			return nil, err
		}
		var d devices.Device
		if cmd == "approve" {
			d, err = h.hub.ApproveDevice(id)
		} else {
			d, err = h.hub.RevokeDevice(id)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": d.ID, "state": string(d.State)}, nil
	case "window":
		if err := need(args, 1, "window <maxNew> [seconds]"); err != nil {
			return nil, err
		}
		var maxNew int
		if _, err := fmt.Sscanf(args[0], "%d", &maxNew); err != nil {
			return nil, fmt.Errorf("bad maxNew %q", args[0])
		}
		d := 10 * time.Minute
		if len(args) > 1 {
			var secs int
			if _, err := fmt.Sscanf(args[1], "%d", &secs); err != nil || secs <= 0 {
				return nil, fmt.Errorf("bad seconds %q", args[1])
			}
			d = time.Duration(secs) * time.Second
		}
		if err := h.hub.OpenPairingWindow(d, maxNew); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	case "peer-add":
		if err := need(args, 1, "peer-add <label>"); err != nil {
			return nil, err
		}
		return h.peerAdd(args[0])
	case "peer-call":
		if err := need(args, 1, "peer-call <label>"); err != nil {
			return nil, err
		}
		p := h.peers[args[0]]
		if p == nil {
			return nil, fmt.Errorf("no peer %q", args[0])
		}
		r, err := p.request("workspace.list", nil, callTimeout)
		if err != nil {
			return nil, err
		}
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message)
		}
		var list []json.RawMessage
		if err := json.Unmarshal(r.Result, &list); err != nil {
			return nil, err
		}
		return map[string]any{"result_len": len(list)}, nil
	case "peer-drop":
		if err := need(args, 1, "peer-drop <label>"); err != nil {
			return nil, err
		}
		p := h.peers[args[0]]
		if p == nil {
			return nil, fmt.Errorf("no peer %q", args[0])
		}
		p.close()
		return map[string]any{}, nil
	case "relay-restart":
		if err := h.relay.restart(); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	case "seed-run":
		return h.seedRun(args)
	case "seed-live":
		return h.seedLive(args)
	case "seed-tree":
		return h.seedTree(args)
	case "peer-rpc":
		return h.peerRPC(args)
	case "set-scenario":
		return h.setScenario(args)
	case "set-task-status":
		return h.setTaskStatus(args)
	case "stop-run":
		return h.stopRun(args)
	case "queue-prompt":
		return h.queuePrompt(args)
	case "queue-list":
		return h.queueList(args)
	case "run-status":
		return h.runStatus(args)
	case "plugin-install", "plugin-link", "plugin-enable", "plugin-disable", "plugin-reload", "plugin-update":
		return h.pluginCmd(cmd, args)
	case "daemon-restart":
		// A new Hub loses every E2EE counter; waiting clients must re-handshake
		// on a fresh session id. Registry and daemon key are kept.
		h.stopHub()
		h.startHub()
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("unknown command %q", cmd)
	}
}

// --- peers ---

type rpcErr struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

type rpcResp struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcErr         `json:"error"`
}

// peer is a Go native-gRPC client behaving like a P13 desktop device.
type peer struct {
	label  string
	priv   ed25519.PrivateKey
	cancel context.CancelFunc
	conn   *grpc.ClientConn
	dc     *client.DataConn

	mu      sync.Mutex
	nextID  int
	waiters map[string]chan rpcResp
	ended   chan struct{}
	endErr  error
	once    sync.Once

	// challenge delivers the daemon's device_hello v2 challenge, the first
	// message of a device route (ADR-0031 D9).
	challenge chan []byte
}

func (p *peer) close() {
	p.once.Do(func() {
		p.cancel()
		if p.conn != nil {
			_ = p.conn.Close()
		}
	})
}

func (h *harness) peerAdd(label string) (map[string]any, error) {
	if old := h.peers[label]; old != nil {
		old.close()
		delete(h.peers, label)
	}
	priv := h.peerKeys[label]
	if priv == nil {
		_, k, err := devices.GenerateKey()
		if err != nil {
			return nil, err
		}
		priv = k
		h.peerKeys[label] = k
	}
	pub := priv.Public().(ed25519.PublicKey)
	deviceID := devices.DeviceID(pub)

	ctx, cancel := context.WithCancel(context.Background())
	p := &peer{label: label, priv: priv, cancel: cancel, waiters: map[string]chan rpcResp{}, ended: make(chan struct{}), challenge: make(chan []byte, 1)}
	fail := func(err error) (map[string]any, error) {
		p.close()
		return nil, err
	}
	conn, err := client.Dial(h.relay.nativeAddr, h.fp)
	if err != nil {
		return fail(err)
	}
	p.conn = conn
	rc := relaypb.NewRelayClient(conn)
	actx, acancel := context.WithTimeout(ctx, callTimeout)
	defer acancel()
	adm, err := client.Admit(actx, rc, harnessHostID, "peer-"+label, h.secret)
	if err != nil {
		return fail(fmt.Errorf("admit: %w", err))
	}
	session := fmt.Sprintf("peer-%s-%d", deviceID[:6], time.Now().UnixNano())
	dc, err := client.OpenClientData(ctx, rc, adm, harnessHostID, session, deviceID,
		client.WithExpectedPeerKey(h.daemonKP.Public()), client.WithKeepalive(client.KeepaliveConfig{}))
	if err != nil {
		return fail(fmt.Errorf("open data: %w", err))
	}
	p.dc = dc
	hctx, hcancel := context.WithTimeout(ctx, callTimeout)
	defer hcancel()
	if err := dc.Handshake(hctx); err != nil {
		// After a revoke the relay itself refuses the device's route.
		if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
			p.close()
			return map[string]any{"id": deviceID, "state": "revoked"}, nil
		}
		return fail(fmt.Errorf("handshake: %w", err))
	}
	go p.pump()
	h.peers[label] = p

	// The daemon speaks first on a device route: sign ITS challenge, for THIS session.
	var challenge []byte
	select {
	case challenge = <-p.challenge:
	case <-time.After(callTimeout):
		return fail(errors.New("no device challenge from the daemon"))
	}
	hello := devices.SignHello(priv, label, h.daemonKP.Public(), dc.TranscriptHash(), []byte(session), challenge)
	r, err := p.request(devices.HelloMethod, hello, callTimeout)
	if err != nil {
		return fail(err)
	}
	if r.Error != nil {
		return map[string]any{"id": deviceID, "state": r.Error.Code}, nil
	}
	var res struct {
		DeviceID string `json:"device_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		return fail(err)
	}
	return map[string]any{"id": res.DeviceID, "state": res.State}, nil
}

func (p *peer) pump() {
	defer close(p.ended)
	for {
		m, err := p.dc.Receive()
		if err != nil {
			p.mu.Lock()
			p.endErr = err
			p.mu.Unlock()
			return
		}
		if c, ok, err := devices.ParseChallengeMessage(m); ok {
			if err == nil {
				select {
				case p.challenge <- c:
				default:
				}
			}
			continue
		}
		var r rpcResp
		if json.Unmarshal(m, &r) != nil || r.ID == "" || (r.Result == nil && r.Error == nil) {
			continue // event or something else
		}
		p.mu.Lock()
		w := p.waiters[r.ID]
		delete(p.waiters, r.ID)
		p.mu.Unlock()
		if w != nil {
			w <- r
		}
	}
}

func (p *peer) request(method string, params any, timeout time.Duration) (rpcResp, error) {
	p.mu.Lock()
	p.nextID++
	id := fmt.Sprintf("r%d", p.nextID)
	w := make(chan rpcResp, 1)
	p.waiters[id] = w
	p.mu.Unlock()
	body := map[string]any{"id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, _ := json.Marshal(body)
	if err := p.dc.Send(raw); err != nil {
		return rpcResp{}, err
	}
	select {
	case r := <-w:
		return r, nil
	case <-p.ended:
		p.mu.Lock()
		err := p.endErr
		p.mu.Unlock()
		return rpcResp{}, fmt.Errorf("stream ended: %v", err)
	case <-time.After(timeout):
		return rpcResp{}, errors.New("timed out waiting for response")
	}
}

// newSeedTask creates (once) the shared seeded workspace and a task in it;
// scenario, when non-empty, selects the fake agent's behaviour via a
// "scenario" file in the task worktree.
func (h *harness) newSeedTask(title, scenario string, opts ...workspace.CreateTaskOption) (store.Workspace, store.Task, error) {
	// One seeded workspace for every seed-run (the app lists the first workspace's tasks): created on first use.
	if h.seedWS == nil {
		repo, err := os.MkdirTemp(h.workDir, "seed-repo-")
		if err != nil {
			return store.Workspace{}, store.Task{}, err
		}
		git := func(gitArgs []string) error {
			cmd := exec.Command("git", gitArgs...)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git %v: %v: %s", gitArgs, err, out)
			}
			return nil
		}
		for _, g := range [][]string{
			{"init"},
			{"config", "core.autocrlf", "false"},
			{"config", "user.email", "devharness@example.com"},
			{"config", "user.name", "devharness"},
		} {
			if err := git(g); err != nil {
				return store.Workspace{}, store.Task{}, err
			}
		}
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seeded\n"), 0o644); err != nil {
			return store.Workspace{}, store.Task{}, err
		}
		if err := git([]string{"add", "README.md"}); err != nil {
			return store.Workspace{}, store.Task{}, err
		}
		if err := git([]string{"commit", "-m", "initial commit"}); err != nil {
			return store.Workspace{}, store.Task{}, err
		}
		created, err := h.wm.CreateWorkspace(repo, "Seeded", "hard", nil)
		if err != nil {
			return store.Workspace{}, store.Task{}, err
		}
		h.seedWS = &created
	}
	ws := *h.seedWS
	task, err := h.wm.CreateTask(ws.ID, nil, title, opts...)
	if err != nil {
		return store.Workspace{}, store.Task{}, err
	}
	if scenario != "" {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			return store.Workspace{}, store.Task{}, err
		}
	}
	return ws, task, nil
}

// seedRun creates a workspace, a task and a real run executed by the fake ACP
// agent (D13): with a scenario "stop <reason>" the run ends done at a cap and
// reports Disposition needs-continue; without one it ends end_turn normally.
func (h *harness) seedRun(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("seed-run needs -fake-agent")
	}
	if err := need(args, 1, "seed-run <stopReason|end_turn> [title]"); err != nil {
		return nil, err
	}
	reason := args[0]
	title := "Seeded task"
	if len(args) > 1 {
		title = strings.Join(args[1:], " ")
	}
	scenario := ""
	if reason != "end_turn" {
		scenario = "stop " + reason
	}
	ws, task, err := h.newSeedTask(title, scenario)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	runID, err := h.api.Runs.Start(ctx, h.wm, h.runner, task.ID, 0, taskrunner.ProviderGLM, "seed", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, st, err := h.api.Runs.History(runID)
		if err != nil {
			return nil, err
		}
		if st.Status == runs.StatusDone {
			return map[string]any{
				"workspaceId": ws.ID,
				"taskId":      task.ID,
				"runId":       runID,
				"status":      "done",
				"stopReason":  st.StopReason,
				"disposition": st.Disposition,
			}, nil
		}
		if st.Status == runs.StatusError {
			return nil, fmt.Errorf("seeded run failed: %s", st.Err)
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for the seeded run to finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// seedLive seeds a task and starts a run that stays LIVE (e.g. scenario
// "hang"); it answers once the run is running, not when it finishes.
func (h *harness) seedLive(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("seed-live needs -fake-agent")
	}
	if err := need(args, 1, "seed-live <scenario> [title]"); err != nil {
		return nil, err
	}
	title := "Live task"
	if len(args) > 1 {
		title = strings.Join(args[1:], " ")
	}
	ws, task, err := h.newSeedTask(title, args[0])
	if err != nil {
		return nil, err
	}
	runID, err := h.api.Runs.Start(context.Background(), h.wm, h.runner, task.ID, 0, taskrunner.ProviderGLM, "seed", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		hist, st, err := h.api.Runs.History(runID)
		if err != nil {
			return nil, err
		}
		if st.Status == runs.StatusError {
			return nil, fmt.Errorf("live run failed: %s", st.Err)
		}
		// "running" holds from the moment the run is registered, before the
		// agent process has read its scenario file. A caller that then
		// rewrites the scenario (set-scenario) or stops the run races the
		// agent's start, so for the scenarios that stream a first chunk
		// wait until that chunk is in the history: the agent is then inside
		// the scenario and a later set-scenario cannot change this run.
		if st.Status == runs.StatusRunning && (!streamsFirst(args[0]) || hasTextEvent(hist)) {
			break
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for the live run to start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	chat, err := h.wm.DefaultChat(task.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"workspaceId": ws.ID,
		"taskId":      task.ID,
		"chatId":      chat.ID,
		"runId":       runID,
		"status":      "running",
	}, nil
}

// streamsFirst reports whether the fake agent's scenario sends a text chunk
// before it blocks (hang, hold and slow do; permission scenarios ask first).
func streamsFirst(scenario string) bool {
	switch scenario {
	case "hang", "hold", "slow":
		return true
	}
	return false
}

func hasTextEvent(events []runs.Event) bool {
	for _, e := range events {
		if e.Type == taskrunner.EventTypeText {
			return true
		}
	}
	return false
}

// setScenario overwrites the fake agent's scenario file in a task worktree.
func (h *harness) setScenario(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("set-scenario needs -fake-agent")
	}
	if err := need(args, 2, "set-scenario <taskId> <text...>"); err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad taskId %q", args[0])
	}
	task, err := h.wm.GetTask(id)
	if err != nil {
		return nil, err
	}
	if err := h.writeScenario(task, strings.Join(args[1:], " ")); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// writeScenario overwrites the fake agent's scenario file in a task worktree.
func (h *harness) writeScenario(task store.Task, scenario string) error {
	return os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644)
}

// setTaskStatus force-writes a task's status column (created|running|done|
// error|stopped), bypassing the run lifecycle, so the mobile smoke can show
// the task list in every state. It does not start or stop any run.
func (h *harness) setTaskStatus(args []string) (map[string]any, error) {
	if err := need(args, 2, "set-task-status <taskId> <status>"); err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad taskId %q", args[0])
	}
	switch args[1] {
	case "created", "running", "done", "error", "stopped":
	default:
		return nil, fmt.Errorf("bad status %q (want created|running|done|error|stopped)", args[1])
	}
	task, err := h.store.UpdateTaskStatus(id, args[1])
	if err != nil {
		return nil, err
	}
	return map[string]any{"taskId": task.ID, "status": task.Status}, nil
}

func (h *harness) stopRun(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("stop-run needs -fake-agent")
	}
	if err := need(args, 1, "stop-run <runId>"); err != nil {
		return nil, err
	}
	if err := h.api.Runs.Stop(args[0]); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// queuePrompt enqueues a prompt on a busy chat (ADR-0021): queue waits for
// the live run to end, interrupt also stops it.
func (h *harness) queuePrompt(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("queue-prompt needs -fake-agent")
	}
	if err := need(args, 3, "queue-prompt <taskId> <queue|interrupt> <prompt...>"); err != nil {
		return nil, err
	}
	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad taskId %q", args[0])
	}
	var whenBusy runs.WhenBusy
	switch args[1] {
	case "queue":
		whenBusy = runs.WhenBusyQueue
	case "interrupt":
		whenBusy = runs.WhenBusyInterrupt
	default:
		return nil, fmt.Errorf("bad whenBusy %q", args[1])
	}
	out, err := h.api.Runs.StartWhenBusy(context.Background(), h.wm, h.runner, taskID, 0, taskrunner.ProviderGLM, strings.Join(args[2:], " "), taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, whenBusy, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		return nil, err
	}
	return map[string]any{"queued": out.Queued, "queueItemId": out.QueueItemID, "runId": out.RunID}, nil
}

func (h *harness) queueList(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("queue-list needs -fake-agent")
	}
	if err := need(args, 1, "queue-list <chatId>"); err != nil {
		return nil, err
	}
	chatID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad chatId %q", args[0])
	}
	items, err := h.api.Runs.ListChatQueue(chatID)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(items))
	for _, it := range items {
		runID := ""
		if it.RunID != nil {
			runID = *it.RunID
		}
		list = append(list, map[string]any{"id": it.ID, "status": it.Status, "priority": it.Priority, "prompt": it.Prompt, "runId": runID, "cancelKind": it.CancelKind})
	}
	return map[string]any{"items": list}, nil
}

func (h *harness) runStatus(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("run-status needs -fake-agent")
	}
	if err := need(args, 1, "run-status <runId>"); err != nil {
		return nil, err
	}
	events, st, err := h.api.Runs.History(args[0])
	if err != nil {
		return nil, err
	}
	texts := 0
	for _, e := range events {
		if e.Type == taskrunner.EventTypeText {
			texts++
		}
	}
	return map[string]any{"status": st.Status, "stopReason": st.StopReason, "disposition": st.Disposition, "textEvents": texts}, nil
}
