package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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
}

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

// Client is a Chrome DevTools Protocol client over one websocket with
// flattened sessions. It is safe for concurrent use.
type Client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan cdpResult
	closed  bool
	lostErr error

	events chan Event
	done   chan struct{}
}

// DialCDP connects to a browser websocket URL. Events are delivered in order
// on Events(); the consumer must keep draining it.
func DialCDP(ctx context.Context, wsURL string) (*Client, error) {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := d.DialContext(ctx, wsURL, nil) // no Origin header: the browser refuses cross-origin pages
	if err != nil {
		return nil, fmt.Errorf("browser: dial devtools: %w", err)
	}
	conn.SetReadLimit(64 << 20)
	c := &Client{
		conn:    conn,
		pending: map[int64]chan cdpResult{},
		events:  make(chan Event, 256),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

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
		_, data, err := c.conn.ReadMessage()
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
			select {
			case c.events <- Event{Method: m.Method, SessionID: m.SessionID, Params: m.Params}:
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
	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := c.conn.WriteMessage(websocket.TextMessage, msg)
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
