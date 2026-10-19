package e2ee

// ADR-0035 invariants. They replace the retired TestE2EECoreUnchanged.

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"
)

// The ADR-0035 §7 reference values, asserted literally.
const (
	adrClientEphPub = "605a725d2a4adfeeb1a29e17edd621c1b7593ee8cdbc44ac6c4ab6e2f805d23c"
	adrDaemonPub    = "07a37cbc142093c8b755dc1b10e86cb426374ad16aa853ed0bdfc0b2b86d1c7c"

	adrAEphPub = "79a631eede1bf9c98f12032cdeadd0e7a079398fc786b88cc846ec89af85a51a"
	adrATh     = "2d1486c943e0176e9d685177f5b8713ca0aaf78da9f26d3ea6fd301c492a18df"
	adrAKd2m   = "abfe27aeb9a17b57b24c49f36cbd0172825436e677d7f31c64de264cd3871d27"
	adrAKm2d   = "9473838513956c5e92f603e2c7b93c9f2156e67055bd556bc004f3090f728ac1"
	adrARd     = "a5686ea0af98582a9193c55f4cfa612c63f26d2a4c070bbe5a2befc367196ba3"
	adrARc     = "231d5e288861ca6336511c5e6f1742bd888c48ef5910aa5f821aa315ae987dc1"

	adrBEphPub = "675dd574ed7789310b3d2e7681f3790b466c773b1521fecf36577958371ea52f"
	adrBTh     = "450564e3ccbd05cf0e6cc4a8aceb4ff83314c49ea69028607df2d784c4e24106"
	adrBKd2m   = "0f61785329080dbc6d6e2a3d9b8372c61c2644d90d72df1e472942c885301097"
	adrBKm2d   = "d574b5f6ca341625895b4f219650e45575acbea93e023e265ec15982d04c62cc"

	// adrEsPrefix/adrEsSuffix are the only parts of `es` the ADR prints
	// ("caa14fed…1759").
	adrEsPrefix = "caa14fed"
	adrEsSuffix = "1759"
)

// refSession is the key schedule run from the ADR's fixed seeds.
type refSession struct {
	es, ee                  []byte
	daemonEphPub            []byte
	sched                   *schedule
	rd, rc                  []byte
	clientEphPub, daemonPub []byte
}

func runReference(t testing.TB, daemonEphSeed []byte) *refSession {
	t.Helper()
	client := privFromSeed(t, seedFrom(0xa0))
	static := privFromSeed(t, seedFrom(0x01))
	eph := privFromSeed(t, daemonEphSeed)
	cpub, spub, epub := client.PublicKey().Bytes(), static.PublicKey().Bytes(), eph.PublicKey().Bytes()
	// Client-side DH: X25519(c_eph_priv, d_static_pub) and (c_eph_priv, d_eph_pub).
	es, err := dh(client, spub)
	if err != nil {
		t.Fatal(err)
	}
	ee, err := dh(client, epub)
	if err != nil {
		t.Fatal(err)
	}
	sched, err := deriveSchedule(es, ee, spub, epub, cpub)
	if err != nil {
		t.Fatal(err)
	}
	return &refSession{
		es: es, ee: ee, daemonEphPub: epub, sched: sched,
		rd: confirmTag(sched.confirmD, sched.th), rc: confirmTag(sched.confirmM, sched.th),
		clientEphPub: cpub, daemonPub: spub,
	}
}

func assertHex(t testing.TB, name string, got []byte, want string) {
	t.Helper()
	if hex.EncodeToString(got) != want {
		t.Errorf("%s = %x, want %s", name, got, want)
	}
}

