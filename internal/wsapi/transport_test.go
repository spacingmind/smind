package wsapi

// transport_test.go proves the Transport refactor (conn.go/server.go) is
// dispatch-identical to the WebSocket path over a Transport with no
// WebSocket, and no relay, involved at all -- the "fake implementation"
// unit test called for in docs/plans/active/mobile-app-milestone-1.md's
// Item 1 Test Scenarios.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// pipeTransport is an in-memory Transport: each end's Send feeds the
// other's Receive over an unbuffered channel, and Close unblocks any
// blocked Receive -- enough to drive conn.serve exactly like a real
// connection, with no network or WebSocket framing anywhere.
type pipeTransport struct {
	out    chan []byte
	in     chan []byte
	closed chan struct{}
}

func newPipeTransportPair() (a, b *pipeTransport) {
	c1 := make(chan []byte)
	c2 := make(chan []byte)
	closed := make(chan struct{})
	return &pipeTransport{out: c1, in: c2, closed: closed},
		&pipeTransport{out: c2, in: c1, closed: closed}
}

func (p *pipeTransport) Receive() ([]byte, error) {
	select {
	case data := <-p.in:
		return data, nil
	case <-p.closed:
		return nil, errPipeClosed
	}
}

func (p *pipeTransport) Send(data []byte) error {
	select {
	case p.out <- data:
		return nil
	case <-p.closed:
		return errPipeClosed
	}
}

func (p *pipeTransport) Close() error {
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	return nil
}

var errPipeClosed = errPipeClosedError{}

type errPipeClosedError struct{}

func (errPipeClosedError) Error() string { return "pipeTransport: closed" }

// TestAPI_ServeTransport_FakeTransportDispatchesLikeWebSocket drives a real
// API (built exactly as wsapi.New does) over a fake Transport instead of a
// WebSocket, sending a request and asserting the response matches what the
// existing WebSocket-based tests get from the same method -- proving
// methodHandlers dispatch is Transport-agnostic, not WebSocket-specific.
func TestAPI_ServeTransport_FakeTransportDispatchesLikeWebSocket(t *testing.T) {
	t.Parallel()

	wm, db := newTestWorkspaceManager(t)
	handler, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(handler.Close)

	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.ServeTransport(ctx, server)
		close(done)
	}()

	reqEnv := envelope{ID: "1", Method: "workspace.list"}
	data, err := json.Marshal(reqEnv)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := client.Send(data); err != nil {
		t.Fatalf("send request: %v", err)
	}

	respCh := make(chan envelope, 1)
	go func() {
		data, err := client.Receive()
		if err != nil {
			t.Errorf("receive response: %v", err)
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Errorf("unmarshal response: %v", err)
			return
		}
		respCh <- env
	}()

	select {
	case resp := <-respCh:
		if resp.ID != "1" {
			t.Fatalf("response id = %q, want %q", resp.ID, "1")
		}
		if resp.Error != nil {
			t.Fatalf("workspace.list error = %v", resp.Error)
		}
		if got := string(resp.Result); got != "[]" {
			t.Fatalf("workspace.list result = %s, want []", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for response")
	}

	cancel()
	client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeTransport did not return after cancel")
	}
}

// TestUnknownMethodAlwaysGetsAMethodNotFoundResponse: a request for a method
// that does not exist is answered with an error response carrying the
// request's id — over ServeTransport (a relay-served device) just as over the
// WebSocket. It used to be dropped silently (the error was sent before the
// request was registered in flight, so `terminate` refused to write it), which
// left a client — a device sending a newer method to an older daemon — waiting
// forever. The message and the absence of an error code are part of the
// contract: clients detect an older daemon by the "method not found" prefix.
func TestUnknownMethodAlwaysGetsAMethodNotFoundResponse(t *testing.T) {
	t.Parallel()

	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go api.ServeTransport(ctx, server)

	for _, method := range []string{"relay.device_hello", "no.such.method"} {
		id := "u-" + method
		data, _ := json.Marshal(envelope{ID: id, Method: method, Params: json.RawMessage(`{}`)})
		if err := client.Send(data); err != nil {
			t.Fatalf("send %s: %v", method, err)
		}
		respCh := make(chan envelope, 1)
		go func() {
			data, err := client.Receive()
			if err != nil {
				return
			}
			var env envelope
			if json.Unmarshal(data, &env) == nil {
				respCh <- env
			}
		}()
		select {
		case resp := <-respCh:
			if resp.ID != id {
				t.Fatalf("%s: response id = %q, want %q", method, resp.ID, id)
			}
			if resp.Error == nil {
				t.Fatalf("%s: expected an error response, got result %s", method, resp.Result)
			}
			if want := "method not found: " + method; resp.Error.Message != want {
				t.Fatalf("%s: error message = %q, want %q", method, resp.Error.Message, want)
			}
			if resp.Error.Code != "" {
				t.Fatalf("%s: unknown-method error must carry no code (clients match on the message), got %q", method, resp.Error.Code)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no response to an unknown method within 5s", method)
		}
	}

	// The connection is still healthy afterwards: a real method is answered.
	data, _ := json.Marshal(envelope{ID: "after", Method: "workspace.list"})
	if err := client.Send(data); err != nil {
		t.Fatal(err)
	}
	got := make(chan envelope, 1)
	go func() {
		d, err := client.Receive()
		var env envelope
		if err == nil && json.Unmarshal(d, &env) == nil {
			got <- env
		}
	}()
	select {
	case env := <-got:
		if env.ID != "after" || env.Error != nil {
			t.Fatalf("workspace.list after an unknown method = %+v", env)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection stopped answering after an unknown method")
	}
}
