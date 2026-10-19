package e2ee

// Cross-language vectors (ADR-0035 §7). The Go implementation generates
// testdata/e2ee_v2_vectors.json; the Rust and TypeScript clients read it.
//
//	go test ./internal/relay/e2ee -run TestWriteVectors -update

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"slices"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/e2ee_v2_vectors.json")

const vectorsPath = "testdata/e2ee_v2_vectors.json"

type vectorData struct {
	Counter      uint64 `json:"counter"`
	PlaintextHex string `json:"plaintext_hex"`
	FrameHex     string `json:"frame_hex"`
}

type vectorSession struct {
	DaemonEphPub string `json:"daemon_eph_pub"`
	Ee           string `json:"ee"`
	Th           string `json:"th"`
	KD2M         string `json:"k_d2m"`
	KM2D         string `json:"k_m2d"`
	ConfirmD     string `json:"confirm_d"`
	ConfirmM     string `json:"confirm_m"`
	M2FrameHex   string `json:"m2_frame_hex"`
	RDFrameHex   string `json:"r_d_frame_hex"`
	RCFrameHex   string `json:"r_c_frame_hex"`
}

type vectorNegative struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Endpoint    string   `json:"endpoint"` // "client" or "daemon": the end under test
	PinHex      string   `json:"pin_hex,omitempty"`
	FramesHex   []string `json:"frames_hex"`            // what the peer sends, in order, as raw wire bytes (usually full frames)
	CloseAfter  bool     `json:"close_after,omitempty"` // the peer's stream ends after frames_hex (a truncated frame)
	Expect      string   `json:"expect"`                // error kind, see errorKind
	PeerVersion *int     `json:"expect_peer_version,omitempty"`
	// Written is what the endpoint under test must have written by the time it
	// fails: any of "m1", "m2", "r_d", "r_c", "data", in order.
	Written []string `json:"expect_written"`
}

type vectorFile struct {
	Comment string `json:"comment"`
	Label   string `json:"label"`
	Seeds   struct {
		ClientEph       string `json:"client_eph"`
		DaemonStatic    string `json:"daemon_static"`
		DaemonEph       string `json:"daemon_eph"`
		DaemonEphReplay string `json:"daemon_eph_replay"`
	} `json:"seeds"`
	PublicKeys struct {
		ClientEph       string `json:"client_eph"`
		DaemonStatic    string `json:"daemon_static"`
		DaemonEph       string `json:"daemon_eph"`
		DaemonEphReplay string `json:"daemon_eph_replay"`
	} `json:"public_keys"`
	Es         string `json:"es"`
	Ee         string `json:"ee"`
	Th         string `json:"th"`
	KD2M       string `json:"k_d2m"`
	KM2D       string `json:"k_m2d"`
	ConfirmD   string `json:"confirm_d"`
	ConfirmM   string `json:"confirm_m"`
	M1FrameHex string `json:"m1_frame_hex"`
	M2FrameHex string `json:"m2_frame_hex"`
	RDFrameHex string `json:"r_d_frame_hex"`
	RCFrameHex string `json:"r_c_frame_hex"`
	Data       struct {
		D2M []vectorData `json:"d2m"`
		M2D []vectorData `json:"m2d"`
	} `json:"data"`
	Replay   vectorSession    `json:"replay"`
	Negative []vectorNegative `json:"negative"`
}

func hx(b []byte) string { return hex.EncodeToString(b) }

func frameHex(typ byte, payload []byte) string { return hx(rawFrame(typ, payload)) }

// errorKind maps an error to the kind string the vector file uses; the Rust
// and TypeScript tests map their own error types onto the same strings.
func errorKind(err error) string {
	switch {
	case errors.Is(err, ErrUnsupportedVersion):
		return "unsupported_version"
	case errors.Is(err, ErrPeerKeyMismatch):
		return "peer_key_mismatch"
	case errors.Is(err, ErrHandshakeConfirm):
		return "handshake_confirm"
	case errors.Is(err, ErrKeyAgreement):
		return "key_agreement"
	case errors.Is(err, ErrProtocol):
		return "protocol"
	case err == nil:
		return "none"
	default:
		return "other: " + err.Error()
	}
}