func TestE2EEV2KeyScheduleVectors(t *testing.T) {
	a := runReference(t, seedFrom(0x40))
	assertHex(t, "client_eph_pub", a.clientEphPub, adrClientEphPub)
	assertHex(t, "daemon_pub", a.daemonPub, adrDaemonPub)

	assertHex(t, "A daemon_eph_pub", a.daemonEphPub, adrAEphPub)
	assertHex(t, "A th", a.sched.th, adrATh)
	assertHex(t, "A k_d2m", a.sched.d2m, adrAKd2m)
	assertHex(t, "A k_m2d", a.sched.m2d, adrAKm2d)
	assertHex(t, "A R_d", a.rd, adrARd)
	assertHex(t, "A R_c", a.rc, adrARc)

	b := runReference(t, seedFrom(0x60))
	assertHex(t, "B daemon_eph_pub", b.daemonEphPub, adrBEphPub)
	assertHex(t, "B th", b.sched.th, adrBTh)
	assertHex(t, "B k_d2m", b.sched.d2m, adrBKd2m)
	assertHex(t, "B k_m2d", b.sched.m2d, adrBKm2d)

	// es is the same in both sessions; only ee changes the keys.
	if !bytes.Equal(a.es, b.es) {
		t.Error("es differs between the two sessions")
	}
	if es := hex.EncodeToString(a.es); es[:len(adrEsPrefix)] != adrEsPrefix || es[len(es)-len(adrEsSuffix):] != adrEsSuffix {
		t.Errorf("es = %s, want %s…%s", es, adrEsPrefix, adrEsSuffix)
	}
	if bytes.Equal(a.ee, b.ee) {
		t.Error("ee must differ between the two sessions")
	}

	// And the full Channels, driven by the same seeds, put exactly those bytes
	// on the wire.
	p := seededPair(t, seedFrom(0xa0), seedFrom(0x01), seedFrom(0x40))
	p.handshake(t)
	assertHex(t, "channel th", p.mobile.TranscriptHash(), adrATh)
	d := p.daemonConn.written()
	if len(d) != 2 || !bytes.Equal(d[1], rawFrame(frameReady, a.rd)) {
		t.Errorf("daemon wire = %d frames, want M2 then the reference R_d", len(d))
	}
	m := p.mobileConn.written()
	if len(m) != 2 || !bytes.Equal(m[1], rawFrame(frameReady, a.rc)) {
		t.Errorf("client wire = %d frames, want M1 then the reference R_c", len(m))
	}
	counter, ct, err := p.daemon.Session().Seal([]byte("x"))
	if err != nil || counter != 0 {
		t.Fatal(err)
	}
	if _, err := p.mobile.Session().Open(0, ct); err != nil {
		t.Errorf("client cannot open a frame sealed under the reference keys: %v", err)
	}
}

// seededPair builds a pair whose three keys come from fixed seeds.
func seededPair(t *testing.T, clientSeed, staticSeed, daemonEphSeed []byte) *pair {
	t.Helper()
	p := newPairWithKey(t, keyPairFromSeed(t, staticSeed))
	seedEphemeral(t, p.mobile, clientSeed)
	seedEphemeral(t, p.daemon, daemonEphSeed)
	return p
}

// TestTranscriptHashIsTheHKDFSalt: th is the HKDF salt of all four outputs
// and covers daemon_eph_pub.
func TestTranscriptHashIsTheHKDFSalt(t *testing.T) {
	a := runReference(t, seedFrom(0x40))
	ikm := append(bytes.Clone(a.es), a.ee...)
	for _, c := range []struct {
		name string
		info string
		got  []byte
	}{
		{"k_d2m", "smind relay e2ee v2 daemon->mobile", a.sched.d2m},
		{"k_m2d", "smind relay e2ee v2 mobile->daemon", a.sched.m2d},
		{"confirm_d", "smind relay e2ee v2 confirm daemon", a.sched.confirmD},
		{"confirm_m", "smind relay e2ee v2 confirm mobile", a.sched.confirmM},
	} {
		want, err := hkdf.Key(sha256.New, ikm, a.sched.th, c.info, 32)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(c.got, want) {
			t.Errorf("%s is not HKDF(ikm = es‖ee, salt = th, info = %q)", c.name, c.info)
		}
	}
	want := sha256.Sum256(append(append(append([]byte("smind relay e2ee v2"), a.daemonPub...), a.daemonEphPub...), a.clientEphPub...))
	if !bytes.Equal(a.sched.th, want[:]) {
		t.Error("th is not SHA-256(label ‖ daemon_pub ‖ daemon_eph_pub ‖ client_eph_pub)")
	}
	b := runReference(t, seedFrom(0x60))
	if bytes.Equal(a.sched.th, b.sched.th) {
		t.Error("th does not depend on daemon_eph_pub")
	}
}

