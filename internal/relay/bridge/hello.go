package bridge

// hello.go — who is on the other end of a freshly handshaken route
// (ADR-0031 D9 "Device identity", decision 35). The E2EE handshake gives each
// session an ephemeral key; the device proves it holds its persistent
// Ed25519 key by signing, inside the encrypted channel, a message bound to
// this very session (internal/relay/devices.Hello), so the hello cannot be
// replayed into another session. The device id in the route must equal the
// hash of that key, so a client cannot claim another's id.
//
// Wire form: the hello is an ordinary request of the wsapi protocol
// ({"id","method":"relay.device_hello","params":Hello}) answered by an
// ordinary response, so a daemon from before this change answers "method not
// found" and a client can tell the two apart.

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/relay/devices"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// Error codes of a refused hello (the response's error.code).
const (
	codeAwaitingApproval = "awaiting_approval"
	codeRevoked          = "revoked"
	codeBadHello         = "bad_hello"
	codeHelloVersion     = "hello_version_unsupported"
	codeTooManyDevices   = "too_many_devices"
	codeTooManyPending   = "too_many_pending"
	codeHelloRequired    = "device_hello_required"
	codeUnsupported      = "device_hello_unsupported"
	codeInternal         = "internal"
)

func decodeHex(s string) ([]byte, error) { return hex.DecodeString(s) }

type rpcEnvelope struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// helloResult is the success response's result.
type helloResult struct {
	V        int    `json:"v"`
	DeviceID string `json:"device_id"`
	State    string `json:"state"`
}

func (h *Hub) respondResult(t *deviceTransport, id string, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return t.Send(mustJSON(map[string]any{"id": id, "result": json.RawMessage(raw)}))
}

func (h *Hub) respondError(t *deviceTransport, id, code, msg string) error {
	return t.Send(mustJSON(map[string]any{"id": id, "error": map[string]string{"message": msg, "code": code}}))
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // maps of strings and RawMessage always marshal
	}
	return b
}

// identify reads the device's first message and decides who it is: a valid
// device_hello (checked against the registry), or — while legacy devices are
// allowed — a client that predates device identity. It returns errSessionOver
// when the device is refused (the caller drops the session).
func (h *Hub) identify(s *session, r *sessionReader, t *deviceTransport) error {
	// On a device route the daemon speaks first: a fresh challenge the device
	// must sign into its hello. The session keys depend only on the daemon's
	// long-lived key and the client's ephemeral key, so a captured client
	// transcript replays into an identical session — only something the daemon
	// chooses afresh per session proves the hello was made for THIS one. (The
	// fixed single-device route, which pre-identity clients use, gets no
	// challenge: they ignore unknown events but need not be sent one.)
	var challenge []byte
	if h.opts.Registry != nil && s.ref.device != DefaultDeviceID {
		c, err := devices.NewChallenge()
		if err != nil {
			h.logf("relay bridge: challenge: %v", err)
			return errSessionOver
		}
		if err := t.Send(devices.ChallengeMessage(c)); err != nil {
			return err
		}
		challenge = c
	}
	first, timedOut, err := h.firstMessage(r)
	if err != nil {
		return err
	}
	if !timedOut {
		var env rpcEnvelope
		if json.Unmarshal(first, &env) == nil && env.Method == devices.HelloMethod && env.ID != "" {
			return h.admitHello(s, r, t, env, challenge)
		}
	}
	// No hello: a legacy client (or one speaking nonsense). Legacy service is
	// only for the fixed single-device route a pre-identity client uses: a
	// client on any other route id is a new-style client and must identify
	// itself (otherwise skipping the hello would dodge revocation and the
	// pending state).
	var env rpcEnvelope
	if !timedOut {
		_ = json.Unmarshal(first, &env)
	}
	if !h.opts.LegacyDevices || s.ref.device != DefaultDeviceID {
		if !timedOut {
			_ = h.respondError(t, env.ID, codeHelloRequired, "this daemon requires device identity (device_hello); update the app")
		}
		h.refuseGrace(r)
		return errSessionOver
	}
	// One legacy device at a time (the old protocol had exactly one): the
	// newest replaces any earlier legacy session, such as an orphan left by a
	// daemon restart.
	h.mu.Lock()
	var older []*session
	for _, o := range h.sessions {
		if o != s && o.legacy {
			older = append(older, o)
		}
	}
	h.mu.Unlock()
	for _, o := range older {
		h.drop(o)
	}
	h.mu.Lock()
	if h.admittedCountLocked() >= h.maxDevs {
		h.mu.Unlock()
		h.refuse(t, r, env.ID, codeTooManyDevices, "too many connected devices")
		return errSessionOver
	}
	s.legacy, s.admitted = true, true
	h.mu.Unlock()
	h.logf("relay bridge: WARNING: serving a client without device identity as a legacy device because relay.legacyDevices is true — it is served without approval and cannot be revoked; update the desktop/mobile app and set relay.legacyDevices: false")
	if !timedOut {
		t.push(first)
	}
	return nil
}

