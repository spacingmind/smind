//go:build linux || darwin

package runsock

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// unixPair returns both ends of a connected unix socket pair created over a
// listener in a short temporary path (unix socket paths are limited to ~104 bytes).
func unixPair(t *testing.T) (client, server *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rs")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	clientAny, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client = clientAny.(*net.UnixConn)
	t.Cleanup(func() { client.Close() })

	serverAny, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	server = serverAny.(*net.UnixConn)
	t.Cleanup(func() { server.Close() })
	return client, server
}

func TestSystemPeerReaderReportsThisProcess(t *testing.T) {
	r := SystemPeerReader()
	wantUID := uint32(os.Getuid())
	wantPID := os.Getpid()

	client, server := unixPair(t)

	for name, conn := range map[string]*net.UnixConn{
		"accepted end": server,
		"client end":   client,
	} {
		got, err := r.Peer(conn)
		if err != nil {
			t.Fatalf("%s: Peer() error = %v", name, err)
		}
		if got.UID != wantUID {
			t.Errorf("%s: UID = %d, want %d", name, got.UID, wantUID)
		}
		if got.PID != wantPID {
			t.Errorf("%s: PID = %d, want %d", name, got.PID, wantPID)
		}
	}
}

func TestSystemPeerReaderRefusesANonUnixConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if _, err := SystemPeerReader().Peer(conn); err == nil {
		t.Fatal("Peer() on a TCP connection = nil error, want error")
	}
}

func TestSystemPeerReaderClosedConn(t *testing.T) {
	client, server := unixPair(t)

	client.Close()
	if _, err := SystemPeerReader().Peer(client); err == nil {
		t.Error("Peer() on closed client conn = nil error, want error")
	}

	server.Close()
	if _, err := SystemPeerReader().Peer(server); err == nil {
		t.Error("Peer() on closed server conn = nil error, want error")
	}
}
