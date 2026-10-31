// Package hello is the fleet's peer-to-peer handshake (ADR-0048 F1b).
// Two hosts that already trust each other's pinned public key prove liveness
// with a mutual challenge-response: the callee sends a fresh challenge
// nonceC, the caller answers with a signed Hello binding nonceC, its own
// fresh nonceA and the callee's host id, and the callee replies with a
// signed HelloOK binding both nonces plus its link state. Every signature
// covers the nonces and the peer ids, so a captured hello is useless on any
// other connection (a new connection has a new nonceC) and cannot be retold
// as the other side's message (the caller and callee signatures use
// different domains).
package hello

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
)

const (
	// FleetV is the fleet wire protocol version this daemon speaks.
	FleetV = 1
	// Domain is signed by the caller's Hello.
	Domain = "smind-fleet-hello-v1"
	// DomainOK is signed by the callee's HelloOK. It differs from Domain so
	// a callee signature can never be replayed as a caller signature.
	DomainOK = "smind-fleet-hello-ok-v1"
	// NonceLen is the size of every handshake nonce.
	NonceLen = 32
	// MaxClockSkew is how far a signed timestamp may drift from local time.
	MaxClockSkew = 120 * time.Second
)

// RPC methods of the fleet link protocol.
const (
	MethodChallenge   = "fleet.challenge"
	MethodHello       = "fleet.hello"
	MethodPairRequest = "fleet.pair.request"
	MethodLinkState   = "fleet.link.state"
)

// Capability names advertised in challenges, hellos and HelloOKs.
const (
	// CapLink is the base capability: speaking the fleet link protocol.
	CapLink = "fleet.link"
)

// RequiredCaps is what a peer must advertise for a link to be usable.
var RequiredCaps = []string{CapLink}

// Caps returns the caps this daemon advertises, as a fresh slice.
func Caps() []string { return []string{CapLink} }

// Refusal codes carried by Error.RPCCode so a caller can branch on the
// reason a link was refused.
const (
	CodeUnknownHost         = "unknown_host"
	CodeAwaitingApproval    = "awaiting_approval"
	CodeRevoked             = "revoked"
	CodeNotGranted          = "not_granted"
	CodeBadSignature        = "bad_signature"
	CodeStale               = "stale"
	CodeReplay              = "replay"
	CodeVersionUnsupported  = "fleet_version_unsupported"
	CodePeerIdentityChanged = "peer_identity_changed"
	CodeBadHello            = "bad_hello"
)

// Challenge is the message a callee sends first on every new connection:
// a fresh nonce the caller must sign, plus the callee's own protocol version
// so the caller can refuse early with actionable guidance (CheckVersion).
type Challenge struct {
	NonceC  []byte   `json:"nonceC"`
	HostID  string   `json:"hostId"`
	FleetV  int      `json:"fleetV"`
	Caps    []string `json:"caps"`
	Version string   `json:"version"`
}

// Hello is the caller's answer to a Challenge. Sig covers Domain, the
// callee's host id, the caller's host id, both nonces, the timestamp, the
// fleet version and the empty extra string.
type Hello struct {
	V       int      `json:"v"`
	HostID  string   `json:"hostId"`
	Pub     []byte   `json:"pub"`
	NonceA  []byte   `json:"nonceA"`
	TS      int64    `json:"ts"`
	Version string   `json:"version"`
	FleetV  int      `json:"fleetV"`
	Caps    []string `json:"caps"`
	Sig     []byte   `json:"sig"`
}

// HelloOK is the callee's reply. Sig covers DomainOK, the callee's host id,
// the caller's host id, both nonces, the timestamp, the fleet version and
// the state as the extra string — so a signed "pending" cannot be flipped
// into "approved".
type HelloOK struct {
	HostID  string   `json:"hostId"`
	Pub     []byte   `json:"pub"`
	State   string   `json:"state"`
	Version string   `json:"version"`
	FleetV  int      `json:"fleetV"`
	Caps    []string `json:"caps"`
	Sig     []byte   `json:"sig"`
}

