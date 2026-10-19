package e2ee

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the E2EE wire protocol version (ADR-0035). Both ends
// must match; there is no negotiation and no older version is implemented.
const ProtocolVersion = 2

// maxFrameLen bounds a single frame so a malformed or hostile length prefix
// cannot make the reader allocate arbitrary memory.
const maxFrameLen = 1 << 20

// maxHandshakeFrameLen bounds a frame (type byte plus payload) while a
// handshake is in progress: the largest legitimate one is M2 (1+66 bytes), so
// an unauthenticated peer cannot make either end allocate more than this.
const maxHandshakeFrameLen = 128

// Frame types.
const (
	frameHello byte = 0x01
	frameReady byte = 0x02
	frameData  byte = 0x03
)

// Handshake payload sizes (ADR-0035 §1): M1 is version ‖ role ‖ client_eph,
// M2 is version ‖ role ‖ daemon_pub ‖ daemon_eph; ready is an HMAC-SHA256.
const (
	clientHelloLen = 1 + 1 + PublicKeySize
	daemonHelloLen = 1 + 1 + PublicKeySize + PublicKeySize
	readyLen       = 32
)

// Role identifies which end of the session a channel is. The two ends must
// differ: the role selects which directional key is used for sending.
type Role uint8

// Session roles. The daemon is the responder; every client (desktop, mobile,
// test clients) is the initiator, carried by RoleMobile.
const (
	RoleDaemon Role = 1
	RoleMobile Role = 2
)

func (r Role) String() string {
	switch r {
	case RoleDaemon:
		return "daemon"
	case RoleMobile:
		return "mobile"
	default:
		return fmt.Sprintf("role(%d)", uint8(r))
	}
}

// Channel errors.
var (
	// ErrProtocol marks any wire-level protocol violation: a malformed or
	// truncated frame, an unexpected frame type, an empty ready, or a role
	// mismatch.
	ErrProtocol = errors.New("e2ee: protocol violation")
	// ErrKeyRotation marks a second handshake carrying a *different* hello
	// than the one the session was established with. Rotation means a brand
	// new session, never an in-place rekey (ADR-0007 (e)), so this closes the
	// channel instead of re-deriving keys.
	ErrKeyRotation = errors.New("e2ee: peer re-handshaked with a different key")
	// ErrPeerKeyMismatch marks a handshake whose daemon hello carried a
	// static public key other than the one pinned with WithExpectedPeerKey —
	// the pairing-offer pin. It is checked before any key agreement. The
	// channel is closed; no session is derived.
	ErrPeerKeyMismatch = errors.New("e2ee: peer public key does not match the pinned key")
	// ErrHandshakeConfirm marks a failed key confirmation: the peer's ready
	// MAC did not verify, so the two ends did not derive the same keys (a
	// tampered ephemeral, a wrong static key, a forged peer).
	ErrHandshakeConfirm = errors.New("e2ee: handshake key confirmation failed")
	// ErrKeyAgreement marks an X25519 exchange that produced the all-zero
	// output (a low-order peer point, RFC 7748 §6.1).
	ErrKeyAgreement = errors.New("e2ee: X25519 key agreement failed")
	// ErrUnsupportedVersion is matched (errors.Is) by every
	// *UnsupportedVersionError: the peer's hello carried another protocol
	// version.
	ErrUnsupportedVersion = errors.New("e2ee: unsupported protocol version")
	// ErrNotEstablished is returned when application traffic is attempted
	// before the handshake completes.
	ErrNotEstablished = errors.New("e2ee: handshake not completed")
	// ErrAlreadyEstablished is returned when Handshake is called twice.
	ErrAlreadyEstablished = errors.New("e2ee: handshake already completed")
	// ErrHandshakeInProgress is returned when Handshake is called while
	// another Handshake call on the same channel is still running.
	ErrHandshakeInProgress = errors.New("e2ee: handshake already in progress")
	// ErrClosed is returned once the channel has been closed.
	ErrClosed = errors.New("e2ee: channel closed")
)