// firstMessage waits for the device's first message for the hello timeout.
func (h *Hub) firstMessage(r *sessionReader) (msg []byte, timedOut bool, err error) {
	ch, release := h.clock.At(h.clock.Now().Add(h.hello))
	defer release()
	select {
	case m, ok := <-r.msgs:
		if !ok {
			if e := r.err(); e != nil {
				return nil, false, e
			}
			return nil, false, errReaderEnded
		}
		return m, false, nil
	case <-ch:
		return nil, true, nil
	}
}

func (h *Hub) admitHello(s *session, r *sessionReader, t *deviceTransport, env rpcEnvelope, challenge []byte) error {
	var hello devices.Hello
	if err := json.Unmarshal(env.Params, &hello); err != nil {
		h.refuse(t, r, env.ID, codeBadHello, "malformed device_hello")
		return errSessionOver
	}
	if h.opts.Registry == nil {
		h.refuse(t, r, env.ID, codeUnsupported, "this daemon does not track relay devices")
		return errSessionOver
	}
	if hello.V != devices.HelloVersion {
		// v1 signed nothing the daemon contributed and could be replayed from a
		// captured session; it is retired, and no released client spoke it.
		h.logf("relay bridge: device_hello version %d refused (need %d)", hello.V, devices.HelloVersion)
		h.refuse(t, r, env.ID, codeHelloVersion, fmt.Sprintf("device_hello version %d is not accepted; update the app (need version %d)", hello.V, devices.HelloVersion))
		return errSessionOver
	}
	if challenge == nil {
		// No challenge was issued on this route (the fixed single-device
		// route): an identity hello cannot be fresh here.
		h.refuse(t, r, env.ID, codeBadHello, "device_hello is not accepted on this route")
		return errSessionOver
	}
	pub, err := hello.Verify(s.ref.device, h.kp.Public(), s.dc.PeerPublicKey(), []byte(s.ref.session), challenge)
	if err != nil {
		// Reason deliberately coarse; the cause is logged without key material.
		h.logf("relay bridge: device_hello rejected: %v", errorKind(err))
		h.refuse(t, r, env.ID, codeBadHello, "device_hello rejected")
		return errSessionOver
	}
	// The same device identifying on a new session (it reconnected with a
	// fresh session id after a daemon restart, say) replaces its old session.
	h.supersede(s, s.ref.device)
	// Room first: a device refused for lack of room must not be recorded
	// (and approved, burning a pairing slot) as a side effect.
	h.mu.Lock()
	full := h.admittedCountLocked() >= h.maxDevs
	h.mu.Unlock()
	if full {
		h.refuse(t, r, env.ID, codeTooManyDevices, "too many connected devices")
		return errSessionOver
	}
	dec, err := h.opts.Registry.Admit(s.ref.device, pub, hello.Label)
	if errors.Is(err, devices.ErrTooManyPending) {
		h.refuse(t, r, env.ID, codeTooManyPending, "too many devices are awaiting approval on this daemon")
		return errSessionOver
	}
	if err != nil {
		h.logf("relay bridge: device registry: %v", err)
		h.refuse(t, r, env.ID, codeInternal, "device registry unavailable")
		return errSessionOver
	}
	if !dec.Approved {
		msg := "awaiting approval: run `smind relay devices approve " + s.ref.device + "` on the daemon"
		if dec.Reason == devices.ReasonRevoked {
			msg = "this device was revoked"
		}
		h.refuse(t, r, env.ID, string(dec.Reason), msg)
		return errSessionOver
	}
	h.mu.Lock()
	if h.admittedCountLocked() >= h.maxDevs {
		h.mu.Unlock()
		h.refuse(t, r, env.ID, codeTooManyDevices, "too many connected devices")
		return errSessionOver
	}
	s.deviceID, s.label, s.admitted = dec.Device.ID, dec.Device.Label, true
	h.mu.Unlock()
	// A revoke that landed between Admit and now could not see this session
	// (its deviceID was not set yet): look again, so a revoked device is never
	// served.
	if d, ok := h.opts.Registry.Get(dec.Device.ID); !ok || d.State != devices.StateApproved {
		h.refuse(t, r, env.ID, codeRevoked, "this device was revoked")
		return errSessionOver
	}
	if err := h.respondResult(t, env.ID, helloResult{V: devices.HelloVersion, DeviceID: dec.Device.ID, State: string(dec.Device.State)}); err != nil {
		return err
	}
	return nil
}

// supersede drops every other session of deviceID (same device, older
// session): a device is served on one session at a time.
func (h *Hub) supersede(keep *session, deviceID string) {
	h.mu.Lock()
	var old []*session
	for _, o := range h.sessions {
		if o != keep && o.deviceID == deviceID {
			old = append(old, o)
		}
	}
	h.mu.Unlock()
	for _, o := range old {
		h.drop(o)
	}
}

