package link

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/wsclient"
)

func newKey(t *testing.T) *identity.Key {
	t.Helper()
	k, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fakeCallee is a scripted callee speaking the real hello protocol over an
// in-memory Conn.
type fakeCallee struct {
	key      *identity.Key // signs the answers
	hostID   string        // host id put in the challenge
	fleetV   int
	caps     []string
	state    string
	helloErr error
	noMethod bool

	nonceC    []byte
	verified  error
	gotHello  bool
	calls     []string
	done      chan struct{}
	closeOnce bool
}

func newCallee(k *identity.Key) *fakeCallee {
	return &fakeCallee{key: k, hostID: k.HostID(), fleetV: hello.FleetV, caps: hello.Caps(), state: "active", done: make(chan struct{})}
}

func (f *fakeCallee) Call(_ context.Context, method string, params, out any) error {
	f.calls = append(f.calls, method)
	var res any
	switch method {
	case hello.MethodChallenge:
		if f.noMethod {
			return &wsclient.RPCError{Message: "method not found: " + method}
		}
		f.nonceC, _ = hello.NewNonce()
		res = hello.Challenge{NonceC: f.nonceC, HostID: f.hostID, FleetV: f.fleetV, Caps: f.caps, Version: "test"}
	case hello.MethodHello:
		if f.helloErr != nil {
			return f.helloErr
		}
		h := params.(hello.Hello)
		f.gotHello = true
		f.verified = hello.VerifyHello(h, f.hostID, f.nonceC, time.Now())
		ok := hello.SignOK(f.key, h.HostID, f.nonceC, h.NonceA, h.TS, f.state, "test", f.caps)
		ok.FleetV = f.fleetV
		res = ok
	default:
		return errors.New("unexpected method " + method)
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

func (f *fakeCallee) Close() error {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}

func (f *fakeCallee) Done() <-chan struct{} { return f.done }

func peerOf(k *identity.Key) Peer {
	return Peer{HostID: k.HostID(), Pub: k.Public(), Alias: "box"}
}

func TestHandshakeMutualOK(t *testing.T) {
	self, other := newKey(t), newKey(t)
	f := newCallee(other)
	res, err := Handshake(context.Background(), f, self, peerOf(other), "v-self", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "active" || !f.gotHello || f.verified != nil {
		t.Fatalf("res=%+v gotHello=%v verified=%v", res, f.gotHello, f.verified)
	}
}

func TestHandshakeRefusesChangedIdentity(t *testing.T) {
	self, orig, impostor := newKey(t), newKey(t), newKey(t)
	// Same challenge host id, different signing key.
	f := newCallee(impostor)
	f.hostID = orig.HostID()
	_, err := Handshake(context.Background(), f, self, peerOf(orig), "v", time.Now)
	var he *hello.Error
	if !errors.As(err, &he) || he.Code != hello.CodePeerIdentityChanged {
		t.Fatalf("key swap: %v", err)
	}
	// Challenge host id mismatch.
	g := newCallee(impostor)
	_, err = Handshake(context.Background(), g, self, peerOf(orig), "v", time.Now)
	if !errors.As(err, &he) || he.Code != hello.CodePeerIdentityChanged {
		t.Fatalf("id mismatch: %v", err)
	}
	if g.gotHello {
		t.Fatal("hello sent to a mismatched host")
	}
}

func TestHandshakeMethodNotFoundMapsToUpdateGuidance(t *testing.T) {
	self, other := newKey(t), newKey(t)
	f := newCallee(other)
	f.noMethod = true
	_, err := Handshake(context.Background(), f, self, peerOf(other), "v", time.Now)
	var ue *hello.UnsupportedError
	if !errors.As(err, &ue) || !strings.Contains(ue.Guidance, "box") {
		t.Fatalf("got %v", err)
	}
}

func TestHandshakeVersionSkewOnReconnect(t *testing.T) {
	self, other := newKey(t), newKey(t)
	if _, err := Handshake(context.Background(), newCallee(other), self, peerOf(other), "v", time.Now); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*fakeCallee){
		"old fleetV": func(f *fakeCallee) { f.fleetV = 2 },
		"no cap":     func(f *fakeCallee) { f.caps = nil },
	} {
		f := newCallee(other)
		mod(f)
		_, err := Handshake(context.Background(), f, self, peerOf(other), "v", time.Now)
		var ue *hello.UnsupportedError
		if !errors.As(err, &ue) || !strings.Contains(ue.Guidance, "box") {
			t.Fatalf("%s: got %v", name, err)
		}
		if f.gotHello {
			t.Fatalf("%s: hello sent despite version skew", name)
		}
	}
}

func TestHandshakeRefusalCodesSurface(t *testing.T) {
	self, other := newKey(t), newKey(t)
	for _, code := range []string{hello.CodeAwaitingApproval, hello.CodeRevoked, hello.CodeNotGranted} {
		f := newCallee(other)
		f.helloErr = &wsclient.RPCError{Message: "nope", Code: code}
		_, err := Handshake(context.Background(), f, self, peerOf(other), "v", time.Now)
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != code {
			t.Fatalf("%s: got %v", code, err)
		}
	}
}
