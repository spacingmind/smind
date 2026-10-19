package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/relaytest"
)

const (
	mobileRetry   = 500 * time.Millisecond
	mobilePoll    = 400 * time.Millisecond
	mobileIOBound = 15 * time.Second
	// mobileRPCBound is how long a round trip may take before the stub
	// concludes the daemon is gone (a restarted daemon never answers the old
	// session) and reconnects on a fresh one.
	mobileRPCBound = 4 * time.Second
	mobileLabel    = "harness-mobile"
	mobileAdmitTag = "test-device"
)

// mobileStub plays a P13-style mobile device over grpc-web: persistent device
// key, fresh session id and ephemeral E2EE key per connection, device_hello,
// then workspace.list round trips.
type mobileStub struct {
	web, fingerprint, workspace string
	secret                      []byte
	daemon                      []byte
	out                         *lineWriter
	d                           *daemon

	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	id   string

	okCount  int
	lastCode string // last refusal code printed in the current streak
}

// prepare generates the device key and opens a pairing window for one new
// device so the stub is approved without the test's help.
func (m *mobileStub) prepare() error {
	var err error
	if m.pub, m.priv, err = devices.GenerateKey(); err != nil {
		return err
	}
	m.id = devices.DeviceID(m.pub)
	return m.d.current().OpenPairingWindow(10*time.Minute, 1)
}

func (m *mobileStub) run(ctx context.Context) {
	seq := 0
	for ctx.Err() == nil {
		seq++
		err := m.connect(ctx, fmt.Sprintf("mobile-%s-%d-%d", m.id[:6], seq, time.Now().UnixNano()))
		if ctx.Err() != nil {
			return
		}
		var ref *refusal
		if err != nil && (status.Code(err) == codes.PermissionDenied || strings.Contains(err.Error(), "PermissionDenied")) {
			// The relay itself refuses a revoked device before the handshake.
			ref = &refusal{code: "revoked"}
			err = ref
		}
		switch {
		case errors.As(err, &ref):
			// A refusal ends the daemon-side session; retry on a new one. Say
			// each distinct code once per streak, not every retry.
			if ref.code != m.lastCode {
				m.lastCode = ref.code
				m.out.printf("MOBILE refused %s", ref.code)
			}
		case err != nil:
			m.out.printf("MOBILE error %s", oneLine(err.Error()))
			m.out.println("MOBILE reconnecting")
		default:
			m.out.println("MOBILE reconnecting")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(mobileRetry):
		}
	}
}

type refusal struct{ code string }

func (r *refusal) Error() string { return "refused: " + r.code }

// connect runs one session until it fails; nil means the stream ended.
func (m *mobileStub) connect(ctx context.Context, session string) error {
	rc, closeRC, err := relaytest.DialWeb(m.web, m.fingerprint)
	if err != nil {
		return err
	}
	defer closeRC()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	actx, acancel := context.WithTimeout(sctx, mobileIOBound)
	adm, err := client.Admit(actx, rc, m.workspace, mobileAdmitTag, m.secret)
	acancel()
	if err != nil {
		return fmt.Errorf("admit: %w", err)
	}
	dc, err := client.OpenClientData(sctx, rc, adm, m.workspace, session, m.id,
		client.WithExpectedPeerKey(m.daemon))
	if err != nil {
		return fmt.Errorf("open data: %w", err)
	}
	// DataConn.Close takes the lock a pending Receive holds, so the stream
	// context must be cancelled (which unblocks Receive) BEFORE Close runs —
	// a plain `defer dc.Close()` runs first (LIFO) and can deadlock the stub.
	defer func() {
		cancel()
		_ = dc.Close()
	}()
	hctx, hcancel := context.WithTimeout(sctx, mobileIOBound)
	err = dc.Handshake(hctx)
	hcancel()
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	c := &rpcConn{cancel: cancel, dc: dc, msgs: make(chan []byte, 64), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for {
			msg, err := dc.Receive()
			if err != nil {
				c.err = err
				return
			}
			select {
			case c.msgs <- msg:
			case <-sctx.Done():
				return
			}
		}
	}()

	challenge, err := c.awaitChallenge(sctx)
	if err != nil {
		return err
	}
	h := devices.SignHello(m.priv, mobileLabel, m.daemon, dc.TranscriptHash(), []byte(session), challenge)
	resp, err := c.request(sctx, devices.HelloMethod, h)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return &refusal{code: resp.Error.Code}
	}
	m.lastCode = ""

	for {
		resp, err := c.request(sctx, "workspace.list", nil)
		if err != nil {
			return err
		}
		if resp.Error != nil {
			m.out.printf("MOBILE error %s", oneLine(resp.Error.Message))
		} else {
			m.okCount++
			m.out.printf("MOBILE ok %d", m.okCount)
		}
		select {
		case <-sctx.Done():
			return nil
		case <-time.After(mobilePoll):
		}
	}
}

type rpcConn struct {
	cancel context.CancelFunc // ends the whole session; the watchdog's lever
	dc     *client.DataConn
	msgs   chan []byte
	done   chan struct{}
	err    error // valid once done is closed
	nextID int
}

type rpcResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// awaitChallenge waits for the daemon's per-session device challenge, which
// it sends first on a device route (hello v2).
func (c *rpcConn) awaitChallenge(ctx context.Context) ([]byte, error) {
	timeout := time.After(mobileRPCBound)
	for {
		select {
		case m := <-c.msgs:
			ch, ok, err := devices.ParseChallengeMessage(m)
			if !ok {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("challenge: %w", err)
			}
			return ch, nil
		case <-c.done:
			if c.err != nil {
				return nil, c.err
			}
			return nil, errors.New("stream ended")
		case <-timeout:
			return nil, errors.New("timed out waiting for the device challenge")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// request sends one request and waits for its response, skipping events.
func (c *rpcConn) request(ctx context.Context, method string, params any) (rpcResponse, error) {
	c.nextID++
	id := fmt.Sprintf("r%d", c.nextID)
	body := map[string]any{"id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return rpcResponse{}, err
	}
	// One watchdog over send + wait: a send into a dead route can block too.
	if c.cancel != nil {
		wd := time.AfterFunc(mobileRPCBound, c.cancel)
		defer wd.Stop()
	}
	if err := c.dc.Send(raw); err != nil {
		return rpcResponse{}, err
	}
	timeout := time.After(mobileRPCBound)
	for {
		select {
		case m := <-c.msgs:
			var r rpcResponse
			if json.Unmarshal(m, &r) != nil || r.ID != id || (r.Result == nil && r.Error == nil) {
				continue
			}
			return r, nil
		case <-c.done:
			if c.err != nil {
				return rpcResponse{}, c.err
			}
			return rpcResponse{}, errors.New("stream ended")
		case <-timeout:
			return rpcResponse{}, errors.New("timed out waiting for response")
		case <-ctx.Done():
			return rpcResponse{}, ctx.Err()
		}
	}
}
