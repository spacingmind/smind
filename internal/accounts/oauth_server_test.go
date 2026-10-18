package accounts

import (
	"net"
	"testing"
)

func TestCallbackListenerIsLoopbackOnly(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"localhost:0", "127.0.0.1:0", "[::1]:0"} {
		t.Run("accepts "+addr, func(t *testing.T) {
			t.Parallel()
			s, err := newCallbackServer(addr, "/callback", "Test")
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

	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0", "192.0.2.1:0", "example.com:0", "127.0.0.1", ""} {
		t.Run("rejects "+addr, func(t *testing.T) {
			t.Parallel()
			s, err := newCallbackServer(addr, "/callback", "Test")
			if err == nil {
				s.close()
				t.Fatalf("newCallbackServer(%q) error = nil, want a loopback-only rejection", addr)
			}
		})
	}

	// The real vendor addresses must pass the guard. Parse only: binding the
	// fixed vendor ports in a test would collide with a real login.
	for name, addr := range map[string]string{"anthropic": anthropicCallbackAddr, "openai": openaiCallbackAddr} {
		if err := checkLoopbackAddr(addr); err != nil {
			t.Errorf("%s callback address %q rejected: %v", name, addr, err)
		}
	}
}
