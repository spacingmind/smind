package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/listeners"
)

// With ListenersDir set, the relay records its native and gRPC-web listeners for
// as long as it runs (the agent browser's deny list reads them), and removes them
// when it stops.
func TestRun_RecordsItsListenersWhileItRuns(t *testing.T) {
	dir := t.TempDir()
	native, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	web, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenersDir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{DataDir: dir, Listener: native, GRPCWebListener: web, ListenersDir: listenersDir})
	}()
	want := map[string]string{"relay": native.Addr().String(), "grpcweb": web.Addr().String()}
	got := map[string]string{}
	for deadline := time.Now().Add(15 * time.Second); len(got) < 2 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		for _, e := range listeners.Read(listenersDir, listeners.ProcessAlive) {
			got[e.Name] = e.Addr
		}
	}
	for name, addr := range want {
		if got[name] != addr {
			t.Fatalf("the relay must record its %s listener %s while running: %v", name, addr, got)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not stop")
	}
	if left := listeners.Read(listenersDir, listeners.ProcessAlive); len(left) != 0 {
		t.Fatalf("a stopped relay leaves no listener record: %+v", left)
	}
}
