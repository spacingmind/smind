package accounts

import (
	"net"
	"testing"
	"time"
)

func TestCallbackListenerIsLoopbackOnly(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"localhost:0", "127.0.0.1:0", "[::1]:0"} {
		t.Run("accepts "+addr, func(t *testing.T) {
			t.Parallel()
			s, err := newCallbackServer(addr, "/callback", "Test", "st")
			if err != nil {
				if addr == "[::1]:0" {
					t.Skipf("IPv6 loopback unavailable: %v", err)
				}
				t.Fatalf("newCallbackServer(%q) error = %v", addr, err)
			}
			defer s.close()
			host, _, err := net.SplitHostPort(s.listener.Addr().String())
			if err != nil {
				t.Fatalf("SplitHostPort() error = %v", err)
			}
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				t.Errorf("listener bound to %q, want a loopback address", s.listener.Addr())
			}
		})
	}

	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0", "192.0.2.1:0", "example.com:0", "localhost.evil:0", "127.0.0.1", ""} {
		t.Run("rejects "+addr, func(t *testing.T) {
			t.Parallel()
			s, err := newCallbackServer(addr, "/callback", "Test", "st")
			if err == nil {
				s.close()
				t.Fatalf("newCallbackServer(%q) error = nil, want a loopback-only rejection", addr)
			}
		})
	}

	// The guard also judges the address actually bound, not just the string
	// asked for (e.g. "localhost" resolving to a routable address).
	for _, tt := range []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:1", true},
		{"[::1]:1", true},
		{"192.0.2.1:1", false},
		{"0.0.0.0:1", false},
		{"[::]:1", false},
	} {
		tcp, err := net.ResolveTCPAddr("tcp", tt.addr)
		if err != nil {
			t.Fatalf("ResolveTCPAddr(%q) error = %v", tt.addr, err)
		}
		err = checkBoundLoopback(fakeListener{addr: tcp})
		if (err == nil) != tt.ok {
			t.Errorf("checkBoundLoopback(%s) error = %v, want ok=%v", tt.addr, err, tt.ok)
		}
	}

	// The real vendor addresses must pass the guard. Parse only: binding the
	// fixed vendor ports in a test would collide with a real login.
	for name, addr := range map[string]string{"anthropic": anthropicCallbackAddr, "openai": openaiCallbackAddr} {
		if err := checkLoopbackAddr(addr); err != nil {
			t.Errorf("%s callback address %q rejected: %v", name, addr, err)
		}
	}
}

func TestCallbackServerHardening(t *testing.T) {
	t.Parallel()

	s, err := newCallbackServer("127.0.0.1:0", "/callback", "Test", "st")
	if err != nil {
		t.Fatalf("newCallbackServer() error = %v", err)
	}
	if s.httpServer.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout = %v, want it set", s.httpServer.ReadHeaderTimeout)
	}

	// close is bounded even with a client holding a half-sent request open.
	conn, err := net.Dial("tcp", s.listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /callback HTTP/1.1\r\nHost: x\r\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() { s.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(callbackShutdownTimeout + 3*time.Second):
		t.Fatal("close() did not return in a bounded time")
	}
	if _, err := net.DialTimeout("tcp", s.listener.Addr().String(), time.Second); err == nil {
		t.Error("listener still accepts connections after close()")
	}
}

// fakeListener reports a fixed address; checkBoundLoopback only reads Addr.
type fakeListener struct {
	net.Listener
	addr net.Addr
}

func (f fakeListener) Addr() net.Addr { return f.addr }
