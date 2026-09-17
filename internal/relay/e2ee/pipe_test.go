package e2ee

import (
	"bytes"
	"encoding/binary"
	"io"
	"sync"
)

// memConn is one end of an in-process, buffered byte pipe standing in for a
// relay-forwarded connection. It records everything written to it, which is
// exactly what a relay would see on the wire — tests use that record both to
// assert the relay only ever sees ciphertext and to replay captured frames.
type memConn struct {
	mu     sync.Mutex
	in     chan []byte
	out    chan []byte
	closed chan struct{}
	once   sync.Once
	rest   []byte
	sent   [][]byte
}

// newMemPipe returns two connected endpoints.
func newMemPipe() (*memConn, *memConn) {
	const bufFrames = 64
	a2b := make(chan []byte, bufFrames)
	b2a := make(chan []byte, bufFrames)
	a := &memConn{in: b2a, out: a2b, closed: make(chan struct{})}
	b := &memConn{in: a2b, out: b2a, closed: make(chan struct{})}
	return a, b
}

func (c *memConn) Read(p []byte) (int, error) {
	for len(c.rest) == 0 {
		select {
		case b, ok := <-c.in:
			if !ok {
				return 0, io.EOF
			}
			c.rest = b
		case <-c.closed:
			return 0, io.EOF
		}
	}
	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}

func (c *memConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	c.sent = append(c.sent, bytes.Clone(p))
	c.out <- bytes.Clone(p)
	return len(p), nil
}

func (c *memConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.once.Do(func() {
		close(c.closed)
		close(c.out)
	})
	return nil
}

// inject delivers raw bytes to this endpoint's reader as if the relay had
// forwarded them, bypassing the peer Channel.
func (c *memConn) inject(raw []byte) {
	c.in <- bytes.Clone(raw)
}

// written returns every chunk this endpoint wrote, in order.
func (c *memConn) written() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.sent...)
}

// lastWritten returns the most recent chunk written by this endpoint.
func (c *memConn) lastWritten() []byte {
	sent := c.written()
	if len(sent) == 0 {
		return nil
	}
	return sent[len(sent)-1]
}

// rawFrame assembles a wire frame by hand, so tests can craft malformed or
// replayed traffic a well-behaved Channel would never send.
func rawFrame(typ byte, payload []byte) []byte {
	frame := make([]byte, 4+1+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(1+len(payload)))
	frame[4] = typ
	copy(frame[5:], payload)
	return frame
}

func helloPayload(version byte, role Role, pub []byte) []byte {
	payload := make([]byte, 0, 2+len(pub))
	payload = append(payload, version, byte(role))
	return append(payload, pub...)
}
