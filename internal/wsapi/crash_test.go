package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/crash"
)

// crashDir points the process-default crash recorder at a temp dir for one test.
func crashDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "crashes")
	crash.Init(crash.Config{Dir: dir, Source: "daemon", Version: "test", MaxPerMinute: 1000})
	t.Cleanup(func() { crash.Init(crash.Config{}) })
	return dir
}

func crashReports(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "crash-*.json"))
	return files
}

// crash-ws-dispatch-panic-recorded-error-response and
// crash-ws-connection-survives-panic: a panicking method is answered with a
// generic internal error, leaves a redacted report, and the same connection
// still serves the next request.
func TestCrashWSDispatchPanicRecordedErrorResponse(t *testing.T) {
	dir := crashDir(t)
	hs := map[string]handlerFunc{
		"boom": func(context.Context, *requestContext, json.RawMessage) (any, error) {
			panic("SECRET-PROMPT-TEXT in a panic value")
		},
		"ping": func(context.Context, *requestContext, json.RawMessage) (any, error) { return "pong", nil },
	}
	client, server := newPipeTransportPair()
	c := newConn(server, hs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.serve(ctx)
	defer client.Close()

	call := func(id, method string) envelope {
		data, _ := json.Marshal(envelope{ID: id, Method: method})
		if err := client.Send(data); err != nil {
			t.Fatal(err)
		}
		raw, err := client.Receive()
		if err != nil {
			t.Fatalf("receive after %s: %v", method, err)
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		return env
	}

	resp := call("1", "boom")
	if resp.Error == nil || resp.Error.Message != "internal error" {
		t.Fatalf("boom response = %+v, want a generic internal error", resp)
	}
	if strings.Contains(string(crashJSON(t, resp)), "SECRET-PROMPT-TEXT") {
		t.Fatal("the panic value leaked to the caller")
	}
	files := crashReports(t, dir)
	if len(files) != 1 {
		t.Fatalf("reports = %d, want 1", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if strings.Contains(string(data), "SECRET-PROMPT-TEXT") || !strings.Contains(string(data), `"site":"wsapi.dispatch"`) {
		t.Fatalf("report leaked the message or lost its site: %s", data)
	}
	// the connection lives
	if pong := call("2", "ping"); pong.Error != nil || !strings.Contains(string(pong.Result), "pong") {
		t.Fatalf("ping after panic = %+v", pong)
	}
}

func crashJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// eventPanicTransport panics when asked to send an event notification and
// records that it was closed.
type eventPanicTransport struct {
	Transport
	closed chan struct{}
	once   sync.Once
}

func (t *eventPanicTransport) Send(data []byte) error {
	if strings.Contains(string(data), `"event"`) {
		panic("SECRET-EVENT-TEXT")
	}
	return t.Transport.Send(data)
}

func (t *eventPanicTransport) Close() error {
	t.once.Do(func() { close(t.closed) })
	return t.Transport.Close()
}

// crash-ws-pump-panic-closes-connection: a panic in a connection's event pump
// is recorded and the connection is closed, instead of the socket staying open
// with its event stream silently dead.
func TestCrashWSPumpPanicClosesConnection(t *testing.T) {
	dir := crashDir(t)
	client, server := newPipeTransportPair()
	tr := &eventPanicTransport{Transport: server, closed: make(chan struct{})}
	c := newConn(tr, map[string]handlerFunc{})
	c.eventsBus = newEventBus()
	c.eventsSub = newSubscriber()
	c.eventsSub.subscribe([]string{"t"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.serve(ctx)
	defer client.Close()

	deadline := time.Now().Add(3 * time.Second)
	for !c.eventsBus.HasSubscribers("t") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	c.eventsBus.Publish(Event{Topic: "t", Payload: "x"})
	select {
	case <-tr.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection stayed open after its event pump panicked")
	}
	files := crashReports(t, dir)
	if len(files) != 1 {
		t.Fatalf("reports = %d, want 1", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"site":"wsapi.pumpEvents"`) || strings.Contains(string(data), "SECRET-EVENT-TEXT") {
		t.Fatalf("report lost its site or leaked the panic text: %s", data)
	}
}
