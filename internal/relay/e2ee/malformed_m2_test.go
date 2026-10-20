package e2ee

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestClientRejectsMalformedM2: the client-side table of malformed daemon
// hellos. Each is a protocol error, the client writes nothing beyond M1, and
// nothing is derived.
func TestClientRejectsMalformedM2(t *testing.T) {
	daemon := newScriptedDaemon(t, seedFrom(0x01), seedFrom(0x40))
	cpub := privFromSeed(t, seedFrom(0xa0)).PublicKey().Bytes()
	m2, _, _, _ := daemon.reply(t, cpub)
	role := func(r byte) []byte { return append([]byte{m2[0], r}, m2[2:]...) }

	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"role is the client's", role(byte(RoleMobile))},
		{"unknown role", role(9)},
		{"length +1", append(bytes.Clone(m2), 0)},
		{"length -1", bytes.Clone(m2[:len(m2)-1])},
		{"empty", nil},
		{"reflected M1", payloadOfHello(cpub)},
		{"daemon_pub too short", m2[:2+16]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, peer := newMemPipe()
			client := newClientChannel(t, conn)
			if _, err := peer.Write(rawFrame(frameHello, tc.payload)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			if err := client.Handshake(ctx); !errors.Is(err, ErrProtocol) {
				t.Fatalf("Handshake = %v, want ErrProtocol", err)
			}
			if client.Established() || len(conn.written()) != 1 {
				t.Fatalf("client established or wrote %d frames", len(conn.written()))
			}
		})
	}
}
