package pairing

// fixture_test.go pins one exact pairing URL as a cross-language fixture:
// mobile/src/relay/__tests__/pairing.test.ts parses this exact literal and
// asserts the decoded fields, proving the TypeScript parser is wire-
// compatible with this package's own encoder, not just internally
// consistent with itself.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const fixtureOfferURL = "https://spacingmind.sh/pair#offer=eyJ2IjoxLCJpZCI6ImRhZW1vbi1maXh0dXJlLTEiLCJwayI6IkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkUiLCJyZWxheSI6Imh0dHBzOi8vcmVsYXkuZXhhbXBsZS50ZXN0Ojc0MDEiLCJmcCI6ImRlYWRiZWVmIiwic2VjIjoicTZ1cnE2dXJxNnVycTZ1cnE2dXJxNnVycTZ1cnE2dXJxNnVycTZ1cnE2cyIsIndzIjoid3MtZml4dHVyZS0xIn0"

func TestFixtureOfferURLForTypeScriptPort(t *testing.T) {
	pub := bytes.Repeat([]byte{0x11}, 32)
	secret := bytes.Repeat([]byte{0xab}, 32)
	offer := Offer{
		DaemonID:         "daemon-fixture-1",
		PublicKey:        pub,
		Relay:            "https://relay.example.test:7401",
		RelayFingerprint: "deadbeef",
		Secret:           secret,
		WorkspaceID:      "ws-fixture-1",
	}
	url, err := offer.URL("")
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if url != fixtureOfferURL {
		t.Fatalf("URL = %s, want %s", url, fixtureOfferURL)
	}

	got, err := ParseURL(url)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	if got.DaemonID != offer.DaemonID || !bytes.Equal(got.PublicKey, offer.PublicKey) ||
		got.Relay != offer.Relay || got.RelayFingerprint != offer.RelayFingerprint ||
		!bytes.Equal(got.Secret, offer.Secret) || got.WorkspaceID != offer.WorkspaceID {
		t.Fatalf("ParseURL round trip = %+v, want %+v", got, offer)
	}
}

// TestOfferWireFormatUnchanged pins that the relay-host rename (ADR-0031 P0)
// left the offer's JSON key names alone: the host id still travels as "ws".
func TestOfferWireFormatUnchanged(t *testing.T) {
	payload := strings.TrimPrefix(fixtureOfferURL, "https://spacingmind.sh/pair#offer=")
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode fixture payload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse fixture payload: %v", err)
	}
	if m["ws"] != "ws-fixture-1" {
		t.Fatalf(`fixture "ws" key = %v, want "ws-fixture-1" (keys: %v)`, m["ws"], m)
	}
	if _, renamed := m["host"]; renamed {
		t.Fatal(`offer payload must not gain a "host" key`)
	}
}