// replayInto feeds a recorded M1 to a fresh daemon channel (optionally with a
// seeded ephemeral) and returns the channel, its wire record and the
// handshake result.
func replayInto(t *testing.T, kp *KeyPair, ephSeed, m1 []byte, extra ...[]byte) (*Channel, *memConn, <-chan error) {
	t.Helper()
	conn, attacker := newMemPipe()
	daemon := newDaemonChannel(t, conn, kp)
	if ephSeed != nil {
		seedEphemeral(t, daemon, ephSeed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Handshake(ctx) }()
	for _, f := range append([][]byte{m1}, extra...) {
		if _, err := attacker.Write(f); err != nil {
			t.Fatal(err)
		}
	}
	return daemon, conn, errCh
}

// refM1 is the recorded client hello of the ADR reference sessions.
func refM1(t *testing.T) []byte {
	t.Helper()
	return rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, privFromSeed(t, seedFrom(0xa0)).PublicKey().Bytes()))
}

func TestReplayedClientHandshakeDerivesFreshKeys(t *testing.T) {
	kp := keyPairFromSeed(t, seedFrom(0x01))
	a := runReference(t, seedFrom(0x40))
	b := runReference(t, seedFrom(0x60))
	for name, pair := range map[string][2][]byte{
		"th": {a.sched.th, b.sched.th}, "k_d2m": {a.sched.d2m, b.sched.d2m}, "k_m2d": {a.sched.m2d, b.sched.m2d},
		"confirm_d": {a.sched.confirmD, b.sched.confirmD}, "confirm_m": {a.sched.confirmM, b.sched.confirmM},
		"R_d": {a.rd, b.rd}, "R_c": {a.rc, b.rc},
	} {
		if bytes.Equal(pair[0], pair[1]) {
			t.Errorf("%s is the same in the original and the replayed session", name)
		}
	}

	// The replayed M1 reaches a daemon channel; it answers with ITS fresh
	// ephemeral (M2 carries session B's daemon_eph_pub), and the handshake
	// fails without a valid R_c, even when the attacker sends session A's.
	_, conn, errCh := replayInto(t, kp, seedFrom(0x60), refM1(t), rawFrame(frameReady, a.rc))
	if err := <-errCh; !errors.Is(err, ErrHandshakeConfirm) {
		t.Fatalf("replayed handshake with the old R_c = %v, want ErrHandshakeConfirm", err)
	}
	w := conn.written()
	if len(w) != 2 {
		t.Fatalf("daemon wrote %d frames, want M2 and R_d", len(w))
	}
	assertHex(t, "replayed M2 daemon_eph_pub", w[0][5+2+32:], adrBEphPub)
	if !bytes.Equal(w[1], rawFrame(frameReady, b.rd)) {
		t.Error("replayed R_d is not the session-B tag")
	}
}

