package link

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/devices"
	relaypairing "github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/wsclient"
)

// relayStepTimeout bounds each step of opening a relayed link (admit, data
// stream, E2EE handshake, challenge, hello), on top of the caller's context.
const relayStepTimeout = 15 * time.Second

// RelayRouteFromOffer is the stored route for a relay pairing offer.
func RelayRouteFromOffer(o relaypairing.Offer) *RelayRoute {
	return &RelayRoute{
		Endpoint:    o.Relay,
		RelayHostID: o.WorkspaceID,
		Fingerprint: o.RelayFingerprint,
		DaemonID:    o.DaemonID,
		DaemonPub:   base64.RawURLEncoding.EncodeToString(o.PublicKey),
	}
}

// RelaySecretHex is the offer's relay host secret in the form Entry stores.
func RelaySecretHex(o relaypairing.Offer) string { return hex.EncodeToString(o.Secret) }

// DialRelay opens an RPC connection to a peer behind a self-hosted relay
// (ADR-0048). The relay is pinned by fingerprint and the daemon by its E2EE
// public key: a different relay certificate or daemon key fails closed before
// anything is sent. The host then proves its identity with a signed
// device_hello (the peer's relay hub decides whether it is approved). Errors
// never carry the relay secret or the password.
func DialRelay(ctx context.Context, t Transport, self *identity.Key, secret Entry, label string) (Conn, error) {
	if t.Kind != KindRelay || t.Relay == nil {
		return nil, fmt.Errorf("link: transport kind %q is not a relay route", t.Kind)
	}
	r := t.Relay
	if r.Fingerprint == "" {
		return nil, errors.New("link: relay route has no pinned fingerprint")
	}
	secretBytes, err := hex.DecodeString(secret.RelaySecret)
	if err != nil || len(secretBytes) == 0 {
		return nil, errors.New("link: relay secret is missing or not hex")
	}
	daemonPub, err := base64.RawURLEncoding.DecodeString(r.DaemonPub)
	if err != nil || len(daemonPub) != 32 {
		return nil, errors.New("link: relay daemon key is not a 32-byte key")
	}
	target := strings.TrimPrefix(strings.TrimPrefix(r.Endpoint, "https://"), "http://")
	target = strings.TrimSuffix(target, "/")

	cc, err := client.Dial(target, r.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("link: relay dial: %w", err)
	}
	rc := relaypb.NewRelayClient(cc)
	sctx, cancel := context.WithCancel(context.Background())
	fail := func(err error) (Conn, error) {
		cancel()
		_ = cc.Close()
		return nil, err
	}
	// The stream outlives ctx (ctx only bounds the opening), so it runs on its
	// own context that the Conn's Close cancels; ctx still aborts the opening.
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	step := func(f func(context.Context) error) error {
		c, done := context.WithTimeout(sctx, relayStepTimeout)
		defer done()
		return f(c)
	}
	var adm string
	if err := step(func(c context.Context) (err error) {
		adm, err = client.Admit(c, rc, r.RelayHostID, "fleet-"+self.HostID()[:8], secretBytes)
		return err
	}); err != nil {
		return fail(fmt.Errorf("link: relay admit: %w", err))
	}
	var sid [8]byte
	if _, err := rand.Read(sid[:]); err != nil {
		return fail(err)
	}
	session := "fleet-" + hex.EncodeToString(sid[:])
	dc, err := client.OpenClientData(sctx, rc, adm, r.RelayHostID, session, self.HostID(), client.WithExpectedPeerKey(daemonPub))
	if err != nil {
		return fail(fmt.Errorf("link: relay open: %w", err))
	}
	closeAll := func() {
		cancel() // unblocks a pending Receive, which Close would otherwise wait on
		_ = dc.Close()
		_ = cc.Close()
	}
	if err := step(dc.Handshake); err != nil {
		closeAll()
		return nil, fmt.Errorf("link: relay handshake: %w", err)
	}

	challenge, err := relayChallenge(sctx, dc)
	if err != nil {
		closeAll()
		return nil, err
	}
	h := devices.Hello{
		V:         devices.HelloVersion,
		DevicePub: self.Public(),
		Label:     label,
		Signature: self.Sign(devices.HelloSignedBytes(daemonPub, dc.TranscriptHash(), []byte(session), challenge)),
		Password:  secret.Password,
	}
	if err := relayHello(sctx, dc, h); err != nil {
		closeAll()
		return nil, err
	}
	c := wsclient.NewFromTransport(relayTransport{dc: dc, closeFn: closeAll})
	return c, nil
}

// relayTransport is the data connection with a Close that also tears down the
// stream context and the grpc connection.
type relayTransport struct {
	dc      *client.DataConn
	closeFn func()
}

func (r relayTransport) Receive() ([]byte, error) { return r.dc.Receive() }
func (r relayTransport) Send(b []byte) error      { return r.dc.Send(b) }
func (r relayTransport) Close() error             { r.closeFn(); return nil }

// recvResult is one Receive outcome.
type recvResult struct {
	msg []byte
	err error
}

// recvCtx reads one message, giving up when ctx ends. A Receive that is still
// pending then is released when the caller closes the connection.
func recvCtx(ctx context.Context, dc *client.DataConn) ([]byte, error) {
	ch := make(chan recvResult, 1)
	go func() {
		m, err := dc.Receive()
		ch <- recvResult{m, err}
	}()
	select {
	case r := <-ch:
		return r.msg, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func relayChallenge(ctx context.Context, dc *client.DataConn) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, relayStepTimeout)
	defer cancel()
	for {
		m, err := recvCtx(ctx, dc)
		if err != nil {
			return nil, fmt.Errorf("link: relay challenge: %w", err)
		}
		ch, ok, err := devices.ParseChallengeMessage(m)
		if !ok {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("link: relay challenge: %w", err)
		}
		return ch, nil
	}
}

// relayHello sends the device_hello request and waits for its answer; a
// refusal becomes a *RefusedError.
func relayHello(ctx context.Context, dc *client.DataConn, h devices.Hello) error {
	ctx, cancel := context.WithTimeout(ctx, relayStepTimeout)
	defer cancel()
	const id = "1"
	raw, err := json.Marshal(map[string]any{"id": id, "method": devices.HelloMethod, "params": h})
	if err != nil {
		return errors.New("link: encode relay hello")
	}
	if err := dc.Send(raw); err != nil {
		return fmt.Errorf("link: relay hello: %w", err)
	}
	for {
		m, err := recvCtx(ctx, dc)
		if err != nil {
			return fmt.Errorf("link: relay hello: %w", err)
		}
		var resp struct {
			ID    string `json:"id"`
			Error *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(m, &resp) != nil || resp.ID != id || (resp.Error == nil && resp.Result == nil) {
			continue
		}
		if resp.Error != nil {
			return &RefusedError{Code: resp.Error.Code, Msg: resp.Error.Message}
		}
		return nil
	}
}