// UnsupportedVersionError is returned when a peer's hello carries a protocol
// version other than ProtocolVersion. It exposes the peer's version so the
// caller can tell "update the daemon" (Peer < ProtocolVersion) from "update
// this app" (Peer > ProtocolVersion). It matches ErrUnsupportedVersion.
type UnsupportedVersionError struct {
	Peer byte
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("e2ee: peer speaks protocol version %d, this end speaks %d", e.Peer, ProtocolVersion)
}

// Is makes errors.Is(err, ErrUnsupportedVersion) match.
func (e *UnsupportedVersionError) Is(target error) bool { return target == ErrUnsupportedVersion }

// Channel is one end of an E2EE session carried over an arbitrary byte
// stream — in production, a relay-forwarded connection; in tests, an
// in-process pipe. The relay that sits between two Channels only ever sees
// the framed ciphertext.
type Channel struct {
	conn io.ReadWriteCloser
	kp   *KeyPair // the daemon's static key; nil for a client
	role Role

	// newEphemeral produces this handshake's ephemeral X25519 key. It is
	// always a fresh random key in production; in-package tests replace it
	// with a seeded source so vectors are reproducible. No exported API can
	// set it.
	newEphemeral func() (*ecdh.PrivateKey, error)

	reader *bufio.Reader

	writeMu sync.Mutex // serialises frame writes
	readMu  sync.Mutex // serialises frame reads

	mu          sync.Mutex // guards the fields below
	session     *Session
	established bool
	closed      bool
	handshaking bool // a Handshake call is running

	daemonPub, daemonEphPub, clientEphPub []byte
	th                                    []byte
	// Retained for ADR-0035 §4 retries on an established session: the peer's
	// hello and ready we must tolerate a repeat of, and (daemon) the replies
	// we re-send for a repeated M1.
	peerHello, peerReady []byte
	replyHello           []byte
	replyReady           []byte
}

func generateEphemeral() (*ecdh.PrivateKey, error) {
	return ecdh.X25519().GenerateKey(rand.Reader)
}

// NewDaemonChannel wraps conn as the responder end. kp is the daemon's
// persisted static keypair; the daemon draws a fresh ephemeral key in every
// Handshake call.
func NewDaemonChannel(conn io.ReadWriteCloser, kp *KeyPair) (*Channel, error) {
	if conn == nil {
		return nil, errors.New("e2ee: nil connection")
	}
	if kp == nil {
		return nil, errors.New("e2ee: nil keypair")
	}
	return &Channel{conn: conn, kp: kp, role: RoleDaemon, newEphemeral: generateEphemeral, reader: bufio.NewReader(conn)}, nil
}

// NewClientChannel wraps conn as the initiator end. There is deliberately no
// way to supply the client's key: Handshake generates a fresh ephemeral, so a
// caller can never reuse one across sessions (ADR-0035 §3).
func NewClientChannel(conn io.ReadWriteCloser) (*Channel, error) {
	if conn == nil {
		return nil, errors.New("e2ee: nil connection")
	}
	return &Channel{conn: conn, role: RoleMobile, newEphemeral: generateEphemeral, reader: bufio.NewReader(conn)}, nil
}

// HandshakeOption configures a single Handshake call.
type HandshakeOption func(*handshakeConfig)

type handshakeConfig struct {
	pinned          bool
	expectedPeerKey []byte
}

// WithExpectedPeerKey pins the daemon's static X25519 public key: if the
// daemon's hello carries any other key, a client Handshake fails with
// ErrPeerKeyMismatch (and closes the channel) before any key agreement or
// further frame.
//
// This is how a device that holds a pairing offer refuses to complete a
// handshake with anything but the daemon the offer named, even if the
// relay/admission layer let the connection through (ADR-0007). The pin fails
// closed: pinning an empty or malformed key can never match, so a missing
// offer key cannot silently turn into "no pin" — omit the option to run
// unpinned. The option has no effect on a daemon channel.
func WithExpectedPeerKey(pub []byte) HandshakeOption {
	return func(c *handshakeConfig) {
		c.pinned = true
		c.expectedPeerKey = bytes.Clone(pub)
	}
}

