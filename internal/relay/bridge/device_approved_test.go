package bridge

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/relay/devices"
)

func TestHubDeviceApproved(t *testing.T) {
	t.Parallel()
	reg, err := devices.Open(filepath.Join(t.TempDir(), "d.json"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() string {
		pub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		id := devices.DeviceID(pub)
		if _, err := reg.Admit(id, pub, "x"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	approved, pending, revoked := admit(), admit(), admit()
	if _, err := reg.Approve(approved); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Revoke(revoked); err != nil {
		t.Fatal(err)
	}

	h := NewHub(Options{Registry: reg})
	for _, tc := range []struct {
		name string
		id   string
		want bool
	}{{"approved", approved, true}, {"pending", pending, false}, {"revoked", revoked, false}, {"unknown", "nope", false}} {
		if got := h.DeviceApproved(tc.id); got != tc.want {
			t.Errorf("%s: DeviceApproved = %v, want %v", tc.name, got, tc.want)
		}
	}
	if NewHub(Options{}).DeviceApproved(approved) {
		t.Error("nil registry: DeviceApproved = true")
	}
}
