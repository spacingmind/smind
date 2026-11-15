package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// The browser is driven over a pipe pair (--remote-debugging-pipe), never a
// TCP port: no other local process can attach to it. Messages are JSON
// objects each terminated by a NUL byte.
const maxMessageBytes = 8 << 20 // one CDP message (a screenshot is ~2 MB base64)

// maxEventBytes bounds undelivered events held for the consumer (a var so a
// test can lower it).
var maxEventBytes int64 = 32 << 20

// CDPError is an error response from the browser.
type CDPError struct {
	Code    int
	Message string
}

func (e *CDPError) Error() string { return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message) }

// ErrClosed is returned for calls on a closed or lost connection.
var ErrClosed = errors.New("browser: connection closed")

// Event is a protocol event. SessionID is empty for browser-level events.
type Event struct {
	Method    string
	SessionID string
	Params    json.RawMessage
	size      int
}

// ErrOverflow is the reason a connection is dropped when the consumer cannot
// keep up with the browser's events (fail closed).
var ErrOverflow = errors.New("browser: too many undelivered events")

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type cdpResult struct {
	raw json.RawMessage
	err error
}

// Client is a Chrome DevTools Protocol client over one pipe pair with
// flattened sessions. It is safe for concurrent use.
type Client struct {
	conn    io.ReadWriteCloser
	br      *bufio.Reader
	writeMu sync.Mutex
	queued  atomic.Int64 // bytes of events not yet released by the consumer

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan cdpResult
	closed  bool
	lostErr error

	events chan Event
	done   chan struct{}
}

// NewClient speaks CDP over conn (the browser's pipe pair). Events are
// delivered in order on Events(); the consumer must keep draining it and call
// release for each one.
func NewClient(conn io.ReadWriteCloser) *Client {
	c := &Client{
		conn:    conn,
		br:      bufio.NewReaderSize(conn, 64<<10),
		pending: map[int64]chan cdpResult{},
		events:  make(chan Event, 256),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// readMessage reads one NUL-terminated message, refusing one over the limit.
func (c *Client) readMessage() ([]byte, error) {
	var msg []byte
	for {
		chunk, err := c.br.ReadSlice(0)
		msg = append(msg, chunk...)
		if len(msg) > maxMessageBytes+1 {
			return nil, errors.New("browser: a devtools message is too large")
		}
		switch err {
		case nil:
			return msg[:len(msg)-1], nil
		case bufio.ErrBufferFull:
			continue
		default:
			return nil, err
		}
	}
}

// release tells the client the consumer is done with an event's bytes.
func (c *Client) release(ev Event) { c.queued.Add(-int64(ev.size)) }

// Events returns the event stream; it is closed when the connection ends.
func (c *Client) Events() <-chan Event { return c.events }

// Done is closed when the connection ends, for any reason.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) readLoop() {
	var readErr error
	defer func() {
		c.fail(readErr)
		close(c.events)
	}()
	for {
		data, err := c.readMessage()
		if err != nil {
			readErr = err
			return
		}
		var m cdpMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch == nil {
				continue
			}
			if m.Error != nil {
				ch <- cdpResult{err: &CDPError{Code: m.Error.Code, Message: m.Error.Message}}
			} else {
				ch <- cdpResult{raw: m.Result}
			}
			continue
		}
		if m.Method != "" {
			if c.queued.Add(int64(len(data))) > maxEventBytes {
				readErr = ErrOverflow
				return
			}
			select {
			case c.events <- Event{Method: m.Method, SessionID: m.SessionID, Params: m.Params, size: len(data)}:
			case <-c.done:
				return
			}
		}
	}
}

// fail marks the connection lost and fails every pending call.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.lostErr = err
	pend := c.pending
	c.pending = map[int64]chan cdpResult{}
	close(c.done)
	c.mu.Unlock()
	for _, ch := range pend {
		ch <- cdpResult{err: ErrClosed}
	}
	_ = c.conn.Close()
}

// Close ends the connection; pending calls fail with ErrClosed.
func (c *Client) Close() error {
	c.fail(nil)
	return nil
}

// Call sends a method (sessionID empty for the browser target) and decodes
// the result into out when out is non-nil. The context bounds the wait.
func (c *Client) Call(ctx context.Context, sessionID, method string, params, out any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	ch := make(chan cdpResult, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	msg, _ := json.Marshal(cdpMessage{ID: id, Method: method, Params: raw, SessionID: sessionID})
	if len(msg) > maxMessageBytes {
		c.drop(id)
		return errors.New("browser: request too large")
	}
	c.writeMu.Lock()
	_, err := c.conn.Write(append(msg, 0))
	c.writeMu.Unlock()
	if err != nil {
		c.drop(id)
		c.fail(err)
		return fmt.Errorf("%w: %v", ErrClosed, err)
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if out != nil && len(res.raw) > 0 {
			return json.Unmarshal(res.raw, out)
		}
		return nil
	case <-ctx.Done():
		c.drop(id)
		return ctx.Err()
	}
}

func (c *Client) drop(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