// Handshake runs the ADR-0035 handshake and returns only once key
// confirmation succeeded in both directions, so no application frame can be
// accepted (or, for the daemon, sent) before the session keys are confirmed.
//
// Client: send M1, read M2 and R_d, verify, send R_c. Daemon: read M1, send
// M2 and R_d, wait for a valid R_c.
//
// Cancelling ctx (or its deadline expiring) closes the connection, which
// unblocks any in-flight read — a peer that connects and then says nothing
// cannot make the handshake hang.
func (c *Channel) Handshake(ctx context.Context, opts ...HandshakeOption) error {
	var cfg handshakeConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	c.mu.Lock()
	switch {
	case c.closed:
		c.mu.Unlock()
		return ErrClosed
	case c.established:
		c.mu.Unlock()
		return ErrAlreadyEstablished
	case c.handshaking:
		c.mu.Unlock()
		return ErrHandshakeInProgress
	}
	c.handshaking = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.handshaking = false
		c.mu.Unlock()
	}()

	stop := c.closeOnCancel(ctx)
	defer stop()

	var err error
	if c.role == RoleDaemon {
		err = c.handshakeDaemon()
	} else {
		err = c.handshakeClient(&cfg)
	}
	if err != nil {
		return c.fail(err)
	}
	return nil
}

// handshakeDaemon is the responder side.
func (c *Channel) handshakeDaemon() error {
	typ, payload, err := c.readFrame(maxHandshakeFrameLen)
	if err != nil {
		return err
	}
	if typ != frameHello {
		return fmt.Errorf("%w: expected hello, got frame type 0x%02x", ErrProtocol, typ)
	}
	clientEphPub, err := parseClientHello(payload)
	if err != nil {
		return err
	}

	eph, err := c.newEphemeral()
	if err != nil {
		return fmt.Errorf("e2ee: generate ephemeral key: %w", err)
	}
	daemonPub := c.kp.Public()
	daemonEphPub := eph.PublicKey().Bytes()

	es, err := dh(c.kp.private(), clientEphPub)
	if err != nil {
		return err
	}
	defer clear(es)
	ee, err := dh(eph, clientEphPub)
	if err != nil {
		return err
	}
	defer clear(ee)
	sched, err := deriveSchedule(es, ee, daemonPub, daemonEphPub, clientEphPub)
	if err != nil {
		return err
	}
	defer sched.zero()
	session, err := newSession(RoleDaemon, sched)
	if err != nil {
		return err
	}

	replyHello := make([]byte, 0, daemonHelloLen)
	replyHello = append(replyHello, ProtocolVersion, byte(RoleDaemon))
	replyHello = append(replyHello, daemonPub...)
	replyHello = append(replyHello, daemonEphPub...)
	replyReady := confirmTag(sched.confirmD, sched.th)
	wantReady := confirmTag(sched.confirmM, sched.th)

	if err := c.writeFrame(frameHello, replyHello); err != nil {
		return err
	}
	if err := c.writeFrame(frameReady, replyReady); err != nil {
		return err
	}

	// Wait for R_c. Nothing but handshake frames is acceptable until it
	// verifies, and the daemon seals nothing.
	for {
		typ, payload, err := c.readFrame(maxHandshakeFrameLen)
		if err != nil {
			return err
		}
		switch typ {
		case frameReady:
			if err := checkReady(payload, wantReady); err != nil {
				return err
			}
			c.mu.Lock()
			c.session = session
			c.established = true
			c.daemonPub, c.daemonEphPub, c.clientEphPub = daemonPub, daemonEphPub, clientEphPub
			c.th = sched.th
			c.peerHello, c.peerReady = bytes.Clone(payloadOfHello(clientEphPub)), wantReady
			c.replyHello, c.replyReady = replyHello, replyReady
			c.mu.Unlock()
			return nil
		case frameHello:
			if err := c.checkRepeatedHello(payload, payloadOfHello(clientEphPub)); err != nil {
				return err
			}
			if err := c.writeFrame(frameHello, replyHello); err != nil {
				return err
			}
			if err := c.writeFrame(frameReady, replyReady); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: expected ready, got frame type 0x%02x", ErrProtocol, typ)
		}
	}
}

