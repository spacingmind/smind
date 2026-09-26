package e2ee

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the E2EE wire protocol version. Both ends must match;
// there is no negotiation in v1.
const ProtocolVersion = 1

// maxFrameLen bounds a single frame so a malformed or hostile length prefix
// cannot make the reader allocate arbitrary memory.
const maxFrameLen = 1 << 20

// Frame types.
const (
	frameHello byte = 0x01
	frameReady byte = 0x02
	frameData  byte = 0x03
)

// helloPayloadLen is protocol version + role + X25519 public key.
const helloPayloadLen = 1 + 1 + PublicKeySize

// Role identifies which end of the session a channel is. The two ends must
// differ: the role selects which directional key is used for sending.
type Role uint8

// Session roles.
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
	// truncated frame, an unexpected frame type, or a version/role mismatch.
	ErrProtocol = errors.New("e2ee: protocol violation")
	// ErrKeyRotation marks a second handshake carrying a *different* public
	// key on an already-established session. Rotation means a brand new
	// session, never an in-place rekey (ADR-0007 (e)), so this closes the
	// channel instead of re-deriving keys.
	ErrKeyRotation = errors.New("e2ee: peer re-handshaked with a different key")
	// ErrNotEstablished is returned when application traffic is attempted
	// before the handshake completes.
	ErrNotEstablished = errors.New("e2ee: handshake not completed")
	// ErrAlreadyEstablished is returned when Handshake is called twice.
	ErrAlreadyEstablished = errors.New("e2ee: handshake already completed")
	// ErrClosed is returned once the channel has been closed.
	ErrClosed = errors.New("e2ee: channel closed")
)

// Channel is one end of an E2EE session carried over an arbitrary byte
// stream — in production, a relay-forwarded connection; in tests, an
// in-process pipe. The relay that sits between two Channels only ever sees
// the framed ciphertext.
type Channel struct {
	conn io.ReadWriteCloser
	kp   *KeyPair
	role Role

	reader *bufio.Reader

	writeMu sync.Mutex // serialises frame writes
	readMu  sync.Mutex // serialises frame reads

	mu          sync.Mutex // guards the fields below
	peerPub     []byte
	session     *Session
	established bool
	closed      bool
}

// NewChannel wraps conn as one end of an E2EE session. kp is this end's
// keypair: the daemon's persisted one, or a mobile device's per-session
// ephemeral one.
func NewChannel(conn io.ReadWriteCloser, kp *KeyPair, role Role) (*Channel, error) {
	if conn == nil {
		return nil, errors.New("e2ee: nil connection")
	}
	if kp == nil {
		return nil, errors.New("e2ee: nil keypair")
	}
	if role != RoleDaemon && role != RoleMobile {
		return nil, fmt.Errorf("e2ee: unknown role %d", uint8(role))
	}
	return &Channel{conn: conn, kp: kp, role: role, reader: bufio.NewReader(conn)}, nil
}

// Handshake runs the X25519 exchange: send hello, read the peer's hello,
// derive the directional keys, then exchange ready frames. It returns only
// once both ends have confirmed, so no application frame can be accepted
// before the session key exists.
//
// Cancelling ctx (or its deadline expiring) closes the connection, which
// unblocks any in-flight read — a peer that connects and then says nothing
// cannot make the handshake hang.
func (c *Channel) Handshake(ctx context.Context) error {
	c.mu.Lock()
	switch {
	case c.closed:
		c.mu.Unlock()
		return ErrClosed
	case c.established:
		c.mu.Unlock()
		return ErrAlreadyEstablished
	}
	c.mu.Unlock()

	stop := c.closeOnCancel(ctx)
	defer stop()

	if err := c.writeHello(); err != nil {
		return c.fail(err)
	}

	peerPub, err := c.readHello()
	if err != nil {
		return c.fail(err)
	}
	session, err := c.deriveSession(peerPub)
	if err != nil {
		return c.fail(err)
	}

	if err := c.writeFrame(frameReady, nil); err != nil {
		return c.fail(err)
	}
	if err := c.awaitReady(peerPub); err != nil {
		return c.fail(err)
	}

	c.mu.Lock()
	c.peerPub = peerPub
	c.session = session
	c.established = true
	c.mu.Unlock()
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
// Handshake frames arriving on an established session are handled here:
// a repeat of the peer's own hello is tolerated as a retry, while a hello
// carrying a different key closes the channel with ErrKeyRotation.
func (c *Channel) Receive() ([]byte, error) {
	session, err := c.activeSession()
	if err != nil {
		return nil, err
	}

	for {
		typ, payload, err := c.readFrame()
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
			if err := c.handleReHello(payload); err != nil {
				return nil, err
			}
			// A duplicate hello with the same key means the peer never saw our
			// ready frame; re-send it and keep reading.
			if err := c.writeFrame(frameReady, nil); err != nil {
				return nil, c.fail(err)
			}

		case frameReady:
			// Duplicate ready: harmless, the peer retried its handshake.

		default:
			return nil, c.fail(fmt.Errorf("%w: unknown frame type 0x%02x", ErrProtocol, typ))
		}
	}
}

