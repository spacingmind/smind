package registry

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/store"
)

func openRegistry(t *testing.T, maxHosts int) *Registry {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, maxHosts)
}

// testKey derives a deterministic key from a seed byte, so each test's
// hosts have distinct, stable ids without touching the real home.
func testKey(seed byte) ed25519.PublicKey {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed
	}
	return ed25519.NewKeyFromSeed(s).Public().(ed25519.PublicKey)
}

func requireErrIs(t *testing.T, err error, target error, what string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: got %v, want %v", what, err, target)
	}
}

func grantsEqual(t *testing.T, got Grant, want Grant) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grant = %+v, want %+v", got, want)
	}
}

func TestRegistryPairingDirectedLinksReverseOff(t *testing.T) {
	r := openRegistry(t, 16)
	pub := testKey(1)
	hostID := identity.HostID(pub)

	h, err := r.PairAsInitiator(pub, "peer", "Lou's laptop", `{"relay":"dev"}`)
	if err != nil {
		t.Fatal(err)
	}
	if h.HostID != hostID || h.State != StatePending {
		t.Fatalf("host = %+v, want pending %s", h, hostID)
	}

	links, err := r.st.ListFleetLinks(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("a pairing writes exactly 2 links, got %d", len(links))
	}
	out, in, err := r.Links(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || out.TransportJSON != `{"relay":"dev"}` || out.GrantJSON != "" {
		t.Fatalf("outbound link = %+v, want enabled with transport and no grant", out)
	}
	if in.Enabled {
		t.Fatal("inbound link must start disabled")
	}
	stored, err := ParseGrant(in.GrantJSON)
	if err != nil {
		t.Fatalf("inbound grant does not parse: %v", err)
	}
	grantsEqual(t, stored, DefaultReverseGrant())

	// Until the responder confirms, the peer may neither be called nor read us.
	if _, err := r.Authorize(hostID, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize read on a pending host: got %v, want ErrNotGranted", err)
	}
	if r.CanCall(hostID) {
		t.Fatal("CanCall must be false while the host is pending")
	}
	if err := r.MarkApproved(hostID); err != nil {
		t.Fatal(err)
	}
	if !r.CanCall(hostID) {
		t.Fatal("CanCall must be true once approved with the outbound link enabled")
	}
}

func TestRegistryResponderAcceptApprove(t *testing.T) {
	r := openRegistry(t, 16)
	pub := testKey(2)
	hostID := identity.HostID(pub)

	if _, err := r.AcceptPairRequest(pub, "pal", "Pal's mini"); err != nil {
		t.Fatal(err)
	}
	_, in, err := r.Links(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if in.Enabled {
		t.Fatal("the inbound link must stay disabled until a human approves")
	}
	stored, err := ParseGrant(in.GrantJSON)
	if err != nil {
		t.Fatal(err)
	}
	grantsEqual(t, stored, DefaultForwardGrant())
	if _, err := r.Authorize(hostID, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize read before approval: got %v, want ErrNotGranted", err)
	}

	// The default forward grant carries no write workspace, so approval
	// must refuse it and change nothing.
	if err := r.Approve(hostID, DefaultForwardGrant()); err == nil || !strings.Contains(err.Error(), "grant.workspacesWrite") {
		t.Fatalf("approving the default forward grant must name grant.workspacesWrite, got %v", err)
	}
	h, err := r.Get(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != StatePending {
		t.Fatalf("state = %s, want still pending after a refused approval", h.State)
	}
	if _, err := r.Authorize(hostID, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize read after a refused approval: got %v, want ErrNotGranted", err)
	}

	g := DefaultForwardGrant()
	g.WorkspacesWrite = []int64{7}
	if err := r.Approve(hostID, g); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID, OpRead, 0); err != nil {
		t.Fatalf("authorize read after approval: %v", err)
	}
	if _, err := r.Authorize(hostID, OpCreate, 7); err != nil {
		t.Fatalf("authorize create on workspace 7: %v", err)
	}
	if _, err := r.Authorize(hostID, OpCreate, 8); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize create on workspace 8: got %v, want ErrNotGranted", err)
	}
	if _, err := r.Authorize(hostID, OpSend, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize send with workspace 0: got %v, want ErrNotGranted", err)
	}
	requireErrIs(t, r.Approve(hostID, g), ErrBadState, "approving twice")
}

func TestRegistryEnableDisableReverse(t *testing.T) {
	r := openRegistry(t, 16)
	pub := testKey(3)
	hostID := identity.HostID(pub)
	if _, err := r.PairAsInitiator(pub, "mac-1", "Lou's laptop", `{}`); err != nil {
		t.Fatal(err)
	}

	requireErrIs(t, r.EnableReverse(hostID), ErrNotApproved, "enabling reverse before approval")

	if err := r.MarkApproved(hostID); err != nil {
		t.Fatal(err)
	}
	if err := r.EnableReverse(hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID, OpRead, 0); err != nil {
		t.Fatalf("read after EnableReverse: %v", err)
	}
	// The reverse grant is read-only: create is refused even with an id.
	if _, err := r.Authorize(hostID, OpCreate, 7); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("create after EnableReverse: got %v, want ErrNotGranted", err)
	}
	// Idempotent.
	if err := r.EnableReverse(hostID); err != nil {
		t.Fatal(err)
	}

	if err := r.DisableReverse(hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("read after DisableReverse: got %v, want ErrNotGranted", err)
	}
	_, in, err := r.Links(hostID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ParseGrant(in.GrantJSON)
	if err != nil {
		t.Fatal(err)
	}
	grantsEqual(t, stored, DefaultReverseGrant())
}

func TestRegistryAuthorize(t *testing.T) {
	r := openRegistry(t, 16)

	// A revoked host is refused even though it was fully live before.
	pub := testKey(4)
	hostID := identity.HostID(pub)
	if _, err := r.PairAsInitiator(pub, "gone", "gone", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkApproved(hostID); err != nil {
		t.Fatal(err)
	}
	if err := r.EnableReverse(hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID, OpRead, 0); err != nil {
		t.Fatalf("read before revoke: %v", err)
	}
	if err := r.Revoke(hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("read after revoke: got %v, want ErrNotGranted", err)
	}
	if r.CanCall(hostID) {
		t.Fatal("a revoked host must not be callable")
	}
	out, in, err := r.Links(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Enabled || in.Enabled {
		t.Fatalf("revoke must disable both links, got outbound=%v inbound=%v", out.Enabled, in.Enabled)
	}
	h, err := r.Get(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != StateRevoked {
		t.Fatalf("state = %s, want revoked", h.State)
	}

	// An approved host whose inbound link is dark (outbound-only) is
	// refused on the callee side, and an unknown host is indistinguishable
	// from an unentitled one.
	pub2 := testKey(5)
	hostID2 := identity.HostID(pub2)
	if _, err := r.PairAsInitiator(pub2, "out-only", "out-only", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkApproved(hostID2); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID2, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("read with a disabled inbound link: got %v, want ErrNotGranted", err)
	}
	if _, err := r.Authorize("no-such-host", OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize an unknown host: got %v, want ErrNotGranted", err)
	}
}

// TestRegistryRevokeStateFirst pins the failure order of Revoke: the state
// flips before any link is touched, and a revoked host is refused even if
// a link write never happened and the inbound link is still enabled.
func TestRegistryRevokeStateFirst(t *testing.T) {
	r := openRegistry(t, 16)
	pub := testKey(6)
	hostID := identity.HostID(pub)
	if _, err := r.PairAsInitiator(pub, "victim", "victim", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkApproved(hostID); err != nil {
		t.Fatal(err)
	}
	if err := r.EnableReverse(hostID); err != nil {
		t.Fatal(err)
	}

	if err := r.Revoke(hostID); err != nil {
		t.Fatal(err)
	}
	h, err := r.Get(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != StateRevoked {
		t.Fatalf("state = %s, want revoked", h.State)
	}
	out, in, err := r.Links(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Enabled || in.Enabled {
		t.Fatalf("both links must be disabled, got outbound=%v inbound=%v", out.Enabled, in.Enabled)
	}

	// A host whose state was forced to revoked while the inbound link
	// stayed enabled (the midway state of a failed revoke) is still
	// refused: state, not link plumbing, carries the authority.
	pub2 := testKey(7)
	hostID2 := identity.HostID(pub2)
	if _, err := r.PairAsInitiator(pub2, "stuck", "stuck", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkApproved(hostID2); err != nil {
		t.Fatal(err)
	}
	if err := r.EnableReverse(hostID2); err != nil {
		t.Fatal(err)
	}
	if _, in2, err := r.Links(hostID2); err != nil || !in2.Enabled {
		t.Fatalf("inbound link must be enabled before forcing the state, link=%+v err=%v", in2, err)
	}
	if err := r.st.UpdateFleetHost(hostID2, "stuck", "stuck", StateRevoked); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authorize(hostID2, OpRead, 0); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("authorize read on a revoked host with a live inbound link: got %v, want ErrNotGranted", err)
	}
}

func TestRegistryMaxHosts(t *testing.T) {
	r := openRegistry(t, 2)
	if _, err := r.PairAsInitiator(testKey(10), "a", "a", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PairAsInitiator(testKey(11), "b", "b", `{}`); err != nil {
		t.Fatal(err)
	}
	_, err := r.PairAsInitiator(testKey(12), "c", "c", `{}`)
	requireErrIs(t, err, ErrTooManyHosts, "a third host past the cap")
	n, err := r.st.CountFleetHosts()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("a refused pairing must store nothing, got %d hosts", n)
	}
}

func TestRegistryAliasUniqueAndRename(t *testing.T) {
	r := openRegistry(t, 16)
	h1, err := r.PairAsInitiator(testKey(20), "mac-1", "one", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.PairAsInitiator(testKey(21), "mac-1", "two", `{}`)
	requireErrIs(t, err, ErrAliasTaken, "pairing a second host on a taken alias")

	h2, err := r.PairAsInitiator(testKey(21), "mac-2", "two", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	requireErrIs(t, r.Rename(h2.HostID, "mac-1"), ErrAliasTaken, "renaming onto a taken alias")
	if err := r.Rename(h2.HostID, "Bad"); err == nil || errors.Is(err, ErrAliasTaken) {
		t.Fatalf("renaming to an invalid alias must be refused as a validation error, got %v", err)
	}
	if err := r.Rename(h2.HostID, "mac-3"); err != nil {
		t.Fatal(err)
	}
	got, err := r.ByAlias("mac-3")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != h2.HostID || got.Alias != "mac-3" {
		t.Fatalf("renamed host = %+v", got)
	}
	if _, err := r.ByAlias("mac-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old alias must be gone, got %v", err)
	}
	if h1.Alias != "mac-1" {
		t.Fatalf("sibling host alias changed to %q", h1.Alias)
	}
}

func TestRegistryNoPeerPathWritesGrant(t *testing.T) {
	// A grant is the only thing a peer may influence here, so nothing it
	// carries may name secret material.
	banned := []string{"secret", "token", "password", "env", "key"}
	typ := reflect.TypeOf(Grant{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		for _, b := range banned {
			if strings.Contains(name, b) {
				t.Errorf("grant field %q names %q", name, b)
			}
		}
	}
}

func TestRegistrySetOutboundTransportKeepsEnabled(t *testing.T) {
	r := openRegistry(t, 4)
	pub := testKey(7)
	h, err := r.PairAsInitiator(pub, "peer", "", `{"kind":"direct","url":"ws://127.0.0.1:1"}`)
	if err != nil {
		t.Fatal(err)
	}
	const next = `{"kind":"direct","url":"ws://127.0.0.1:2"}`
	if err := r.SetOutboundTransport(h.HostID, next); err != nil {
		t.Fatal(err)
	}
	out, in, err := r.Links(h.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || out.TransportJSON != next {
		t.Fatalf("outbound = %+v, want enabled with the new transport", out)
	}
	if in.Enabled {
		t.Fatalf("inbound enabled = true, want untouched (false)")
	}
	requireErrIs(t, r.SetOutboundTransport("nope", next), ErrNotFound, "unknown host")
}
