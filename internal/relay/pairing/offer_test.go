package pairing

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/relay/e2ee"
)

func testOffer(t *testing.T) Offer {
	t.Helper()
	kp, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return Offer{
		DaemonID:         "e6f0c2a4-6e1f-4f0e-9f4c-8a1b2c3d4e5f",
		PublicKey:        kp.Public(),
		Relay:            "wss://relay.spacingmind.sh",
		RelayFingerprint: "sha256:3b7a1f9c",
	}
}

func TestOfferPayloadRoundTrip(t *testing.T) {
	offer := testOffer(t)

	payload, err := offer.EncodePayload()
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}
	got, err := DecodePayload(payload)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	if got.DaemonID != offer.DaemonID {
		t.Errorf("DaemonID = %q, want %q", got.DaemonID, offer.DaemonID)
	}
	if !bytes.Equal(got.PublicKey, offer.PublicKey) {
		t.Errorf("PublicKey = %x, want %x", got.PublicKey, offer.PublicKey)
	}
	if got.Relay != offer.Relay {
		t.Errorf("Relay = %q, want %q", got.Relay, offer.Relay)
	}
	if got.RelayFingerprint != offer.RelayFingerprint {
		t.Errorf("RelayFingerprint = %q, want %q", got.RelayFingerprint, offer.RelayFingerprint)
	}
}

func TestOfferURLPutsPayloadInFragmentOnly(t *testing.T) {
	offer := testOffer(t)

	raw, err := offer.URL("")
	if err != nil {
		t.Fatalf("URL: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if u.RawQuery != "" {
		t.Errorf("pairing URL has a query string %q; the offer must never be sent to a server", u.RawQuery)
	}
	if len(u.Query()) != 0 {
		t.Errorf("pairing URL query = %v, want empty", u.Query())
	}
	if u.Fragment == "" {
		t.Fatal("pairing URL has no fragment")
	}
	if !strings.HasPrefix(u.Fragment, FragmentKey+"=") {
		t.Errorf("fragment = %q, want %s=... prefix", u.Fragment, FragmentKey)
	}

	payload, err := offer.EncodePayload()
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}
	if !strings.Contains(u.Fragment, payload) {
		t.Error("fragment does not carry the offer payload")
	}
	// Everything before the "#" — the part a server, CDN, or access log would
	// actually see — must not contain any of the offer.
	sent, _, _ := strings.Cut(raw, "#")
	for _, secret := range []string{payload, offer.DaemonID, offer.Relay, offer.RelayFingerprint} {
		if strings.Contains(sent, secret) {
			t.Errorf("server-visible part of URL %q leaks %q", sent, secret)
		}
	}
}

func TestParseURLRoundTrip(t *testing.T) {
	offer := testOffer(t)

	for _, base := range []string{"", DefaultPairURL, "smind://pair", "https://example.test/x/pair"} {
		raw, err := offer.URL(base)
		if err != nil {
			t.Fatalf("URL(%q): %v", base, err)
		}
		got, err := ParseURL(raw)
		if err != nil {
			t.Fatalf("ParseURL(%q): %v", raw, err)
		}
		if got.DaemonID != offer.DaemonID || !bytes.Equal(got.PublicKey, offer.PublicKey) || got.Relay != offer.Relay {
			t.Errorf("ParseURL(base %q) = %+v, want %+v", base, got, offer)
		}
	}
}

func TestParseURLRejectsQueryStringOffer(t *testing.T) {
	offer := testOffer(t)
	payload, err := offer.EncodePayload()
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}

	raw := DefaultPairURL + "?" + FragmentKey + "=" + payload
	if _, err := ParseURL(raw); !errors.Is(err, ErrInvalidOffer) {
		t.Errorf("ParseURL(query-string offer) error = %v, want ErrInvalidOffer", err)
	}
}

func TestURLRejectsBaseWithFragment(t *testing.T) {
	offer := testOffer(t)

	if _, err := offer.URL(DefaultPairURL + "#already"); !errors.Is(err, ErrInvalidOffer) {
		t.Errorf("URL(base with fragment) error = %v, want ErrInvalidOffer", err)
	}
}

func TestOfferValidation(t *testing.T) {
	valid := testOffer(t)

	tests := []struct {
		name  string
		offer Offer
	}{
		{name: "missing daemon ID", offer: Offer{PublicKey: valid.PublicKey, Relay: valid.Relay}},
		{name: "blank daemon ID", offer: Offer{DaemonID: "  ", PublicKey: valid.PublicKey, Relay: valid.Relay}},
		{name: "missing relay", offer: Offer{DaemonID: valid.DaemonID, PublicKey: valid.PublicKey}},
		{name: "no public key", offer: Offer{DaemonID: valid.DaemonID, Relay: valid.Relay}},
		{name: "short public key", offer: Offer{DaemonID: valid.DaemonID, PublicKey: make([]byte, 16), Relay: valid.Relay}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.offer.Validate(); !errors.Is(err, ErrInvalidOffer) {
				t.Errorf("Validate() error = %v, want ErrInvalidOffer", err)
			}
			if _, err := tc.offer.EncodePayload(); !errors.Is(err, ErrInvalidOffer) {
				t.Errorf("EncodePayload() error = %v, want ErrInvalidOffer", err)
			}
		})
	}

	if err := valid.Validate(); err != nil {
		t.Errorf("Validate(valid offer): %v", err)
	}
}

func TestDecodePayloadRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "not base64", payload: "!!!not base64!!!"},
		{name: "not json", payload: encodeRaw(t, "nope")},
		{name: "wrong version", payload: encodeRaw(t, `{"v":99,"id":"a","pk":"","relay":"wss://r"}`)},
		{name: "bad public key encoding", payload: encodeRaw(t, `{"v":1,"id":"a","pk":"!!","relay":"wss://r"}`)},
		{name: "short public key", payload: encodeRaw(t, `{"v":1,"id":"a","pk":"AAAA","relay":"wss://r"}`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodePayload(tc.payload); !errors.Is(err, ErrInvalidOffer) {
				t.Errorf("DecodePayload error = %v, want ErrInvalidOffer", err)
			}
		})
	}
}

func TestParseURLWithoutOfferFragment(t *testing.T) {
	for _, raw := range []string{DefaultPairURL, DefaultPairURL + "#", DefaultPairURL + "#other=1", "://bad url"} {
		if _, err := ParseURL(raw); !errors.Is(err, ErrInvalidOffer) {
			t.Errorf("ParseURL(%q) error = %v, want ErrInvalidOffer", raw, err)
		}
	}
}

func encodeRaw(t *testing.T, s string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