// PeerPublicKey returns the peer's X25519 public key once established.
func (c *Channel) PeerPublicKey() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peerPub == nil {
		return nil
	}
	return bytes.Clone(c.peerPub)
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

func (c *Channel) writeHello() error {
	payload := make([]byte, 0, helloPayloadLen)
	payload = append(payload, ProtocolVersion, byte(c.role))
	payload = append(payload, c.kp.Public()...)
	return c.writeFrame(frameHello, payload)
}

// readHello reads frames until the peer's hello arrives, rejecting anything
// else: application data cannot precede the handshake.
func (c *Channel) readHello() ([]byte, error) {
	typ, payload, err := c.readFrame()
	if err != nil {
		return nil, err
	}
	if typ != frameHello {
		return nil, fmt.Errorf("%w: expected hello, got frame type 0x%02x", ErrProtocol, typ)
	}
	return c.parseHello(payload)
}

func (c *Channel) parseHello(payload []byte) ([]byte, error) {
	if len(payload) != helloPayloadLen {
		return nil, fmt.Errorf("%w: hello is %d bytes, want %d", ErrProtocol, len(payload), helloPayloadLen)
	}
	if version := payload[0]; version != ProtocolVersion {
		return nil, fmt.Errorf("%w: peer protocol version %d, want %d", ErrProtocol, version, ProtocolVersion)
	}
	peerRole := Role(payload[1])
	if peerRole != RoleDaemon && peerRole != RoleMobile {
		return nil, fmt.Errorf("%w: peer sent unknown role %d", ErrProtocol, payload[1])
	}
	if peerRole == c.role {
		return nil, fmt.Errorf("%w: peer claims the same role (%s) as this end", ErrProtocol, peerRole)
	}
	peerPub := bytes.Clone(payload[2:])
	if _, err := ParsePublicKey(peerPub); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProtocol, err)
	}
	return peerPub, nil
}

// awaitReady waits for the peer's ready frame, tolerating a duplicate hello
// that repeats the same key (a retry) but not one that changes it.
func (c *Channel) awaitReady(peerPub []byte) error {
	for {
		typ, payload, err := c.readFrame()
		if err != nil {
			return err
		}
		switch typ {
		case frameReady:
			return nil
		case frameHello:
			retryPub, err := c.parseHello(payload)
			if err != nil {
				return err
			}
			if !bytes.Equal(retryPub, peerPub) {
				return fmt.Errorf("%w: peer changed keys mid-handshake", ErrKeyRotation)
			}
			if err := c.writeFrame(frameReady, nil); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: expected ready, got frame type 0x%02x", ErrProtocol, typ)
		}
	}
}

// handleReHello applies ADR-0007 (e) to a hello arriving on an established
// session: same key is a retry, a different key is an attack.
func (c *Channel) handleReHello(payload []byte) error {
	newPub, err := c.parseHello(payload)
	if err != nil {
		return c.fail(err)
	}
	if !bytes.Equal(newPub, c.PeerPublicKey()) {
		return c.fail(fmt.Errorf("%w: rotation requires a new session, not an in-session rekey", ErrKeyRotation))
	}
	return nil
}

func (c *Channel) deriveSession(peerPub []byte) (*Session, error) {
	peerKey, err := ParsePublicKey(peerPub)
	if err != nil {
		return nil, err
	}
	daemonPub, mobilePub := c.kp.Public(), peerPub
	if c.role == RoleMobile {
		daemonPub, mobilePub = peerPub, c.kp.Public()
	}
	return newSession(c.kp.private(), peerKey, c.role, daemonPub, mobilePub)
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
func (c *Channel) readFrame() (byte, []byte, error) {
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
	if length > maxFrameLen {
		return 0, nil, fmt.Errorf("%w: frame length %d exceeds the %d byte limit", ErrProtocol, length, maxFrameLen)
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
