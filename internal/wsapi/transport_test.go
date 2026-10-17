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
