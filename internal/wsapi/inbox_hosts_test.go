package wsapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/store"
)

func TestInboxHostSourceOffWhenFleetDisabled(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	reg := registry.New(db, 16)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AcceptPairRequest(pub, "mac", "Mac"); err != nil { // responder side: awaits our approval
		t.Fatal(err)
	}
	if _, err := reg.PairAsInitiator(pub2, "linux", "Linux", `{}`); err != nil { // initiator side: we act, not approve
		t.Fatal(err)
	}

	if pairs := fleetInboxPairs(db, func() bool { return false })(); len(pairs) != 0 {
		t.Fatalf("fleet off must derive no pairs without touching the store: %+v", pairs)
	}
	pairs := fleetInboxPairs(db, func() bool { return true })()
	if len(pairs) != 1 || pairs[0].Alias != "mac" {
		t.Fatalf("only the responder-side pending pairing counts: %+v", pairs)
	}
}
