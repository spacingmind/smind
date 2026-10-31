package link

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/wsclient"
)

// Peer is the identity the caller pinned when it paired with a host.
type Peer struct {
	HostID string
	Pub    ed25519.PublicKey
	Alias  string
}

// HelloResult is what a successful handshake learned about the callee.
type HelloResult struct {
	// State is the callee's signed link state ("active", "pending", ...).
	State   string
	Version string
	Caps    []string
	// NonceC and NonceA are the challenge nonce and the caller's nonce this
	// handshake used, so the caller can derive the pairing SAS itself.
	NonceC, NonceA []byte
}

// RefusedError is a callee's refusal of the link, carrying its code
// (awaiting_approval, revoked, not_granted, ...).
type RefusedError struct {
	Code string
	Msg  string
}

// Error implements error.
func (e *RefusedError) Error() string {
	return fmt.Sprintf("link: peer refused: %s: %s", e.Code, e.Msg)
}

// Handshake runs the caller side of the mutual fleet handshake over c
// against the pinned peer: challenge, signed hello, and verification of the
// callee's signed answer against the pinned key. A peer whose id or key
// differs from the pin, or whose fleet version is unusable, is refused
// before or right after any state is trusted.
func Handshake(ctx context.Context, c Conn, self *identity.Key, peer Peer, version string, now func() time.Time) (HelloResult, error) {
	var ch hello.Challenge
	if err := c.Call(ctx, hello.MethodChallenge, nil, &ch); err != nil {
		if isMethodNotFound(err) {
			return HelloResult{}, &hello.UnsupportedError{
				Needs:    "fleet v1 fleet.link",
				Has:      "no fleet support",
				Host:     peer.Alias,
				Guidance: fmt.Sprintf("update smind on %s (it does not speak the fleet protocol)", peer.Alias),
			}
		}
		return HelloResult{}, fmt.Errorf("link: challenge: %w", err)
	}
	if ch.HostID != peer.HostID {
		return HelloResult{}, &hello.Error{Code: hello.CodePeerIdentityChanged, Msg: "the host answering is not the one paired under this alias"}
	}
	if err := hello.CheckVersion(peer.Alias, ch.FleetV, ch.Caps, ch.Version); err != nil {
		return HelloResult{}, err
	}
	nonceA, err := hello.NewNonce()
	if err != nil {
		return HelloResult{}, err
	}
	ts := now().Unix()
	h := hello.SignHello(self, peer.HostID, ch.NonceC, nonceA, ts, version, hello.Caps())
	var ok hello.HelloOK
	if err := c.Call(ctx, hello.MethodHello, h, &ok); err != nil {
		var rpc *wsclient.RPCError
		if errors.As(err, &rpc) && rpc.Code != "" {
			if rpc.Code == hello.CodeVersionUnsupported {
				return HelloResult{}, unsupportedFromRPC(rpc, peer.Alias)
			}
			return HelloResult{}, &RefusedError{Code: rpc.Code, Msg: rpc.Message}
		}
		return HelloResult{}, fmt.Errorf("link: hello: %w", err)
	}
	if err := hello.VerifyOK(ok, peer.Pub, self.HostID(), ch.NonceC, nonceA, ts); err != nil {
		return HelloResult{}, err
	}
	if err := hello.CheckVersion(peer.Alias, ok.FleetV, ok.Caps, ok.Version); err != nil {
		return HelloResult{}, err
	}
	return HelloResult{State: ok.State, Version: ok.Version, Caps: ok.Caps, NonceC: ch.NonceC, NonceA: nonceA}, nil
}

func isMethodNotFound(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "method not found")
}

func unsupportedFromRPC(rpc *wsclient.RPCError, alias string) *hello.UnsupportedError {
	var d struct {
		Needs    string `json:"needs"`
		Has      string `json:"has"`
		Host     string `json:"host"`
		Guidance string `json:"guidance"`
	}
	_ = json.Unmarshal(rpc.Data, &d)
	if d.Host == "" {
		d.Host = alias
	}
	if d.Guidance == "" {
		d.Guidance = fmt.Sprintf("update smind on %s (it refused the fleet protocol version)", alias)
	}
	return &hello.UnsupportedError{Needs: d.Needs, Has: d.Has, Host: d.Host, Guidance: d.Guidance}
}