// errorKind names a Verify failure without echoing key material.
func errorKind(err error) string {
	switch {
	case errors.Is(err, devices.ErrBadSignature):
		return "bad signature"
	case errors.Is(err, devices.ErrIDMismatch):
		return "device id does not match key"
	case errors.Is(err, devices.ErrBadVersion):
		return "unsupported version"
	case errors.Is(err, devices.ErrBadKey):
		return "bad key"
	case errors.Is(err, devices.ErrBadLabel):
		return "bad label"
	default:
		return "invalid"
	}
}

// refuse answers the device's request with an error, then gives the answer a
// moment to reach it before the session is cancelled (cancelling the stream
// immediately could discard the unsent response).
func (h *Hub) refuse(t *deviceTransport, r *sessionReader, id, code, msg string) {
	_ = h.respondError(t, id, code, msg)
	h.refuseGrace(r)
}

func (h *Hub) refuseGrace(r *sessionReader) {
	ch, release := h.clock.At(h.clock.Now().Add(refuseFlushGrace))
	defer release()
	select {
	case <-r.done:
	case <-ch:
	}
}

// refuseFlushGrace is how long a refused device gets to read its refusal (it
// normally hangs up at once, ending the wait early).
const refuseFlushGrace = time.Second

// --- admin surface (wsapi relay.device.* and the CLI) ---

// Devices lists every registered device with its connection state.
func (h *Hub) Devices() []devices.Info {
	if h.opts.Registry == nil {
		return nil
	}
	list := h.opts.Registry.List()
	h.mu.Lock()
	connected := make(map[string]bool)
	for _, s := range h.sessions {
		if s.serving && s.deviceID != "" {
			connected[s.deviceID] = true
		}
	}
	h.mu.Unlock()
	out := make([]devices.Info, len(list))
	for i, d := range list {
		out[i] = devices.Info{Device: d, Connected: connected[d.ID]}
	}
	return out
}

// ResolveDevice expands a unique id prefix to a full device id.
func (h *Hub) ResolveDevice(prefix string) (string, error) {
	if h.opts.Registry == nil {
		return "", devices.ErrNotFound
	}
	return h.opts.Registry.Resolve(prefix)
}

func (h *Hub) ApproveDevice(id string) (devices.Device, error) {
	if h.opts.Registry == nil {
		return devices.Device{}, devices.ErrNotFound
	}
	return h.opts.Registry.Approve(id)
}

func (h *Hub) RenameDevice(id, label string) (devices.Device, error) {
	if h.opts.Registry == nil {
		return devices.Device{}, devices.ErrNotFound
	}
	return h.opts.Registry.Rename(id, label)
}

// RevokeDevice marks the device revoked, ends its live sessions immediately
// and tells the relay to close its routes and refuse it (best effort: the
// registry is authoritative and a reconnect is refused at hello anyway).
func (h *Hub) RevokeDevice(id string) (devices.Device, error) {
	if h.opts.Registry == nil {
		return devices.Device{}, devices.ErrNotFound
	}
	d, err := h.opts.Registry.Revoke(id)
	if err != nil {
		return devices.Device{}, err
	}
	h.mu.Lock()
	var victims []*session
	for _, s := range h.sessions {
		if s.deviceID == id {
			victims = append(victims, s)
		}
	}
	c := h.conn
	h.mu.Unlock()
	for _, s := range victims {
		h.drop(s)
	}
	h.sendRevoke(c, id)
	return d, nil
}

// OpenPairingWindow opens the auto-approve window (`smind relay offer`).
func (h *Hub) OpenPairingWindow(d time.Duration, maxNew int) error {
	if h.opts.Registry == nil {
		return fmt.Errorf("relay devices are not enabled on this daemon")
	}
	return h.opts.Registry.OpenWindow(d, maxNew)
}

// PairingWindow reports the open window, if any.
func (h *Hub) PairingWindow() (devices.Window, bool) {
	if h.opts.Registry == nil {
		return devices.Window{}, false
	}
	return h.opts.Registry.Window()
}

// sendRevoke asks the relay to close a device's routes; only on a connection
// whose relay is known to speak Revoke (it sent a Sync).
func (h *Hub) sendRevoke(c *relayConn, deviceID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	multi := c.multi
	c.mu.Unlock()
	if !multi {
		return
	}
	if err := c.sendControl(&relaypb.ControlFrame{
		WorkspaceId: h.cfg.WorkspaceID,
		Body:        &relaypb.ControlFrame_Revoke_{Revoke: &relaypb.ControlFrame_Revoke{DeviceId: deviceID}},
	}); err != nil {
		h.logf("relay bridge: revoke: %v", err)
	}
}

// resendRevocations re-announces every revoked device to the relay, which
// forgets revocations when a control stream ends.
func (h *Hub) resendRevocations(c *relayConn) {
	if h.opts.Registry == nil {
		return
	}
	for _, d := range h.opts.Registry.List() {
		if d.State == devices.StateRevoked {
			h.sendRevoke(c, d.ID)
		}
	}
}
