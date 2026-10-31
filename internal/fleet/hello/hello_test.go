package hello

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
)

func mustKey(t *testing.T) *identity.Key {
	t.Helper()
	k, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustNonce(t *testing.T) []byte {
	t.Helper()
	n, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v (%T), want *hello.Error", err, err)
	}
	if e.RPCCode() != code {
		t.Fatalf("RPCCode() = %q, want %q (error %v)", e.RPCCode(), code, err)
	}
}

// rpcCoder is the subset of refusal errors the RPC layer matches on.
type rpcCoder interface {
	error
	RPCCode() string
}

// TestHelloMutualPinnedIdentity walks the full mutual handshake between two
// pinned hosts: the caller's Hello verifies against the callee's challenge,
// the callee's HelloOK verifies against the caller's pin, and both replay
// defences hold — a HelloOK signed by anyone but the pinned callee is a peer
// identity change, and a tampered state no longer matches the signature.
func TestHelloMutualPinnedIdentity(t *testing.T) {
	caller := mustKey(t)
	callee := mustKey(t)
	third := mustKey(t)
	now := time.Unix(1_750_000_000, 0)
	nonceC := mustNonce(t)
	nonceA := mustNonce(t)

	h := SignHello(caller, callee.HostID(), nonceC, nonceA, now.Unix(), "0.17.0", Caps())
	if err := VerifyHello(h, callee.HostID(), nonceC, now); err != nil {
		t.Fatalf("VerifyHello() = %v, want nil", err)
	}

	ok := SignOK(callee, caller.HostID(), nonceC, nonceA, now.Unix(), "pending", "0.17.0", Caps())
	if err := VerifyOK(ok, callee.Public(), caller.HostID(), nonceC, nonceA, now.Unix()); err != nil {
		t.Fatalf("VerifyOK() = %v, want nil", err)
	}

	// The same HelloOK under a different pin: the pin check fires before
	// the signature check and reports a peer identity change.
	if err := VerifyOK(ok, third.Public(), caller.HostID(), nonceC, nonceA, now.Unix()); err == nil {
		t.Fatal("VerifyOK() = nil under a third-party pin, want peer_identity_changed")
	} else {
		wantCode(t, err, CodePeerIdentityChanged)
	}

	// A state flip after signing must not verify.
	tampered := ok
	tampered.State = "approved"
	if err := VerifyOK(tampered, callee.Public(), caller.HostID(), nonceC, nonceA, now.Unix()); err == nil {
		t.Fatal("VerifyOK() = nil with tampered state, want bad_signature")
	} else {
		wantCode(t, err, CodeBadSignature)
	}
}

// TestHelloReplayOnNewConnectionRefused proves a Hello is bound to one
// connection's challenge: replaying it against a fresh nonceC (a new
// connection) or towards a different callee fails the signature check.
func TestHelloReplayOnNewConnectionRefused(t *testing.T) {
	caller := mustKey(t)
	callee := mustKey(t)
	other := mustKey(t)
	now := time.Unix(1_750_000_000, 0)
	nonceC1 := mustNonce(t)
	nonceC2 := mustNonce(t)
	nonceA := mustNonce(t)

	h := SignHello(caller, callee.HostID(), nonceC1, nonceA, now.Unix(), "0.17.0", Caps())
	if err := VerifyHello(h, callee.HostID(), nonceC1, now); err != nil {
		t.Fatalf("original connection: VerifyHello() = %v, want nil", err)
	}

	cases := []struct {
		name     string
		nonceC   []byte
		calleeID string
	}{
		{"new connection nonce", nonceC2, callee.HostID()},
		{"different callee id", nonceC1, other.HostID()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyHello(h, tc.calleeID, tc.nonceC, now)
			if err == nil {
				t.Fatal("VerifyHello() = nil for a replayed hello, want bad_signature")
			}
			wantCode(t, err, CodeBadSignature)
		})
	}
}

