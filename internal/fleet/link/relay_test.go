package link

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/relaytest"
	"github.com/spacingmind/smind/internal/wsapi"
)

// idleServer accepts a device session and holds it until the peer goes away.
type idleServer struct{}

func (idleServer) ServeTransport(ctx context.Context, tr wsapi.Transport) {
	for {
		if _, err := tr.Receive(); err != nil {
			return
		}
	}
}

// startTestHub runs a real bridge.Hub against r as a daemon with kp serving
// srv (idleServer when nil), with a device registry under regDir, and waits
// until it is connected to the relay. The hub is stopped at test end.
func startTestHub(t *testing.T, r *relaytest.Relay, kp *e2ee.KeyPair, regDir string, srv bridge.Server, opts bridge.Options) (*bridge.Hub, *devices.Registry) {
	t.Helper()
	reg, err := devices.Open(filepath.Join(regDir, "relay-devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	connected := make(chan struct{})
	var once sync.Once
	opts.Registry = reg
	opts.Logf = func(f string, a ...any) {
		if strings.Contains(f, "connected to") {
			mu.Lock()
			once.Do(func() { close(connected) })
			mu.Unlock()
		}
	}
	hub := bridge.NewHub(opts)
	if srv == nil {
		srv = idleServer{}
	}
	cfg := bridge.Config{
		RelayAddress: r.NativeAddr, WorkspaceID: r.HostID,
		SecretHex: hex.EncodeToString(r.Secret), Fingerprint: r.Fingerprint,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = hub.Run(ctx, cfg, kp, srv)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("hub did not stop")
		}
	})
	select {
	case <-connected:
	case <-time.After(15 * time.Second):
		t.Fatal("hub did not connect to the relay")
	}
	return hub, reg
}

func newTestKeyPair(t *testing.T) *e2ee.KeyPair {
	t.Helper()
	kp, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

const relayPasswordCanary = "pw-canary-7f3a9c"

func relayTestKey(t *testing.T) *identity.Key {
	t.Helper()
	k, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func relayTestTransport(r *relaytest.Relay, daemonPub []byte) Transport {
	return Transport{Kind: KindRelay, Relay: &RelayRoute{
		Endpoint:    r.NativeAddr,
		RelayHostID: r.HostID,
		Fingerprint: r.Fingerprint,
		DaemonID:    "daemon",
		DaemonPub:   base64.RawURLEncoding.EncodeToString(daemonPub),
	}}
}

func relayCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRelayFingerprintMismatchFailsClosed(t *testing.T) {
	r := relaytest.StartRelay(t)
	kp := newTestKeyPair(t)
	startTestHub(t, r, kp, t.TempDir(), nil, bridge.Options{})
	tr := relayTestTransport(r, kp.Public())
	tr.Relay.Fingerprint = strings.Repeat("ab", 32)
	entry := Entry{RelaySecret: hex.EncodeToString(r.Secret)}
	if _, err := DialRelay(relayCtx(t), tr, relayTestKey(t), entry, "fleet"); err == nil {
		t.Fatal("DialRelay accepted a relay with a different certificate")
	}
}

func TestRelayRefusesUnpinnedFingerprint(t *testing.T) {
	r := relaytest.StartRelay(t)
	tr := relayTestTransport(r, make([]byte, 32))
	tr.Relay.Fingerprint = ""
	entry := Entry{RelaySecret: hex.EncodeToString(r.Secret)}
	_, err := DialRelay(relayCtx(t), tr, relayTestKey(t), entry, "fleet")
	if err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("err = %v, want an unpinned-route refusal", err)
	}
}

func TestRelayWrongDaemonKeyFailsClosed(t *testing.T) {
	r := relaytest.StartRelay(t)
	kp := newTestKeyPair(t)
	startTestHub(t, r, kp, t.TempDir(), nil, bridge.Options{})
	wrong := make([]byte, 32)
	copy(wrong, kp.Public())
	wrong[0] ^= 0xff
	tr := relayTestTransport(r, wrong)
	entry := Entry{RelaySecret: hex.EncodeToString(r.Secret)}
	if _, err := DialRelay(relayCtx(t), tr, relayTestKey(t), entry, "fleet"); err == nil {
		t.Fatal("DialRelay trusted a daemon whose key differs from the pin")
	}
}

func TestDialRelayErrorsNeverContainSecret(t *testing.T) {
	r := relaytest.StartRelay(t)
	kp := newTestKeyPair(t)
	startTestHub(t, r, kp, t.TempDir(), nil, bridge.Options{})
	secretHex := hex.EncodeToString(r.Secret)
	entry := Entry{RelaySecret: secretHex, Password: relayPasswordCanary}
	key := relayTestKey(t)

	wrong := make([]byte, 32)
	copy(wrong, kp.Public())
	wrong[0] ^= 0xff
	badFP := relayTestTransport(r, kp.Public())
	badFP.Relay.Fingerprint = strings.Repeat("cd", 32)
	unpinned := relayTestTransport(r, kp.Public())
	unpinned.Relay.Fingerprint = ""
	badSecret := Entry{RelaySecret: "zz" + secretHex, Password: relayPasswordCanary}
	wrongSecret := Entry{RelaySecret: strings.Repeat("00", 32), Password: relayPasswordCanary}

	cases := []struct {
		name  string
		tr    Transport
		entry Entry
	}{
		{"wrong daemon key", relayTestTransport(r, wrong), entry},
		{"bad fingerprint", badFP, entry},
		{"unpinned", unpinned, entry},
		{"secret not hex", relayTestTransport(r, kp.Public()), badSecret},
		{"wrong secret", relayTestTransport(r, kp.Public()), wrongSecret},
	}
	for _, c := range cases {
		_, err := DialRelay(relayCtx(t), c.tr, key, c.entry, "fleet")
		if err == nil {
			t.Fatalf("%s: expected an error", c.name)
		}
		for _, bad := range []string{secretHex, c.entry.RelaySecret, relayPasswordCanary} {
			if strings.Contains(err.Error(), bad) {
				t.Fatalf("%s: error leaks a credential: %v", c.name, err)
			}
		}
	}
}
