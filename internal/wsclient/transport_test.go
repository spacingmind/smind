package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeTransport struct {
	in     chan []byte
	out    chan []byte
	once   sync.Once
	closed chan struct{}
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{in: make(chan []byte, 16), out: make(chan []byte, 16), closed: make(chan struct{})}
}

func (f *fakeTransport) Receive() ([]byte, error) {
	select {
	case d := <-f.in:
		return d, nil
	case <-f.closed:
		return nil, errors.New("closed")
	}
}

func (f *fakeTransport) Send(d []byte) error {
	select {
	case f.out <- d:
		return nil
	case <-f.closed:
		return errors.New("closed")
	}
}

func (f *fakeTransport) Close() error { f.once.Do(func() { close(f.closed) }); return nil }

func TestNewFromTransportCallsAndEvents(t *testing.T) {
	ft := newFakeTransport()
	go func() {
		for {
			select {
			case d := <-ft.out:
				var e envelope
				_ = json.Unmarshal(d, &e)
				if e.ID == "" {
					continue
				}
				if e.Method == "stream" {
					ft.in <- []byte(`{"id":"` + e.ID + `","event":"tick","params":{"n":1}}`)
				}
				ft.in <- []byte(`{"id":"` + e.ID + `","result":{"ok":true}}`)
			case <-ft.closed:
				return
			}
		}
	}()
	c := NewFromTransport(ft)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out struct{ OK bool }
	if err := c.Call(ctx, "ping", nil, &out); err != nil || !out.OK {
		t.Fatalf("Call: %v %+v", err, out)
	}
	var events []string
	if err := c.CallStream(ctx, "stream", nil, func(ev string, _ json.RawMessage) { events = append(events, ev) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0] != "tick" {
		t.Fatalf("events = %v", events)
	}
	_ = c.Close()
	select {
	case <-ft.closed:
	default:
		t.Fatal("Close did not close the transport")
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not fire")
	}
}

func TestNewFromTransportDoneOnReceiveError(t *testing.T) {
	ft := newFakeTransport()
	c := NewFromTransport(ft)
	_ = ft.Close()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not fire when Receive errored")
	}
}