// TestHelloStaleRefused pins the clock-skew window: 121 seconds in either
// direction is stale, 119 seconds in the past still verifies.
func TestHelloStaleRefused(t *testing.T) {
	caller := mustKey(t)
	callee := mustKey(t)
	now := time.Unix(1_750_000_000, 0)
	nonceC := mustNonce(t)
	nonceA := mustNonce(t)

	cases := []struct {
		name string
		ts   time.Time
		ok   bool
	}{
		{"121s past", now.Add(-121 * time.Second), false},
		{"121s future", now.Add(121 * time.Second), false},
		{"119s past", now.Add(-119 * time.Second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := SignHello(caller, callee.HostID(), nonceC, nonceA, tc.ts.Unix(), "0.17.0", Caps())
			err := VerifyHello(h, callee.HostID(), nonceC, now)
			if tc.ok {
				if err != nil {
					t.Fatalf("VerifyHello() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("VerifyHello() = nil, want stale")
			}
			wantCode(t, err, CodeStale)
		})
	}
}

// TestHelloVersionSkewRefusedWithGuidance checks CheckVersion: a peer on a
// newer fleet version or without the required caps is refused with
// guidance naming the peer and its version; a matching peer passes; and the
// error carries the fleet_version_unsupported RPC code.
func TestHelloVersionSkewRefusedWithGuidance(t *testing.T) {
	cases := []struct {
		name        string
		peerFleetV  int
		peerCaps    []string
		peerVersion string
		wantErr     bool
	}{
		{"newer fleet version", FleetV + 1, Caps(), "0.16.0", true},
		{"missing caps", FleetV, nil, "0.16.0", true},
		{"matching peer", FleetV, Caps(), "0.17.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckVersion("win", tc.peerFleetV, tc.peerCaps, tc.peerVersion)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("CheckVersion() = %v, want nil", err)
				}
				return
			}
			var ue *UnsupportedError
			if !errors.As(err, &ue) {
				t.Fatalf("CheckVersion() error = %v (%T), want *UnsupportedError", err, err)
			}
			if !strings.Contains(ue.Guidance, "update smind on win") {
				t.Errorf("Guidance %q does not name the peer", ue.Guidance)
			}
			if !strings.Contains(ue.Guidance, tc.peerVersion) {
				t.Errorf("Guidance %q does not mention the peer version %q", ue.Guidance, tc.peerVersion)
			}
			var rc rpcCoder
			if !errors.As(err, &rc) {
				t.Fatalf("CheckVersion() error = %v, want an RPCCode() error", err)
			}
			if rc.RPCCode() != "fleet_version_unsupported" {
				t.Errorf("RPCCode() = %q, want fleet_version_unsupported", rc.RPCCode())
			}
		})
	}
}

// TestSignedBytesDomainSeparation proves the caller and callee signatures
// cover different bytes: the two canonical messages differ, and neither
// signature verifies over the other's message.
func TestSignedBytesDomainSeparation(t *testing.T) {
	caller := mustKey(t)
	callee := mustKey(t)
	ts := int64(1_750_000_000)
	nonceC := mustNonce(t)
	nonceA := mustNonce(t)

	helloMsg := signedBytes(Domain, callee.HostID(), caller.HostID(), nonceC, nonceA, ts, FleetV, "")
	okMsg := signedBytes(DomainOK, callee.HostID(), caller.HostID(), nonceC, nonceA, ts, FleetV, "pending")
	if bytes.Equal(helloMsg, okMsg) {
		t.Fatal("hello and helloOK signed bytes are identical; domains do not separate")
	}
	if bytes.Equal(signedBytes(Domain, callee.HostID(), caller.HostID(), nonceC, nonceA, ts, FleetV, ""), signedBytes(Domain, callee.HostID(), caller.HostID(), nonceC, nonceA, ts, FleetV, "approved")) {
		t.Fatal("empty and state extras produce identical hello bytes; state is not covered")
	}

	h := SignHello(caller, callee.HostID(), nonceC, nonceA, ts, "0.17.0", Caps())
	ok := SignOK(callee, caller.HostID(), nonceC, nonceA, ts, "pending", "0.17.0", Caps())
	if identity.Verify(caller.Public(), okMsg, h.Sig) {
		t.Error("a Hello signature verifies over the HelloOK message")
	}
	if identity.Verify(callee.Public(), helloMsg, ok.Sig) {
		t.Error("a HelloOK signature verifies over the Hello message")
	}
}