func TestReplayedClientHandshakeWritesNoDataFrame(t *testing.T) {
	kp := keyPairFromSeed(t, seedFrom(0x01))
	for name, extra := range map[string][][]byte{
		"silent attacker":     nil,
		"data instead of R_c": {rawFrame(frameData, make([]byte, 40))},
		"empty ready":         {rawFrame(frameReady, nil)},
		"old R_c":             {rawFrame(frameReady, runReference(t, seedFrom(0x40)).rc)},
	} {
		t.Run(name, func(t *testing.T) {
			daemon, conn, errCh := replayInto(t, kp, nil, refM1(t), extra...)
			if err := <-errCh; err == nil {
				t.Fatal("replayed handshake succeeded")
			}
			if daemon.Established() {
				t.Fatal("daemon established on a replay")
			}
			if err := daemon.Send([]byte("leak")); err == nil {
				t.Fatal("daemon Send succeeded on a replayed handshake")
			}
			w := conn.written()
			if len(w) != 2 || w[0][4] != frameHello || w[1][4] != frameReady {
				t.Fatalf("daemon wrote %d frames, want exactly M2 and R_d", len(w))
			}
			for _, f := range w {
				if f[4] == frameData {
					t.Fatal("daemon wrote a data frame to a replayed handshake")
				}
			}
		})
	}
}

// TestNoNonceReuseAcrossSessions: two sessions from the same client ephemeral
// never share a (key, nonce): sealing the same plaintext at counter 0 differs.
func TestNoNonceReuseAcrossSessions(t *testing.T) {
	seal := func(daemonEphSeed []byte) (d2m, m2d []byte) {
		p := seededPair(t, seedFrom(0xa0), seedFrom(0x01), daemonEphSeed)
		p.handshake(t)
		_, d2m, err := p.daemon.Session().Seal([]byte("same plaintext"))
		if err != nil {
			t.Fatal(err)
		}
		_, m2d, err = p.mobile.Session().Seal([]byte("same plaintext"))
		if err != nil {
			t.Fatal(err)
		}
		return d2m, m2d
	}
	d1, m1 := seal(seedFrom(0x40))
	d2, m2 := seal(seedFrom(0x60))
	if bytes.Equal(d1, d2) || bytes.Equal(m1, m2) {
		t.Fatal("two sessions sealed the same plaintext at counter 0 to the same ciphertext (key and nonce reuse)")
	}
}

func TestDaemonEphemeralIsFreshPerHandshake(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var ths [][]byte
	for range 8 {
		_, conn, errCh := replayInto(t, kp, nil, refM1(t))
		<-errCh
		w := conn.written()
		eph := string(w[0][5+2+32:])
		if seen[eph] {
			t.Fatal("daemon reused its ephemeral key across Handshake calls")
		}
		seen[eph] = true
		ths = append(ths, TranscriptHash(kp.Public(), []byte(eph), privFromSeed(t, seedFrom(0xa0)).PublicKey().Bytes()))
	}
	for i := range ths {
		for j := i + 1; j < len(ths); j++ {
			if bytes.Equal(ths[i], ths[j]) {
				t.Fatal("two handshakes of the same recorded M1 share a transcript hash")
			}
		}
	}
}

// TestClientEphemeralNeverReused: every client Handshake draws a new
// ephemeral, including a retry after a failed handshake.
func TestClientEphemeralNeverReused(t *testing.T) {
	seen := map[string]bool{}
	for range 8 {
		conn, peer := newMemPipe()
		client := newClientChannel(t, conn)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_ = client.Handshake(ctx) // the peer is silent: fails at the deadline
		cancel()
		_ = peer.Close()
		w := conn.written()
		if len(w) != 1 {
			t.Fatalf("client wrote %d frames, want M1 only", len(w))
		}
		eph := string(w[0][7:])
		if seen[eph] {
			t.Fatal("client reused its ephemeral key across handshakes")
		}
		seen[eph] = true
	}
}

