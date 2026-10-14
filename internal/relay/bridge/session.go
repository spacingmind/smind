package bridge

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/relay/client"
)

// session is one device's daemon-side state: its relay route, its E2EE
// DataConn, who the device proved to be, and its lifecycle flags.
//
// Fields marked "hub" are guarded by Hub.mu; fields marked "runner" are only
// touched by the single runSession goroutine that owns the session at any
// moment (consecutive owners are ordered through Hub.mu via running).
type session struct {
	ref    routeRef
	ctx    context.Context
	cancel context.CancelFunc

	running, orphaned, serving bool       // hub
	conn                       *relayConn // hub: the connection the runner serves on
	pending                    *relayConn // hub: a newer connection that announced this route while the runner was still unwinding
	orphanedAt                 time.Time  // hub
	deviceID, label            string     // hub: verified identity ("" until/unless hello)
	admitted, legacy           bool       // hub: past identification (counts toward the device cap)

	dc         *client.DataConn // runner
	identified bool             // runner
}

// newSessionLocked creates and registers a session. Caller holds Hub.mu and
// has checked the session cap.
func (h *Hub) newSessionLocked(ref routeRef) *session {
	ctx, cancel := context.WithCancel(h.hubCtx)
	s := &session{ref: ref, ctx: ctx, cancel: cancel}
	h.sessions[ref] = s
	return s
}

func (h *Hub) setSessionConn(s *session, c *relayConn) {
	h.mu.Lock()
	s.conn = c
	h.mu.Unlock()
}

// admittedCountLocked is how many sessions have passed identification: the
// ones the device cap counts. Caller holds Hub.mu.
func (h *Hub) admittedCountLocked() int {
	n := 0
	for _, s := range h.sessions {
		if s.admitted {
			n++
		}
	}
	return n
}

// sessionReader is the per-route receive loop. DataConn.Receive holds the
// channel's read lock while blocked, and DataConn.Close/DropTransport take
// that same lock, so nothing may call them while a Receive is pending: the
// loop owns Receive, and everything else ends it by cancelling the session's
// context (which cancels the stream and unblocks Receive).
type sessionReader struct {
	msgs chan []byte
	done chan struct{}

	mu sync.Mutex
	e  error
}

func newSessionReader(ctx context.Context, dc *client.DataConn) *sessionReader {
	r := &sessionReader{msgs: make(chan []byte, 16), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		defer close(r.msgs)
		for {
			msg, err := dc.Receive()
			if err != nil {
				r.mu.Lock()
				r.e = err
				r.mu.Unlock()
				return
			}
			select {
			case r.msgs <- msg:
			case <-ctx.Done():
				r.mu.Lock()
				r.e = ctx.Err()
				r.mu.Unlock()
				return
			}
		}
	}()
	return r
}

// stop is a no-op by design: the reader ends when the stream does (transport
// failure, relay connection closed, or the session context cancelled) and
// must never be interrupted by closing the DataConn under it.
func (r *sessionReader) stop() {}

// err is why the reader ended (nil while it is still running).
func (r *sessionReader) err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.e
}

// deviceTransport adapts a session to wsapi.Transport.
type deviceTransport struct {
	s *session
	r *sessionReader

	sendMu sync.Mutex

	mu       sync.Mutex
	pushback []byte
	hasPush  bool
	closed   bool
}

func (t *deviceTransport) push(msg []byte) {
	t.mu.Lock()
	t.pushback, t.hasPush = msg, true
	t.mu.Unlock()
}

func (t *deviceTransport) Receive() ([]byte, error) {
	t.mu.Lock()
	if t.hasPush {
		m := t.pushback
		t.pushback, t.hasPush = nil, false
		t.mu.Unlock()
		return m, nil
	}
	t.mu.Unlock()
	m, ok := <-t.r.msgs
	if !ok {
		if err := t.r.err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return m, nil
}

func (t *deviceTransport) Send(data []byte) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	return t.s.dc.Send(data)
}

// Close is called by the RPC server when it ends the connection: the session
// is over (cancelling its context also unblocks the reader).
func (t *deviceTransport) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.s.cancel()
	return nil
}

func (t *deviceTransport) closedByServer() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

var errReaderEnded = errors.New("relay bridge: device stream ended")
