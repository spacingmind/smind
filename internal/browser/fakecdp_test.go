package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCDP is a scripted DevTools endpoint over an in-memory pipe pair:
// handlers answer methods, and the test pushes events. Every received call is
// recorded.
type fakeCDP struct {
	t      *testing.T
	client io.ReadWriteCloser // the end a Client uses
	server io.ReadWriteCloser
	mu     sync.Mutex
	calls  []fakeCall
	handle func(f *fakeCDP, c fakeCall) (result any, errMsg string)
	wmu    sync.Mutex
}

type fakeCall struct {
	ID        int64
	Method    string
	SessionID string
	Params    map[string]any
}

type pipeEnd struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p pipeEnd) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeEnd) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeEnd) Close() error                { _ = p.r.Close(); return p.w.Close() }

func newFakeCDP(t *testing.T, handle func(f *fakeCDP, c fakeCall) (any, string)) *fakeCDP {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	f := &fakeCDP{t: t, handle: handle, client: pipeEnd{s2cR, c2sW}, server: pipeEnd{c2sR, s2cW}}
	go f.serve()
	t.Cleanup(func() { _ = f.server.Close(); _ = f.client.Close() })
	return f
}

func (f *fakeCDP) serve() {
	br := bufio.NewReader(f.server)
	for {
		data, err := br.ReadBytes(0)
		if err != nil {
			return
		}
		data = data[:len(data)-1]
		var m struct {
			ID        int64          `json:"id"`
			Method    string         `json:"method"`
			SessionID string         `json:"sessionId"`
			Params    map[string]any `json:"params"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		call := fakeCall{ID: m.ID, Method: m.Method, SessionID: m.SessionID, Params: m.Params}
		f.mu.Lock()
		f.calls = append(f.calls, call)
		f.mu.Unlock()
		var res any = map[string]any{}
		errMsg := ""
		if f.handle != nil {
			res, errMsg = f.handle(f, call)
			if res == nil && errMsg == "" {
				res = map[string]any{}
			}
		}
		if res == hold {
			continue // never answered, for timeout tests
		}
		resp := map[string]any{"id": m.ID, "sessionId": m.SessionID}
		if errMsg != "" {
			resp["error"] = map[string]any{"code": -32000, "message": errMsg}
		} else {
			resp["result"] = res
		}
		f.send(resp)
	}
}

var hold = &struct{ x int }{}

func (f *fakeCDP) send(v any) {
	b, _ := json.Marshal(v)
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_, _ = f.server.Write(append(b, 0))
}

func (f *fakeCDP) event(sessionID, method string, params any) {
	f.send(map[string]any{"method": method, "sessionId": sessionID, "params": params})
}

func (f *fakeCDP) closeConn() { _ = f.server.Close() }

func (f *fakeCDP) callsFor(method string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// waitCall waits until n calls of method were received and returns them.
func (f *fakeCDP) waitCall(method string, n int) []fakeCall {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := f.callsFor(method); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.t.Fatalf("timed out waiting for %d %s calls, got %v", n, method, f.callsFor(method))
	return nil
}

func TestCDPCallEventAndErrors(t *testing.T) {
	f := newFakeCDP(t, func(f *fakeCDP, c fakeCall) (any, string) {
		switch c.Method {
		case "Browser.getVersion":
			return map[string]any{"product": "Fake/1"}, ""
		case "Bad.method":
			return nil, "no such method"
		}
		return nil, ""
	})
	c := NewClient(f.client)
	defer c.Close()
	var v struct{ Product string }
	if err := c.Call(context.Background(), "", "Browser.getVersion", nil, &v); err != nil || v.Product != "Fake/1" {
		t.Fatalf("call: %v %+v", err, v)
	}
	err := c.Call(context.Background(), "S1", "Bad.method", map[string]any{"a": 1}, nil)
	var ce *CDPError
	if !asCDPError(err, &ce) || ce.Message != "no such method" {
		t.Fatalf("error response: %v", err)
	}
	if got := f.callsFor("Bad.method"); len(got) != 1 || got[0].SessionID != "S1" || got[0].Params["a"] != float64(1) {
		t.Fatalf("session and params: %+v", got)
	}
	f.event("S1", "Page.loadEventFired", map[string]any{"timestamp": 1})
	select {
	case ev := <-c.Events():
		if ev.Method != "Page.loadEventFired" || ev.SessionID != "S1" {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
}

func asCDPError(err error, target **CDPError) bool {
	ce, ok := err.(*CDPError)
	if ok {
		*target = ce
	}
	return ok
}

func TestCDPCallTimeout(t *testing.T) {
	f := newFakeCDP(t, func(f *fakeCDP, c fakeCall) (any, string) { return hold, "" })
	c := NewClient(f.client)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := c.Call(ctx, "", "Slow.call", nil, nil); err != context.DeadlineExceeded {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	c.mu.Lock()
	n := len(c.pending)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("a timed-out call must not leak its pending slot, %d left", n)
	}
}

func TestCDPConnectionLossFailsPending(t *testing.T) {
	f := newFakeCDP(t, func(f *fakeCDP, c fakeCall) (any, string) { return hold, "" })
	c := NewClient(f.client)
	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "", "Slow.call", nil, nil) }()
	f.waitCall("Slow.call", 1)
	f.closeConn()
	select {
	case err := <-errc:
		if err != ErrClosed {
			t.Fatalf("want ErrClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call was not failed")
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed")
	}
	if err := c.Call(context.Background(), "", "X", nil, nil); err != ErrClosed {
		t.Fatalf("call after loss: %v", err)
	}
}

// A page must not be able to end its own session by making the browser send a
// huge message: an over-cap reply fails only the call it answers, an over-cap
// event is dropped, and the connection lives on.
func TestCDPOverCapReplyFailsOnlyThatCall(t *testing.T) {
	f := newFakeCDP(t, func(f *fakeCDP, c fakeCall) (any, string) {
		if c.Method == "Accessibility.getFullAXTree" {
			return map[string]any{"nodes": strings.Repeat("a", maxMessageBytes+4096)}, ""
		}
		return map[string]any{"ok": true}, ""
	})
	c := NewClient(f.client)
	defer c.Close()
	err := c.Call(context.Background(), "", "Accessibility.getFullAXTree", nil, nil)
	if !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("an over-cap reply must fail its call with ErrReplyTooLarge, got %v", err)
	}
	select {
	case <-c.Done():
		t.Fatalf("the connection must survive an over-cap reply: %v", c.Err())
	default:
	}
	var out struct{ OK bool }
	if err := c.Call(context.Background(), "", "Browser.getVersion", nil, &out); err != nil || !out.OK {
		t.Fatalf("the next call must work: %v %+v", err, out)
	}
}

func TestCDPOverCapEventIsDroppedAndTheConnectionLives(t *testing.T) {
	f := newFakeCDP(t, nil)
	c := NewClient(f.client)
	defer c.Close()
	go f.send(map[string]any{"method": "Runtime.consoleAPICalled", "params": map[string]any{"x": strings.Repeat("a", maxMessageBytes+10)}})
	go f.event("S1", "Page.loadEventFired", map[string]any{"timestamp": 1})
	select {
	case ev := <-c.Events():
		if ev.Method != "Page.loadEventFired" {
			t.Fatalf("the over-cap event must be dropped, the next one delivered: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event after an over-cap one was never delivered")
	}
	select {
	case <-c.Done():
		t.Fatalf("the connection must survive an over-cap event: %v", c.Err())
	default:
	}
}

func TestCDPAbsurdMessageDropsTheConnection(t *testing.T) {
	old := maxDiscardBytes
	maxDiscardBytes = 1 << 20
	defer func() { maxDiscardBytes = old }()
	f := newFakeCDP(t, nil)
	c := NewClient(f.client)
	defer c.Close()
	go f.send(map[string]any{"method": "Big.event", "params": map[string]any{"x": strings.Repeat("a", maxMessageBytes+(2<<20))}})
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("a message beyond the discard bound must end the connection")
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "far too large") {
		t.Fatalf("reason: %v", err)
	}
}

func TestCDPEventBacklogDropsTheConnection(t *testing.T) {
	old := maxEventBytes
	maxEventBytes = 1 << 10
	defer func() { maxEventBytes = old }()
	f := newFakeCDP(t, nil)
	c := NewClient(f.client) // nobody drains Events(): a slow consumer
	defer c.Close()
	for i := 0; i < 50; i++ {
		go f.event("", "Spam.event", map[string]any{"x": strings.Repeat("a", 100)})
	}
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("an unbounded event backlog must end the connection")
	}
}

func TestCDPLargeValidMessageThroughThePipe(t *testing.T) {
	// Bigger than the 64 KB read buffer (ReadSlice returns ErrBufferFull), smaller than the cap.
	big := strings.Repeat("b", 3<<20)
	f := newFakeCDP(t, func(f *fakeCDP, c fakeCall) (any, string) {
		return map[string]any{"data": big}, ""
	})
	c := NewClient(f.client)
	defer c.Close()
	var out struct{ Data string }
	if err := c.Call(context.Background(), "", "Page.captureScreenshot", nil, &out); err != nil || out.Data != big {
		t.Fatalf("a valid 3 MB message must arrive intact: err=%v len=%d", err, len(out.Data))
	}
	f.event("S", "Big.event", map[string]any{"x": big})
	select {
	case ev := <-c.Events():
		if ev.size < len(big) {
			t.Fatalf("event size %d", ev.size)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("large event not delivered")
	}
}

// wedgedConn accepts reads that never return and writes that block until closed.
type wedgedConn struct{ closed chan struct{} }

func (w *wedgedConn) Read([]byte) (int, error)  { <-w.closed; return 0, io.EOF }
func (w *wedgedConn) Write([]byte) (int, error) { <-w.closed; return 0, io.ErrClosedPipe }
func (w *wedgedConn) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func TestCDPWedgedWriteClosesTheConnection(t *testing.T) {
	c := NewClient(&wedgedConn{closed: make(chan struct{})})
	c.SetWriteTimeout(100 * time.Millisecond)
	errc := make(chan error, 2)
	for i := 0; i < 2; i++ { // the second caller waits behind the wedged writer
		go func() { errc <- c.Call(context.Background(), "", "X.y", nil, nil) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err == nil {
				t.Fatal("a wedged write must fail the call")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("one wedged browser must not block every caller regardless of ctx")
		}
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("the connection must be closed")
	}
	if !errors.Is(c.Err(), ErrWriteTimeout) {
		t.Fatalf("reason: %v", c.Err())
	}
}