// payloadOfHello rebuilds the M1 payload for a client ephemeral, the exact
// bytes a faithful retry repeats.
func payloadOfHello(clientEphPub []byte) []byte {
	p := make([]byte, 0, clientHelloLen)
	p = append(p, ProtocolVersion, byte(RoleMobile))
	return append(p, clientEphPub...)
}

// handshakeClient is the initiator side.
func (c *Channel) handshakeClient(cfg *handshakeConfig) error {
	eph, err := c.newEphemeral()
	if err != nil {
		return fmt.Errorf("e2ee: generate ephemeral key: %w", err)
	}
	clientEphPub := eph.PublicKey().Bytes()
	if err := c.writeFrame(frameHello, payloadOfHello(clientEphPub)); err != nil {
		return err
	}

	typ, m2, err := c.readFrame(maxHandshakeFrameLen)
	if err != nil {
		return err
	}
	if typ != frameHello {
		return fmt.Errorf("%w: expected hello, got frame type 0x%02x", ErrProtocol, typ)
	}
	daemonPub, daemonEphPub, err := parseDaemonHello(m2)
	if err != nil {
		return err
	}
	// The pin is checked before any key agreement.
	if cfg.pinned && !bytes.Equal(daemonPub, cfg.expectedPeerKey) {
		return ErrPeerKeyMismatch
	}

	es, err := dh(eph, daemonPub)
	if err != nil {
		return err
	}
	defer clear(es)
	ee, err := dh(eph, daemonEphPub)
	if err != nil {
		return err
	}
	defer clear(ee)
	sched, err := deriveSchedule(es, ee, daemonPub, daemonEphPub, clientEphPub)
	if err != nil {
		return err
	}
	defer sched.zero()
	session, err := newSession(RoleMobile, sched)
	if err != nil {
		return err
	}
	wantReady := confirmTag(sched.confirmD, sched.th)
	replyReady := confirmTag(sched.confirmM, sched.th)

	for {
		typ, payload, err := c.readFrame(maxHandshakeFrameLen)
		if err != nil {
			return err
		}
		switch typ {
		case frameReady:
			if err := checkReady(payload, wantReady); err != nil {
				return err
			}
		case frameHello:
			if err := c.checkRepeatedHello(payload, m2); err != nil {
				return err
			}
			continue
		default:
			return fmt.Errorf("%w: expected ready, got frame type 0x%02x", ErrProtocol, typ)
		}
		break
	}

	if err := c.writeFrame(frameReady, replyReady); err != nil {
		return err
	}
	c.mu.Lock()
	c.session = session
	c.established = true
	c.daemonPub, c.daemonEphPub, c.clientEphPub = daemonPub, daemonEphPub, clientEphPub
	c.th = sched.th
	c.peerHello, c.peerReady = bytes.Clone(m2), wantReady
	c.mu.Unlock()
	return nil
}

// parseVersion reads the version byte first, before length or role, so a v1
// hello (or any other version) is reported as such regardless of its shape.
func parseVersion(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: empty hello", ErrProtocol)
	}
	if v := payload[0]; v != ProtocolVersion {
		return &UnsupportedVersionError{Peer: v}
	}
	return nil
}

// parseClientHello validates M1 and returns the client ephemeral public key.
func parseClientHello(payload []byte) ([]byte, error) {
	if err := parseVersion(payload); err != nil {
		return nil, err
	}
	if len(payload) != clientHelloLen {
		return nil, fmt.Errorf("%w: client hello is %d bytes, want %d", ErrProtocol, len(payload), clientHelloLen)
	}
	if role := Role(payload[1]); role != RoleMobile {
		return nil, fmt.Errorf("%w: client hello carries role %s, want %s", ErrProtocol, role, RoleMobile)
	}
	pub := bytes.Clone(payload[2:])
	if _, err := ParsePublicKey(pub); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProtocol, err)
	}
	return pub, nil
}

