package e2ee

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// testSessions derives both ends of a session directly, without a transport.
func testSessions(t *testing.T) (daemonSide, mobileSide *Session) {
	t.Helper()

	daemonKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("daemon GenerateKeyPair: %v", err)
	}
	mobileKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("mobile GenerateKeyPair: %v", err)
	}
	daemonPub, mobilePub := daemonKey.Public(), mobileKey.Public()

	mobileParsed, err := ParsePublicKey(mobilePub)
	if err != nil {
		t.Fatalf("ParsePublicKey(mobile): %v", err)
	}
	daemonParsed, err := ParsePublicKey(daemonPub)
	if err != nil {
		t.Fatalf("ParsePublicKey(daemon): %v", err)
	}

	daemonSide, err = newSession(daemonKey.private(), mobileParsed, RoleDaemon, daemonPub, mobilePub)
	if err != nil {
		t.Fatalf("newSession(daemon): %v", err)
	}
	mobileSide, err = newSession(mobileKey.private(), daemonParsed, RoleMobile, daemonPub, mobilePub)
	if err != nil {
		t.Fatalf("newSession(mobile): %v", err)
	}
	return daemonSide, mobileSide
}

func TestBothEndsDeriveTheSameKeys(t *testing.T) {
	daemonKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("daemon GenerateKeyPair: %v", err)
	}
	mobileKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("mobile GenerateKeyPair: %v", err)
	}
	daemonPub, mobilePub := daemonKey.Public(), mobileKey.Public()

	mobileParsed, err := ParsePublicKey(mobilePub)
	if err != nil {
		t.Fatalf("ParsePublicKey(mobile): %v", err)
	}
	daemonParsed, err := ParsePublicKey(daemonPub)
	if err != nil {
		t.Fatalf("ParsePublicKey(daemon): %v", err)
	}

	d2mFromDaemon, m2dFromDaemon, err := deriveKeys(daemonKey.private(), mobileParsed, daemonPub, mobilePub)
	if err != nil {
		t.Fatalf("deriveKeys(daemon side): %v", err)
	}
	d2mFromMobile, m2dFromMobile, err := deriveKeys(mobileKey.private(), daemonParsed, daemonPub, mobilePub)
	if err != nil {
		t.Fatalf("deriveKeys(mobile side): %v", err)
	}

	if !bytes.Equal(d2mFromDaemon, d2mFromMobile) {
		t.Error("the two ends derived different daemon->mobile keys")
	}
	if !bytes.Equal(m2dFromDaemon, m2dFromMobile) {
		t.Error("the two ends derived different mobile->daemon keys")
	}
	if bytes.Equal(d2mFromDaemon, m2dFromDaemon) {
		t.Error("both directions share one key; they must be independent")
	}
	if len(d2mFromDaemon) != chacha20poly1305.KeySize {
		t.Errorf("derived key is %d bytes, want %d", len(d2mFromDaemon), chacha20poly1305.KeySize)
	}
}

func TestSessionRoundTripBothDirections(t *testing.T) {
	daemonSide, mobileSide := testSessions(t)

	for i, msg := range [][]byte{[]byte("first"), []byte("second"), {}, bytes.Repeat([]byte("x"), 4096)} {
		counter, ciphertext, err := daemonSide.Seal(msg)
		if err != nil {
			t.Fatalf("daemon Seal %d: %v", i, err)
		}
		if counter != uint64(i) {
			t.Errorf("daemon frame %d sealed with counter %d", i, counter)
		}
		got, err := mobileSide.Open(counter, ciphertext)
		if err != nil {
			t.Fatalf("mobile Open %d: %v", i, err)
		}
		if !bytes.Equal(got, msg) {
			t.Errorf("mobile Open %d = %q, want %q", i, got, msg)
		}
	}

	counter, ciphertext, err := mobileSide.Seal([]byte("reply"))
	if err != nil {
		t.Fatalf("mobile Seal: %v", err)
	}
	got, err := daemonSide.Open(counter, ciphertext)
	if err != nil {
		t.Fatalf("daemon Open: %v", err)
	}
	if string(got) != "reply" {
		t.Errorf("daemon Open = %q, want %q", got, "reply")
	}
}