// dataFrame seals plaintext with s and returns its full wire frame.
func dataFrame(t testing.TB, s *Session, plaintext []byte) vectorData {
	t.Helper()
	counter, ct, err := s.Seal(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	payload := binary.BigEndian.AppendUint64(nil, counter)
	payload = append(payload, ct...)
	return vectorData{Counter: counter, PlaintextHex: hx(plaintext), FrameHex: frameHex(frameData, payload)}
}

func sessionVector(t testing.TB, ref *refSession) vectorSession {
	m2 := append([]byte{ProtocolVersion, byte(RoleDaemon)}, ref.daemonPub...)
	m2 = append(m2, ref.daemonEphPub...)
	return vectorSession{
		DaemonEphPub: hx(ref.daemonEphPub),
		Ee:           hx(ref.ee),
		Th:           hx(ref.sched.th),
		KD2M:         hx(ref.sched.d2m),
		KM2D:         hx(ref.sched.m2d),
		ConfirmD:     hx(ref.sched.confirmD),
		ConfirmM:     hx(ref.sched.confirmM),
		M2FrameHex:   frameHex(frameHello, m2),
		RDFrameHex:   frameHex(frameReady, ref.rd),
		RCFrameHex:   frameHex(frameReady, ref.rc),
	}
}

func buildVectors(t *testing.T) *vectorFile {
	a := runReference(t, seedFrom(0x40))
	b := runReference(t, seedFrom(0x60))
	sv := sessionVector(t, a)

	v := &vectorFile{
		Comment: "E2EE v2 vectors (ADR-0035 section 7), generated by internal/relay/e2ee TestWriteVectors -update; " +
			"hex strings are lowercase, *_frame_hex are full wire frames (u32BE(1+len) || type || payload). " +
			"Session A is the reference session; `replay` is the same client ephemeral against daemon_eph_replay. " +
			"`negative` entries drive the end under test (`endpoint`) with the client seeds/daemon seeds above: " +
			"`frames_hex` are what the peer sends as raw bytes (usually full frames; a few are bare headers or cut short), `close_after` means the peer's stream ends after them, `expect` the error kind, `expect_written` what the endpoint wrote first. " +
			"The device_hello negatives live in internal/relay/devices/testdata/device_hello_vector.json.",
		Label: hkdfLabel,
	}
	v.Seeds.ClientEph, v.Seeds.DaemonStatic = hx(seedFrom(0xa0)), hx(seedFrom(0x01))
	v.Seeds.DaemonEph, v.Seeds.DaemonEphReplay = hx(seedFrom(0x40)), hx(seedFrom(0x60))
	v.PublicKeys.ClientEph, v.PublicKeys.DaemonStatic = hx(a.clientEphPub), hx(a.daemonPub)
	v.PublicKeys.DaemonEph, v.PublicKeys.DaemonEphReplay = hx(a.daemonEphPub), hx(b.daemonEphPub)
	v.Es, v.Ee, v.Th = hx(a.es), hx(a.ee), hx(a.sched.th)
	v.KD2M, v.KM2D, v.ConfirmD, v.ConfirmM = hx(a.sched.d2m), hx(a.sched.m2d), hx(a.sched.confirmD), hx(a.sched.confirmM)
	v.M1FrameHex = frameHex(frameHello, payloadOfHello(a.clientEphPub))
	v.M2FrameHex, v.RDFrameHex, v.RCFrameHex = sv.M2FrameHex, sv.RDFrameHex, sv.RCFrameHex
	v.Replay = sessionVector(t, b)

	// Two data frames per direction, sealed by the real Session on fresh
	// copies of session A's keys.
	d, err := newSession(RoleDaemon, a.sched)
	if err != nil {
		t.Fatal(err)
	}
	m, err := newSession(RoleMobile, a.sched)
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range []string{"hello from the daemon", "second daemon frame"} {
		v.Data.D2M = append(v.Data.D2M, dataFrame(t, d, []byte(pt)))
	}
	for _, pt := range []string{"hello from the client", "second client frame"} {
		v.Data.M2D = append(v.Data.M2D, dataFrame(t, m, []byte(pt)))
	}
	v.Negative = buildNegatives(t, a, b)
	return v
}

func buildNegatives(t *testing.T, a, b *refSession) []vectorNegative {
	zero := make([]byte, PublicKeySize)
	m2 := func(dpub, deph []byte) []byte {
		return append(append([]byte{ProtocolVersion, byte(RoleDaemon)}, dpub...), deph...)
	}
	realM2 := m2(a.daemonPub, a.daemonEphPub)
	tampered := bytes.Clone(realM2)
	tampered[2+PublicKeySize] ^= 0x01
	otherStatic := privFromSeed(t, seedFrom(0x80)).PublicKey().Bytes()
	attacker := newScriptedDaemon(t, seedFrom(0x80), seedFrom(0x90))
	attM2, attRd, _, _ := attacker.reply(t, a.clientEphPub)
	forgedRd := confirmTag(make([]byte, 32), a.sched.th)
	pin := hx(a.daemonPub)
	clientCase := func(name, desc, pinHex, expect string, frames ...[]byte) vectorNegative {
		n := vectorNegative{Name: name, Description: desc, Endpoint: "client", PinHex: pinHex, Expect: expect, Written: []string{"m1"}}
		for _, f := range frames {
			n.FramesHex = append(n.FramesHex, hx(f))
		}
		return n
	}
	daemonCase := func(name, desc, expect string, written []string, frames ...[]byte) vectorNegative {
		n := vectorNegative{Name: name, Description: desc, Endpoint: "daemon", Expect: expect, Written: written}
		for _, f := range frames {
			n.FramesHex = append(n.FramesHex, hx(f))
		}
		return n
	}
	rd := rawFrame(frameReady, a.rd)
	one := 1
	m1Payload := payloadOfHello(a.clientEphPub)
	truncated := func(n vectorNegative) vectorNegative { n.CloseAfter = true; return n }
	withRole := func(p []byte, r byte) []byte { return append([]byte{p[0], r}, p[2:]...) }
	grow := func(p []byte) []byte { return append(bytes.Clone(p), 0) }
	shrink := func(p []byte) []byte { return bytes.Clone(p[:len(p)-1]) }
	zeroLen := []byte{0, 0, 0, 0}
	overLen := []byte{0x00, 0x10, 0x00, 0x01} // 1 MiB + 1
	v1Client := append([]byte{1, byte(RoleMobile)}, a.clientEphPub...)
	v1Daemon := append([]byte{1, byte(RoleDaemon)}, a.daemonPub...)

	out := []vectorNegative{
		// Wire-parse negatives: framing, length, role and version-first checks.
		daemonCase("m1_wrong_role", "M1 whose role byte is the daemon's (02 01 ...)", "protocol", []string{},
			rawFrame(frameHello, withRole(m1Payload, byte(RoleDaemon)))),
		daemonCase("m1_unknown_role", "M1 with an unknown role byte", "protocol", []string{},
			rawFrame(frameHello, withRole(m1Payload, 9))),
		daemonCase("m1_length_plus_one", "M1 payload one byte too long (35)", "protocol", []string{},
			rawFrame(frameHello, grow(m1Payload))),
		daemonCase("m1_length_minus_one", "M1 payload one byte too short (33)", "protocol", []string{},
			rawFrame(frameHello, shrink(m1Payload))),
		daemonCase("v1_hello_wrong_length_from_client", "version 1 with a wrong length: still unsupported_version (version byte first)", "unsupported_version", []string{},
			rawFrame(frameHello, append([]byte{1, byte(RoleMobile)}, a.clientEphPub[:16]...))),
		daemonCase("zero_length_frame_to_daemon", "a frame header with length 0", "protocol", []string{}, zeroLen),
		daemonCase("oversized_frame_to_daemon", "a frame header with length 1 MiB + 1", "protocol", []string{}, overLen),
		truncated(daemonCase("truncated_frame_to_daemon", "the stream ends in the middle of M1", "protocol", []string{},
			rawFrame(frameHello, m1Payload)[:10])),
		clientCase("m2_wrong_role", "M2 whose role byte is the client's (02 02 ...)", "", "protocol",
			rawFrame(frameHello, withRole(realM2, byte(RoleMobile)))),
		clientCase("m2_unknown_role", "M2 with an unknown role byte", "", "protocol",
			rawFrame(frameHello, withRole(realM2, 9))),
		clientCase("m2_length_plus_one", "M2 payload one byte too long (67)", "", "protocol",
			rawFrame(frameHello, grow(realM2))),
		clientCase("m2_length_minus_one", "M2 payload one byte too short (65)", "", "protocol",
			rawFrame(frameHello, shrink(realM2))),
		clientCase("reflected_m1_to_client", "the client's own 34-byte M1 reflected back to it", "", "protocol",
			rawFrame(frameHello, m1Payload)),
		clientCase("v1_hello_wrong_length_from_daemon", "version 1 with a wrong length: still unsupported_version (version byte first)", "", "unsupported_version",
			rawFrame(frameHello, append([]byte{1, byte(RoleDaemon)}, a.daemonPub[:16]...))),
		clientCase("zero_length_frame_to_client", "a frame header with length 0", "", "protocol", zeroLen),
		clientCase("oversized_frame_to_client", "a frame header with length 1 MiB + 1", "", "protocol", overLen),
		truncated(clientCase("truncated_frame_to_client", "the stream ends in the middle of M2", "", "protocol",
			rawFrame(frameHello, realM2)[:20])),
		clientCase("tampered_daemon_eph", "M2 with one bit flipped in daemon_eph_pub, original R_d", "", "handshake_confirm",
			rawFrame(frameHello, tampered), rd),
		clientCase("wrong_static_key", "M2 carrying a different daemon_pub, pin = the original: rejected before any DH", pin, "peer_key_mismatch",
			rawFrame(frameHello, m2(otherStatic, a.daemonEphPub)), rd),
		clientCase("forged_daemon_other_static", "an attacker's M2/R_d built with its own static key, pin = the real daemon key", pin, "peer_key_mismatch",
			rawFrame(frameHello, attM2), rawFrame(frameReady, attRd)),
		clientCase("forged_daemon_ready", "the real daemon_pub in M2 but R_d made without the static private key", pin, "handshake_confirm",
			rawFrame(frameHello, realM2), rawFrame(frameReady, forgedRd)),
		clientCase("v1_hello_from_daemon", "v1 daemon hello (version 1, role 1, 34 bytes)", "", "unsupported_version",
			rawFrame(frameHello, v1Daemon)),
		daemonCase("v1_hello_from_client", "v1 client hello (version 1, role 2, 34 bytes): closed, nothing written", "unsupported_version", []string{},
			rawFrame(frameHello, v1Client)),
		clientCase("empty_ready_from_daemon", "M2 then an R_d with an empty payload", "", "protocol",
			rawFrame(frameHello, realM2), rawFrame(frameReady, nil)),
		daemonCase("empty_ready_from_client", "M1 then an R_c with an empty payload", "protocol", []string{"m2", "r_d"},
			rawFrame(frameHello, payloadOfHello(a.clientEphPub)), rawFrame(frameReady, nil)),
		daemonCase("low_order_client_eph", "M1 with the all-zero point: key agreement error, no M2", "key_agreement", []string{},
			rawFrame(frameHello, payloadOfHello(zero))),
		clientCase("low_order_daemon_eph", "M2 whose daemon_eph_pub is the all-zero point (ee)", "", "key_agreement",
			rawFrame(frameHello, m2(a.daemonPub, zero))),
		clientCase("low_order_daemon_static", "M2 whose daemon_pub is the all-zero point (es), no pin", "", "key_agreement",
			rawFrame(frameHello, m2(zero, a.daemonEphPub))),
		daemonCase("wrong_r_c", "R_c computed from the replay session (same client ephemeral, other daemon ephemeral)", "handshake_confirm", []string{"m2", "r_d"},
			rawFrame(frameHello, payloadOfHello(a.clientEphPub)), rawFrame(frameReady, b.rc)),
		daemonCase("data_before_r_c", "a data frame where R_c is expected: protocol error, no data sent", "protocol", []string{"m2", "r_d"},
			rawFrame(frameHello, payloadOfHello(a.clientEphPub)), rawFrame(frameData, make([]byte, 40))),
	}
	for i := range out {
		switch out[i].Name {
		case "v1_hello_from_daemon", "v1_hello_from_client", "v1_hello_wrong_length_from_client", "v1_hello_wrong_length_from_daemon":
			out[i].PeerVersion = &one
		}
	}
	return out
}

func marshalVectors(t *testing.T, v *vectorFile) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// TestWriteVectors regenerates the shared vector file with -update, and
// otherwise fails if the committed file drifted from the implementation.
func TestWriteVectors(t *testing.T) {
	want := marshalVectors(t, buildVectors(t))
	if *update {
		if err := os.WriteFile(vectorsPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale: run `go test ./internal/relay/e2ee -run TestWriteVectors -update`", vectorsPath)
	}
}

// TestVectorFileDataFramesOpen: the committed data frames open under the
// committed keys, in both directions, and tampering is rejected.
func TestVectorFileDataFramesOpen(t *testing.T) {
	var v vectorFile
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	sched := &schedule{th: mustHex(t, v.Th), d2m: mustHex(t, v.KD2M), m2d: mustHex(t, v.KM2D)}
	for _, tc := range []struct {
		role   Role
		frames []vectorData
	}{{RoleMobile, v.Data.D2M}, {RoleDaemon, v.Data.M2D}} {
		s, err := newSession(tc.role, sched)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range tc.frames {
			frame := mustHex(t, f.FrameHex)
			payload := frame[5:]
			got, err := s.Open(binary.BigEndian.Uint64(payload[:8]), payload[8:])
			if err != nil || hx(got) != f.PlaintextHex {
				t.Fatalf("%v frame %d: %v / %x", tc.role, f.Counter, err, got)
			}
		}
	}
}

func frameKinds(frames [][]byte) []string {
	kinds := []string{}
	for _, f := range frames {
		switch f[4] {
		case frameHello:
			kinds = append(kinds, "hello")
		case frameReady:
			kinds = append(kinds, "ready")
		case frameData:
			kinds = append(kinds, "data")
		}
	}
	return kinds
}

// TestNegativeVectorsFromFile drives every `negative` entry of the committed
// file through a real Channel and checks the error kind and what was written.
func TestNegativeVectorsFromFile(t *testing.T) {
	var v vectorFile
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Negative) == 0 {
		t.Fatal("no negative vectors")
	}
	for _, n := range v.Negative {
		t.Run(n.Name, func(t *testing.T) {
			conn, peer := newMemPipe()
			var ch *Channel
			var opts []HandshakeOption
			switch n.Endpoint {
			case "client":
				ch = newClientChannel(t, conn)
				seedEphemeral(t, ch, mustHex(t, v.Seeds.ClientEph))
				if n.PinHex != "" {
					opts = append(opts, WithExpectedPeerKey(mustHex(t, n.PinHex)))
				}
			case "daemon":
				ch = newDaemonChannel(t, conn, keyPairFromSeed(t, mustHex(t, v.Seeds.DaemonStatic)))
				seedEphemeral(t, ch, mustHex(t, v.Seeds.DaemonEph))
			default:
				t.Fatalf("unknown endpoint %q", n.Endpoint)
			}
			for _, f := range n.FramesHex {
				if _, err := peer.Write(mustHex(t, f)); err != nil {
					t.Fatal(err)
				}
			}
			if n.CloseAfter {
				_ = peer.Close()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := ch.Handshake(ctx, opts...)
			if got := errorKind(err); got != n.Expect {
				t.Fatalf("error kind = %s (%v), want %s", got, err, n.Expect)
			}
			if n.PeerVersion != nil {
				var uv *UnsupportedVersionError
				if !errors.As(err, &uv) || int(uv.Peer) != *n.PeerVersion {
					t.Fatalf("peer version = %v, want %d", err, *n.PeerVersion)
				}
			}
			var want []string
			for _, w := range n.Written {
				switch w {
				case "m1", "m2":
					want = append(want, "hello")
				case "r_d", "r_c":
					want = append(want, "ready")
				default:
					want = append(want, w)
				}
			}
			if want == nil {
				want = []string{}
			}
			if got := frameKinds(conn.written()); !slices.Equal(got, want) {
				t.Fatalf("endpoint wrote %v, want %v", got, want)
			}
		})
	}
}