// NewNonce returns fresh random bytes for a handshake nonce.
func NewNonce() ([]byte, error) {
	n := make([]byte, NonceLen)
	if _, err := rand.Read(n); err != nil {
		return nil, fmt.Errorf("hello: generate nonce: %w", err)
	}
	return n, nil
}

// appendField appends a uint16-big-endian length followed by the bytes, so
// concatenated variable-length fields cannot be re-cut into a different
// split.
func appendField(out, b []byte) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(len(b)))
	return append(out, b...)
}

// signedBytes is the canonical signed message for both directions:
//
//	domain
//	‖ u16(len(calleeID)) ‖ calleeID
//	‖ u16(len(callerID)) ‖ callerID
//	‖ u16(len(nonceC)) ‖ nonceC
//	‖ u16(len(nonceA)) ‖ nonceA
//	‖ u16(len(extra)) ‖ extra
//	‖ ts                                     8 bytes big-endian
//	‖ fleetV                                 2 bytes big-endian
//
// A Hello signs domain Domain with extra ""; a HelloOK signs DomainOK with
// extra state. The differing domains make the two signatures
// non-interchangeable.
func signedBytes(domain, calleeID, callerID string, nonceC, nonceA []byte, ts int64, fleetV int, extra string) []byte {
	out := make([]byte, 0, len(domain)+10+len(calleeID)+len(callerID)+len(nonceC)+len(nonceA)+len(extra))
	out = append(out, domain...)
	out = appendField(out, []byte(calleeID))
	out = appendField(out, []byte(callerID))
	out = appendField(out, nonceC)
	out = appendField(out, nonceA)
	out = appendField(out, []byte(extra))
	out = binary.BigEndian.AppendUint64(out, uint64(ts))
	out = binary.BigEndian.AppendUint16(out, uint16(fleetV))
	return out
}

// SignHello builds the caller's signed Hello for the callee identified by
// calleeID, answering the callee's challenge nonceC with a fresh nonceA.
func SignHello(k *identity.Key, calleeID string, nonceC, nonceA []byte, ts int64, version string, caps []string) Hello {
	h := Hello{
		V:       1,
		HostID:  k.HostID(),
		Pub:     k.Public(),
		NonceA:  append([]byte(nil), nonceA...),
		TS:      ts,
		Version: version,
		FleetV:  FleetV,
		Caps:    append([]string(nil), caps...),
	}
	h.Sig = k.Sign(signedBytes(Domain, calleeID, h.HostID, nonceC, h.NonceA, h.TS, h.FleetV, ""))
	return h
}

// VerifyHello checks the caller's Hello against the challenge this
// connection issued (nonceC) and the local host's id. The nonce binding is
// the replay defence: a hello captured on another connection was signed for
// that connection's nonceC, so it fails here with CodeBadSignature. The
// fleet version and caps are NOT checked here; see CheckVersion.
func VerifyHello(h Hello, calleeID string, nonceC []byte, now time.Time) error {
	if h.V != 1 {
		return codeErr(CodeBadHello, fmt.Sprintf("hello version %d", h.V))
	}
	if len(h.Pub) != ed25519.PublicKeySize {
		return codeErr(CodeBadHello, fmt.Sprintf("public key is %d bytes, want %d", len(h.Pub), ed25519.PublicKeySize))
	}
	if h.HostID != identity.HostID(ed25519.PublicKey(h.Pub)) {
		return codeErr(CodeBadHello, "host id does not match public key")
	}
	if len(nonceC) != NonceLen {
		return codeErr(CodeBadHello, fmt.Sprintf("challenge nonce is %d bytes, want %d", len(nonceC), NonceLen))
	}
	if len(h.NonceA) != NonceLen {
		return codeErr(CodeBadHello, fmt.Sprintf("hello nonce is %d bytes, want %d", len(h.NonceA), NonceLen))
	}
	if delta := now.Sub(time.Unix(h.TS, 0)); delta > MaxClockSkew || delta < -MaxClockSkew {
		return codeErr(CodeStale, fmt.Sprintf("timestamp %d is %s from now, max %s", h.TS, delta, MaxClockSkew))
	}
	msg := signedBytes(Domain, calleeID, h.HostID, nonceC, h.NonceA, h.TS, h.FleetV, "")
	if !identity.Verify(ed25519.PublicKey(h.Pub), msg, h.Sig) {
		return codeErr(CodeBadSignature, "hello signature does not verify for this connection")
	}
	return nil
}