func TestTamperedDaemonEphemeralFailsHandshake(t *testing.T) {
	staticSeed, ephSeed, clientSeed := seedFrom(0x01), seedFrom(0x40), seedFrom(0xa0)
	conn, peer := newMemPipe()
	client := newClientChannel(t, conn)
	seedEphemeral(t, client, clientSeed)
	m2, rd, _, _ := newScriptedDaemon(t, staticSeed, ephSeed).reply(t, privFromSeed(t, clientSeed).PublicKey().Bytes())
	m2[2+32] ^= 0x01 // one bit of daemon_eph_pub
	for _, f := range [][]byte{rawFrame(frameHello, m2), rawFrame(frameReady, rd)} {
		if _, err := peer.Write(f); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := client.Handshake(ctx); !errors.Is(err, ErrHandshakeConfirm) {
		t.Fatalf("Handshake with a tampered daemon ephemeral = %v, want ErrHandshakeConfirm", err)
	}
	if n := len(conn.written()); n != 1 {
		t.Fatalf("client wrote %d frames, want only M1 (no R_c)", n)
	}
}

// TestWrongStaticKeyFailsHandshake: an unpinned client talking to a daemon
// whose static key it derived against wrongly (the M2 claims another static
// key than the one R_d was built from) fails key confirmation; with the pin it
// fails before any DH.
func TestWrongStaticKeyFailsHandshake(t *testing.T) {
	clientSeed := seedFrom(0xa0)
	cpub := privFromSeed(t, clientSeed).PublicKey().Bytes()
	real := newScriptedDaemon(t, seedFrom(0x01), seedFrom(0x40))
	_, rd, _, _ := real.reply(t, cpub)
	other := privFromSeed(t, seedFrom(0x80)).PublicKey().Bytes()
	m2 := append([]byte{ProtocolVersion, byte(RoleDaemon)}, other...)
	m2 = append(m2, real.eph.PublicKey().Bytes()...)

	run := func(opts ...HandshakeOption) (error, *memConn) {
		conn, peer := newMemPipe()
		client := newClientChannel(t, conn)
		seedEphemeral(t, client, clientSeed)
		for _, f := range [][]byte{rawFrame(frameHello, m2), rawFrame(frameReady, rd)} {
			if _, err := peer.Write(f); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		return client.Handshake(ctx, opts...), conn
	}
	err, conn := run()
	if !errors.Is(err, ErrHandshakeConfirm) {
		t.Fatalf("unpinned = %v, want ErrHandshakeConfirm", err)
	}
	if len(conn.written()) != 1 {
		t.Error("client sent R_c to a daemon it could not confirm")
	}
	err, conn = run(WithExpectedPeerKey(real.static.PublicKey().Bytes()))
	if !errors.Is(err, ErrPeerKeyMismatch) {
		t.Fatalf("pinned = %v, want ErrPeerKeyMismatch", err)
	}
	if len(conn.written()) != 1 {
		t.Error("client sent more than M1 after a pin mismatch")
	}
}

// TestForgedDaemonReadyFails: the real daemon_pub and an attacker-chosen R_d
// (no static private key), and an attacker that brings its own static key
// against a pinned client.
func TestForgedDaemonReadyFails(t *testing.T) {
	clientSeed := seedFrom(0xa0)
	cpub := privFromSeed(t, clientSeed).PublicKey().Bytes()
	real := newScriptedDaemon(t, seedFrom(0x01), seedFrom(0x40))
	m2, goodRd, _, sched := real.reply(t, cpub)

	forged := confirmTag(make([]byte, 32), sched.th) // MAC without the static key
	attacker := newScriptedDaemon(t, seedFrom(0x80), seedFrom(0x90))
	am2, ard, _, _ := attacker.reply(t, cpub)
	pin := WithExpectedPeerKey(real.static.PublicKey().Bytes())

	for _, tc := range []struct {
		name   string
		frames [][]byte
		want   error
	}{
		{"real daemon_pub, forged R_d", [][]byte{rawFrame(frameHello, m2), rawFrame(frameReady, forged)}, ErrHandshakeConfirm},
		{"real daemon_pub, R_d bit flipped", [][]byte{rawFrame(frameHello, m2), rawFrame(frameReady, append([]byte{goodRd[0] ^ 1}, goodRd[1:]...))}, ErrHandshakeConfirm},
		{"attacker static key against the pin", [][]byte{rawFrame(frameHello, am2), rawFrame(frameReady, ard)}, ErrPeerKeyMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, peer := newMemPipe()
			client := newClientChannel(t, conn)
			seedEphemeral(t, client, clientSeed)
			for _, f := range tc.frames {
				if _, err := peer.Write(f); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			if err := client.Handshake(ctx, pin); !errors.Is(err, tc.want) {
				t.Fatalf("Handshake = %v, want %v", err, tc.want)
			}
			if client.Established() || len(conn.written()) != 1 {
				t.Fatal("client confirmed or wrote after a forged daemon")
			}
		})
	}
}

func TestDowngradeRefused(t *testing.T) {
	pub := privFromSeed(t, seedFrom(0xa0)).PublicKey().Bytes()
	// v1 hellos: version 1, daemon or client role, 34 bytes. A v1 payload of
	// any shape is rejected on the version byte before length or role.
	v1Daemon := append([]byte{1, byte(RoleDaemon)}, pub...)
	v1Client := append([]byte{1, byte(RoleMobile)}, pub...)

	t.Run("daemon refuses a v1 client hello", func(t *testing.T) {
		for name, payload := range map[string][]byte{"v1 client hello": v1Client, "version-only": {1}, "garbage": {1, 9, 9}} {
			conn, peer := newMemPipe()
			daemon := newDaemonChannel(t, conn, keyPairFromSeed(t, seedFrom(0x01)))
			if _, err := peer.Write(rawFrame(frameHello, payload)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			err := daemon.Handshake(ctx)
			cancel()
			var uv *UnsupportedVersionError
			if !errors.Is(err, ErrUnsupportedVersion) || !errors.As(err, &uv) || uv.Peer != 1 {
				t.Fatalf("%s: Handshake = %v, want UnsupportedVersionError{Peer: 1}", name, err)
			}
			if len(conn.written()) != 0 || !daemon.Closed() {
				t.Fatalf("%s: daemon must close without replying", name)
			}
		}
	})
	t.Run("client refuses a v1 daemon hello", func(t *testing.T) {
		for name, payload := range map[string][]byte{"v1 daemon hello": v1Daemon, "newer": {3, 1}} {
			conn, peer := newMemPipe()
			client := newClientChannel(t, conn)
			if _, err := peer.Write(rawFrame(frameHello, payload)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			err := client.Handshake(ctx)
			cancel()
			var uv *UnsupportedVersionError
			if !errors.Is(err, ErrUnsupportedVersion) || !errors.As(err, &uv) || uv.Peer != payload[0] {
				t.Fatalf("%s: Handshake = %v, want UnsupportedVersionError{Peer: %d}", name, err, payload[0])
			}
			if len(conn.written()) != 1 || client.Established() {
				t.Fatalf("%s: client wrote beyond M1 or established", name)
			}
		}
	})
}

func TestEmptyReadyIsProtocolError(t *testing.T) {
	clientSeed := seedFrom(0xa0)
	cpub := privFromSeed(t, clientSeed).PublicKey().Bytes()
	t.Run("client gets an empty R_d", func(t *testing.T) {
		m2, _, _, _ := newScriptedDaemon(t, seedFrom(0x01), seedFrom(0x40)).reply(t, cpub)
		conn, peer := newMemPipe()
		client := newClientChannel(t, conn)
		seedEphemeral(t, client, clientSeed)
		for _, f := range [][]byte{rawFrame(frameHello, m2), rawFrame(frameReady, nil)} {
			if _, err := peer.Write(f); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := client.Handshake(ctx); !errors.Is(err, ErrProtocol) {
			t.Fatalf("Handshake = %v, want ErrProtocol", err)
		}
	})
	t.Run("daemon gets an empty R_c", func(t *testing.T) {
		_, errCh := func() (*memConn, <-chan error) {
			_, conn, errCh := replayInto(t, keyPairFromSeed(t, seedFrom(0x01)), seedFrom(0x40), refM1(t), rawFrame(frameReady, nil))
			return conn, errCh
		}()
		if err := <-errCh; !errors.Is(err, ErrProtocol) {
			t.Fatalf("Handshake = %v, want ErrProtocol", err)
		}
	})
}

// TestLowOrderEphemeralRejected: both DH outputs are checked, on both ends.
func TestLowOrderEphemeralRejected(t *testing.T) {
	zero := make([]byte, PublicKeySize)
	t.Run("daemon: low-order client ephemeral", func(t *testing.T) {
		_, conn, errCh := replayInto(t, keyPairFromSeed(t, seedFrom(0x01)), nil, rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, zero)))
		if err := <-errCh; !errors.Is(err, ErrKeyAgreement) {
			t.Fatalf("Handshake = %v, want ErrKeyAgreement", err)
		}
		if len(conn.written()) != 0 {
			t.Error("daemon answered a low-order client ephemeral")
		}
	})
	real := newScriptedDaemon(t, seedFrom(0x01), seedFrom(0x40))
	for name, m2 := range map[string][]byte{
		"client: low-order daemon ephemeral (ee)": append(append([]byte{ProtocolVersion, byte(RoleDaemon)}, real.static.PublicKey().Bytes()...), zero...),
		"client: low-order daemon static (es)":    append(append([]byte{ProtocolVersion, byte(RoleDaemon)}, zero...), real.eph.PublicKey().Bytes()...),
	} {
		t.Run(name, func(t *testing.T) {
			conn, peer := newMemPipe()
			client := newClientChannel(t, conn)
			if _, err := peer.Write(rawFrame(frameHello, m2)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			if err := client.Handshake(ctx); !errors.Is(err, ErrKeyAgreement) {
				t.Fatalf("Handshake = %v, want ErrKeyAgreement", err)
			}
			if len(conn.written()) != 1 {
				t.Error("client wrote beyond M1")
			}
		})
	}
}

// TestNoDataBeforeClientReady: a data frame instead of R_c is a protocol
// error and the daemon never opens or seals anything.
func TestNoDataBeforeClientReady(t *testing.T) {
	daemon, conn, errCh := replayInto(t, keyPairFromSeed(t, seedFrom(0x01)), seedFrom(0x40), refM1(t), rawFrame(frameData, append(make([]byte, 8), make([]byte, 32)...)))
	if err := <-errCh; !errors.Is(err, ErrProtocol) {
		t.Fatalf("Handshake = %v, want ErrProtocol", err)
	}
	if daemon.Established() || daemon.Session() != nil {
		t.Fatal("a session exists before R_c verified")
	}
	if n := len(conn.written()); n != 2 {
		t.Fatalf("daemon wrote %d frames, want M2 and R_d only", n)
	}
}

// TestWrongRcIsHandshakeConfirm: an R_c computed from another session.
func TestWrongRcIsHandshakeConfirm(t *testing.T) {
	b := runReference(t, seedFrom(0x60))
	_, conn, errCh := replayInto(t, keyPairFromSeed(t, seedFrom(0x01)), seedFrom(0x40), refM1(t), rawFrame(frameReady, b.rc))
	if err := <-errCh; !errors.Is(err, ErrHandshakeConfirm) {
		t.Fatalf("Handshake = %v, want ErrHandshakeConfirm", err)
	}
	for _, f := range conn.written() {
		if f[4] == frameData {
			t.Fatal("daemon sent data after a wrong R_c")
		}
	}
}

// TestPublicAPIHasNoInitiatorKeyParameter pins the exported constructors'
// signatures at compile time: the client constructor takes no key, the daemon
// one takes exactly the daemon's static keypair.
func TestPublicAPIHasNoInitiatorKeyParameter(t *testing.T) {
	var _ func(io.ReadWriteCloser) (*Channel, error) = NewClientChannel
	var _ func(io.ReadWriteCloser, *KeyPair) (*Channel, error) = NewDaemonChannel
}