// parseDaemonHello validates M2 and returns the daemon static and ephemeral
// public keys.
func parseDaemonHello(payload []byte) (daemonPub, daemonEphPub []byte, err error) {
	if err := parseVersion(payload); err != nil {
		return nil, nil, err
	}
	if len(payload) != daemonHelloLen {
		return nil, nil, fmt.Errorf("%w: daemon hello is %d bytes, want %d", ErrProtocol, len(payload), daemonHelloLen)
	}
	if role := Role(payload[1]); role != RoleDaemon {
		return nil, nil, fmt.Errorf("%w: daemon hello carries role %s, want %s", ErrProtocol, role, RoleDaemon)
	}
	daemonPub = bytes.Clone(payload[2 : 2+PublicKeySize])
	daemonEphPub = bytes.Clone(payload[2+PublicKeySize:])
	for _, k := range [][]byte{daemonPub, daemonEphPub} {
		if _, err := ParsePublicKey(k); err != nil {
			return nil, nil, fmt.Errorf("%w: %s", ErrProtocol, err)
		}
	}
	return daemonPub, daemonEphPub, nil
}

// checkReady verifies a ready payload against the expected MAC in constant
// time. An empty ready (the retired v1 shape) or any other length is a
// protocol error.
func checkReady(payload, want []byte) error {
	if len(payload) != readyLen {
		return fmt.Errorf("%w: ready is %d bytes, want %d", ErrProtocol, len(payload), readyLen)
	}
	if !hmac.Equal(payload, want) {
		return ErrHandshakeConfirm
	}
	return nil
}

// checkRepeatedHello applies ADR-0035 §4 to a hello that arrives after the
// peer's first: a byte-identical repeat is a transport retry, anything else
// is ErrKeyRotation. The hello is parsed as the peer's role would send it, so
// a v1 or malformed repeat is reported as such rather than as rotation.
func (c *Channel) checkRepeatedHello(payload, first []byte) error {
	var err error
	if c.role == RoleDaemon {
		_, err = parseClientHello(payload)
	} else {
		_, _, err = parseDaemonHello(payload)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, first) {
		return fmt.Errorf("%w: rotation requires a new session, not an in-session rekey", ErrKeyRotation)
	}
	return nil
}

// Send encrypts and writes one application message.
func (c *Channel) Send(msg []byte) error {
	session, err := c.activeSession()
	if err != nil {
		return err
	}
	counter, ciphertext, err := session.Seal(msg)
	if err != nil {
		return err
	}
	payload := make([]byte, 8+len(ciphertext))
	binary.BigEndian.PutUint64(payload[:8], counter)
	copy(payload[8:], ciphertext)
	return c.writeFrame(frameData, payload)
}

// Receive reads the next application message, decrypting and replay-checking
// it. A replayed or out-of-order frame returns ErrReplay and leaves the
// channel usable — the receive counter does not advance, so the peer's next
// live frame is still accepted — while a protocol violation closes it.
//
// Handshake frames arriving on an established session are handled here
// (ADR-0035 §4): a byte-identical repeat of the peer's hello is tolerated (the
// daemon re-sends its stored M2 and R_d), a different hello closes the channel
// with ErrKeyRotation, and a repeated ready must carry the same MAC.
func (c *Channel) Receive() ([]byte, error) {
	session, err := c.activeSession()
	if err != nil {
		return nil, err
	}

	for {
		typ, payload, err := c.readFrame(maxFrameLen)
		if err != nil {
			return nil, err
		}

		switch typ {
		case frameData:
			if len(payload) < 8 {
				return nil, c.fail(fmt.Errorf("%w: data frame is %d bytes, want at least 8", ErrProtocol, len(payload)))
			}
			counter := binary.BigEndian.Uint64(payload[:8])
			plaintext, err := session.Open(counter, payload[8:])
			if err != nil {
				return nil, err
			}
			return plaintext, nil

		case frameHello:
			if err := c.checkRepeatedHello(payload, c.peerHello); err != nil {
				return nil, c.fail(err)
			}
			if c.role == RoleDaemon {
				// The client never saw our reply: re-send the stored M2 and
				// R_d (no new keys, no AEAD frame).
				if err := c.writeFrame(frameHello, c.replyHello); err != nil {
					return nil, c.fail(err)
				}
				if err := c.writeFrame(frameReady, c.replyReady); err != nil {
					return nil, c.fail(err)
				}
			}

		case frameReady:
			if err := checkReady(payload, c.peerReady); err != nil {
				return nil, c.fail(err)
			}

		default:
			return nil, c.fail(fmt.Errorf("%w: unknown frame type 0x%02x", ErrProtocol, typ))
		}
	}
}