// SignOK builds the callee's signed HelloOK answering the caller's Hello.
// ts is the timestamp to bind; the caller checks it against its own clock.
func SignOK(k *identity.Key, callerID string, nonceC, nonceA []byte, ts int64, state, version string, caps []string) HelloOK {
	ok := HelloOK{
		HostID:  k.HostID(),
		Pub:     k.Public(),
		State:   state,
		Version: version,
		FleetV:  FleetV,
		Caps:    append([]string(nil), caps...),
	}
	ok.Sig = k.Sign(signedBytes(DomainOK, ok.HostID, callerID, nonceC, nonceA, ts, ok.FleetV, ok.State))
	return ok
}

// VerifyOK checks the callee's HelloOK against the public key pinned when
// the link was stored. The pin check comes first and is fatal on its own:
// a different key is a peer identity change, which the human must see, not
// a mere signature failure.
func VerifyOK(ok HelloOK, pinned ed25519.PublicKey, callerID string, nonceC, nonceA []byte, ts int64) error {
	if len(pinned) != ed25519.PublicKeySize {
		return codeErr(CodePeerIdentityChanged, fmt.Sprintf("pinned key is %d bytes, want %d", len(pinned), ed25519.PublicKeySize))
	}
	if !bytes.Equal(ok.Pub, pinned) || ok.HostID != identity.HostID(pinned) {
		return codeErr(CodePeerIdentityChanged, "callee public key or id differs from the pinned identity")
	}
	msg := signedBytes(DomainOK, ok.HostID, callerID, nonceC, nonceA, ts, ok.FleetV, ok.State)
	if !identity.Verify(pinned, msg, ok.Sig) {
		return codeErr(CodeBadSignature, "helloOK signature does not verify for this connection")
	}
	return nil
}

// Error is a link refusal with a machine-readable code.
type Error struct {
	Code string
	Msg  string
}

// Error implements error.
func (e *Error) Error() string { return fmt.Sprintf("hello: %s: %s", e.Code, e.Msg) }

// RPCCode returns the refusal code for the RPC error envelope.
func (e *Error) RPCCode() string { return e.Code }

func codeErr(code, msg string) *Error { return &Error{Code: code, Msg: msg} }

// UnsupportedError reports that a peer speaks an older or otherwise
// incompatible fleet protocol, with everything the UI needs to tell the
// human what to update.
type UnsupportedError struct {
	Needs    string
	Has      string
	Host     string
	Guidance string
}

// Error implements error, returning the human-facing guidance.
func (e *UnsupportedError) Error() string { return e.Guidance }

// RPCCode returns the refusal code for the RPC error envelope.
func (e *UnsupportedError) RPCCode() string { return CodeVersionUnsupported }

// RPCData returns the structured detail for the RPC error envelope.
func (e *UnsupportedError) RPCData() any {
	return map[string]string{
		"needs":    e.Needs,
		"has":      e.Has,
		"host":     e.Host,
		"guidance": e.Guidance,
	}
}

// CheckVersion reports whether a peer's advertised fleet version and caps
// are usable by this daemon: nil iff peerFleetV equals FleetV and peerCaps
// contains every cap in RequiredCaps. Otherwise it returns an
// *UnsupportedError whose Guidance names the peer and what to do.
func CheckVersion(peerAlias string, peerFleetV int, peerCaps []string, peerVersion string) error {
	if peerFleetV == FleetV && hasCaps(peerCaps, RequiredCaps) {
		return nil
	}
	return &UnsupportedError{
		Needs:    "fleet v1 fleet.link",
		Has:      fmt.Sprintf("fleet v%d %s", peerFleetV, strings.Join(peerCaps, " ")),
		Host:     peerAlias,
		Guidance: fmt.Sprintf("update smind on %s (has %s, needs fleet protocol v1 with fleet.link)", peerAlias, peerVersion),
	}
}

func hasCaps(peerCaps, required []string) bool {
	for _, want := range required {
		found := false
		for _, got := range peerCaps {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