func TestSessionRejectsReplayedAndOutOfOrderFrames(t *testing.T) {
	daemonSide, mobileSide := testSessions(t)

	counter0, frame0, err := daemonSide.Seal([]byte("frame zero"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	counter1, frame1, err := daemonSide.Seal([]byte("frame one"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Skipping ahead is refused: counter 1 cannot be accepted before 0.
	if _, err := mobileSide.Open(counter1, frame1); !errors.Is(err, ErrReplay) {
		t.Errorf("out-of-order Open = %v, want ErrReplay", err)
	}
	if got := mobileSide.ExpectedRecvCounter(); got != 0 {
		t.Errorf("expected counter advanced to %d after a rejected frame", got)
	}

	if _, err := mobileSide.Open(counter0, frame0); err != nil {
		t.Fatalf("in-order Open: %v", err)
	}
	// The same captured frame, re-injected, is refused.
	if _, err := mobileSide.Open(counter0, frame0); !errors.Is(err, ErrReplay) {
		t.Errorf("replayed Open = %v, want ErrReplay", err)
	}
	// ...and the live next frame still works afterwards.
	if _, err := mobileSide.Open(counter1, frame1); err != nil {
		t.Fatalf("next in-order Open after a rejected replay: %v", err)
	}
	if got := mobileSide.ExpectedRecvCounter(); got != 2 {
		t.Errorf("ExpectedRecvCounter = %d, want 2", got)
	}
	if got := daemonSide.SendCounter(); got != 2 {
		t.Errorf("SendCounter = %d, want 2", got)
	}
}

func TestSessionRejectsTamperedCiphertext(t *testing.T) {
	daemonSide, mobileSide := testSessions(t)

	counter, ciphertext, err := daemonSide.Seal([]byte("authentic"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	tampered := bytes.Clone(ciphertext)
	tampered[0] ^= 0xff

	if _, err := mobileSide.Open(counter, tampered); !errors.Is(err, ErrDecrypt) {
		t.Errorf("Open(tampered) = %v, want ErrDecrypt", err)
	}
	// A rejected frame must not consume the counter.
	if _, err := mobileSide.Open(counter, ciphertext); err != nil {
		t.Fatalf("Open(authentic) after tampered frame: %v", err)
	}
}

func TestSessionKeysAreDirectional(t *testing.T) {
	daemonSide, _ := testSessions(t)

	counter, ciphertext, err := daemonSide.Seal([]byte("outbound"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// Reflecting a daemon-sent frame back at the daemon must not decrypt:
	// each direction has its own key.
	if _, err := daemonSide.Open(counter, ciphertext); !errors.Is(err, ErrDecrypt) {
		t.Errorf("Open(own frame) = %v, want ErrDecrypt", err)
	}
}

func TestSessionKeysAreUniquePerHandshake(t *testing.T) {
	daemonSide, _ := testSessions(t)
	_, otherMobile := testSessions(t)

	counter, ciphertext, err := daemonSide.Seal([]byte("session one"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := otherMobile.Open(counter, ciphertext); !errors.Is(err, ErrDecrypt) {
		t.Errorf("Open with another session's key = %v, want ErrDecrypt", err)
	}
}

func TestSessionCounterExhaustion(t *testing.T) {
	daemonSide, mobileSide := testSessions(t)

	daemonSide.sendCounter = math.MaxUint64
	if _, _, err := daemonSide.Seal([]byte("too late")); !errors.Is(err, ErrCounterExhausted) {
		t.Errorf("Seal at max counter = %v, want ErrCounterExhausted", err)
	}
	mobileSide.recvCounter = math.MaxUint64
	if _, err := mobileSide.Open(math.MaxUint64, []byte("anything")); !errors.Is(err, ErrCounterExhausted) {
		t.Errorf("Open at max counter = %v, want ErrCounterExhausted", err)
	}
}

func TestCounterNonce(t *testing.T) {
	seen := make(map[string]uint64)
	for _, counter := range []uint64{0, 1, 2, 255, 256, 1 << 40, math.MaxUint64 - 1} {
		nonce := counterNonce(counter)
		if len(nonce) != chacha20poly1305.NonceSize {
			t.Fatalf("nonce for %d is %d bytes, want %d", counter, len(nonce), chacha20poly1305.NonceSize)
		}
		if !bytes.Equal(nonce[:4], []byte{0, 0, 0, 0}) {
			t.Errorf("nonce for %d does not have a zero prefix: %x", counter, nonce)
		}
		if got := binary.BigEndian.Uint64(nonce[4:]); got != counter {
			t.Errorf("nonce for %d encodes %d", counter, got)
		}
		if prev, dup := seen[string(nonce)]; dup {
			t.Fatalf("counters %d and %d produced the same nonce", prev, counter)
		}
		seen[string(nonce)] = counter
	}
}

func TestChannelRejectsReplayedFrame(t *testing.T) {
	p := establishedPair(t)

	first := []byte("live frame")
	if err := p.daemon.Send(first); err != nil {
		t.Fatalf("daemon Send: %v", err)
	}
	captured := p.daemonConn.lastWritten()

	got, err := p.mobile.Receive()
	if err != nil {
		t.Fatalf("mobile Receive: %v", err)
	}
	if !bytes.Equal(got, first) {
		t.Fatalf("mobile received %q, want %q", got, first)
	}

	// The relay (or anyone who captured the ciphertext) re-delivers the exact
	// same frame: the counter has already been used, so it is refused.
	p.mobileConn.inject(captured)
	if _, err := p.mobile.Receive(); !errors.Is(err, ErrReplay) {
		t.Errorf("Receive(replayed frame) = %v, want ErrReplay", err)
	}

	// A live, in-order frame with the next expected counter still works.
	second := []byte("next live frame")
	if err := p.daemon.Send(second); err != nil {
		t.Fatalf("daemon Send: %v", err)
	}
	got, err = p.mobile.Receive()
	if err != nil {
		t.Fatalf("mobile Receive after replay: %v", err)
	}
	if !bytes.Equal(got, second) {
		t.Errorf("mobile received %q, want %q", got, second)
	}
}

func TestChannelRejectsShortDataFrame(t *testing.T) {
	p := establishedPair(t)

	p.mobileConn.inject(rawFrame(frameData, []byte{0, 0, 0}))
	if _, err := p.mobile.Receive(); !errors.Is(err, ErrProtocol) {
		t.Errorf("Receive(short data frame) = %v, want ErrProtocol", err)
	}
	if !p.mobile.Closed() {
		t.Error("channel should be closed after a malformed data frame")
	}
}