// TranscriptHash returns the handshake transcript hash th (ADR-0035 §2) once
// established, or nil before. It is public data that application layers bind
// signatures to.
func (c *Channel) TranscriptHash() []byte { return c.ifEstablished(c.th) }

// DaemonPublicKey returns the daemon's static public key once established.
func (c *Channel) DaemonPublicKey() []byte { return c.ifEstablished(c.daemonPub) }

// DaemonEphemeralPublicKey returns the daemon's per-handshake ephemeral
// public key once established.
func (c *Channel) DaemonEphemeralPublicKey() []byte { return c.ifEstablished(c.daemonEphPub) }

// ClientEphemeralPublicKey returns the client's per-handshake ephemeral
// public key once established.
func (c *Channel) ClientEphemeralPublicKey() []byte { return c.ifEstablished(c.clientEphPub) }

// ifEstablished returns a copy of b once the handshake completed, else nil.
func (c *Channel) ifEstablished(b []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.established {
		return nil
	}
	return bytes.Clone(b)
}

// Session returns the established session, or nil before the handshake
// completes.
func (c *Channel) Session() *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// Established reports whether the handshake completed.
func (c *Channel) Established() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.established
}

// Closed reports whether the channel has been closed.
func (c *Channel) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close closes the underlying connection. It is safe to call more than once.
func (c *Channel) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.conn.Close()
}

func (c *Channel) activeSession() (*Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	if !c.established {
		return nil, ErrNotEstablished
	}
	return c.session, nil
}

// fail closes the channel and returns err: every protocol violation is
// terminal, never something the channel silently continues past.
func (c *Channel) fail(err error) error {
	_ = c.Close()
	return err
}

// closeOnCancel closes the connection when ctx is done, returning a stop
// function that tears the watcher down.
//
// This uses context.AfterFunc rather than a home-rolled
// select-on-ctx.Done()-or-done-channel, because that pattern races: when the
// caller cancels ctx right after Handshake returns (the common
// ctx, cancel := ...; defer cancel() idiom our own callers use), a plain
// select has no ordering guarantee between the two now-ready cases and can
// still fire Close() on an already-established channel. AfterFunc's stop
// deregisters under ctx's own lock, so a stop() that happens-before cancel()
// is guaranteed to suppress the callback.
func (c *Channel) closeOnCancel(ctx context.Context) func() {
	if ctx == nil || ctx.Done() == nil {
		return func() {}
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	return func() { stop() }
}

func (c *Channel) writeFrame(typ byte, payload []byte) error {
	if len(payload)+1 > maxFrameLen {
		return fmt.Errorf("%w: frame of %d bytes exceeds the %d byte limit", ErrProtocol, len(payload)+1, maxFrameLen)
	}
	frame := make([]byte, 4+1+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(1+len(payload)))
	frame[4] = typ
	copy(frame[5:], payload)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.conn.Write(frame); err != nil {
		return fmt.Errorf("e2ee: write frame: %w", err)
	}
	return nil
}

// readFrame reads one length-prefixed frame. A truncated stream surfaces as
// an error rather than a partially-parsed frame, and an oversized length
// prefix is rejected before any allocation.
func (c *Channel) readFrame(limit uint32) (byte, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	var header [4]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return 0, nil, readErr(err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 {
		return 0, nil, fmt.Errorf("%w: zero-length frame", ErrProtocol)
	}
	if length > limit {
		return 0, nil, fmt.Errorf("%w: frame length %d exceeds the %d byte limit", ErrProtocol, length, limit)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.reader, buf); err != nil {
		return 0, nil, readErr(err)
	}
	return buf[0], buf[1:], nil
}

// readErr normalises a short read: a stream that ends mid-frame is a
// protocol violation, not a clean end of stream.
func readErr(err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: truncated frame: %w", ErrProtocol, err)
	}
	return fmt.Errorf("e2ee: read frame: %w", err)
}
