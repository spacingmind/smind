package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeCDP is a scripted DevTools endpoint: handlers answer methods, and the
// test pushes events. Every received call is recorded.
type fakeCDP struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	conn   *websocket.Conn
	calls  []fakeCall
	handle func(f *fakeCDP, c fakeCall) (result any, errMsg string)
	wmu    sync.Mutex
	ready  chan struct{}
}

type fakeCall struct {
	ID        int64
	Method    string
	SessionID string
	Params    map[string]any
}

func newFakeCDP(t *testing.T, handle func(f *fakeCDP, c fakeCall) (any, string)) *fakeCDP {
	f := &fakeCDP{t: t, handle: handle, ready: make(chan struct{})}
	up := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		close(f.ready)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
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
	}))
	t.Cleanup(f.srv.Close)
	return f
}

var hold = &struct{ x int }{}

func (f *fakeCDP) wsURL() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

func (f *fakeCDP) send(v any) {
	<-f.ready
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_ = f.conn.WriteJSON(v)
}

func (f *fakeCDP) event(sessionID, method string, params any) {
	f.send(map[string]any{"method": method, "sessionId": sessionID, "params": params})
}

func (f *fakeCDP) closeConn() {
	<-f.ready
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.conn.Close()
}

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
	c, err := DialCDP(context.Background(), f.wsURL())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var v struct{ Product string }
	if err := c.Call(context.Background(), "", "Browser.getVersion", nil, &v); err != nil || v.Product != "Fake/1" {
		t.Fatalf("call: %v %+v", err, v)
	}
	err = c.Call(context.Background(), "S1", "Bad.method", map[string]any{"a": 1}, nil)
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
	c, err := DialCDP(context.Background(), f.wsURL())
	if err != nil {
		t.Fatal(err)
	}
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
	c, err := DialCDP(context.Background(), f.wsURL())
	if err != nil {
		t.Fatal(err)
	}
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
