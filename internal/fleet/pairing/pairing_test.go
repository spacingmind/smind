package pairing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
	relaypairing "github.com/spacingmind/smind/internal/relay/pairing"
)

func validInvite(t *testing.T, relay string) Invite {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	inv := Invite{
		V:       InviteVersion,
		HostID:  identity.HostID(pub),
		Pub:     pub,
		Expires: time.Unix(1_800_000_000, 0),
		Nonce:   nonce,
	}
	if relay != "" {
		inv.Relay = relay
	} else {
		inv.Addresses = []string{"ws://192.168.1.10:7700/fleet", "wss://fleet.example.com/fleet"}
	}
	return inv
}

func relayPayload(t *testing.T) string {
	t.Helper()
	x25519 := make([]byte, 32)
	if _, err := rand.Read(x25519); err != nil {
		t.Fatal(err)
	}
	offer := relaypairing.Offer{
		DaemonID:  "daemon-1",
		PublicKey: x25519,
		Relay:     "wss://relay.example.com",
	}
	payload, err := offer.EncodePayload()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// TestInvitePayloadFragmentOnlyNoSecrets pins the invite link's shape and
// refusal surface: the payload lives in the fragment and never in a query
// string, no field invites a secret by name, unknown JSON fields (like a
// smuggled password) are refused, and a mismatched host id or an address
// with userinfo is refused.
func TestInvitePayloadFragmentOnlyNoSecrets(t *testing.T) {
	relay := relayPayload(t)
	cases := []struct {
		name  string
		relay string
	}{
		{"direct addresses only", ""},
		{"with relay offer", relay},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := validInvite(t, tc.relay)
			link, err := inv.Encode()
			if err != nil {
				t.Fatalf("Encode() = %v, want nil", err)
			}
			if !strings.Contains(link, "#") || strings.Contains(link, "?") {
				t.Errorf("link %q must carry its payload after '#' and contain no '?'", link)
			}
			if got, want := link[:strings.IndexByte(link, '#')], Scheme+"://v1"; got != want {
				t.Errorf("link prefix = %q, want %q", got, want)
			}
			back, err := Decode(link)
			if err != nil {
				t.Fatalf("Decode() = %v, want nil", err)
			}
			if back.HostID != inv.HostID || !reflect.DeepEqual(back.Pub, inv.Pub) {
				t.Errorf("round trip changed the identity: got host %q, want %q", back.HostID, inv.HostID)
			}
			if back.Relay != inv.Relay {
				t.Errorf("round trip relay = %q, want %q", back.Relay, inv.Relay)
			}
		})
	}

	t.Run("no_secret_field_names", func(t *testing.T) {
		typ := reflect.TypeOf(Invite{})
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			for _, forbidden := range []string{"password", "secret", "token"} {
				if strings.Contains(name, forbidden) {
					t.Errorf("Invite field %s contains %q", typ.Field(i).Name, forbidden)
				}
			}
		}
	})

	payload := func(t *testing.T, extra map[string]any) string {
		t.Helper()
		inv := validInvite(t, "")
		raw, err := json.Marshal(inv)
		if err != nil {
			t.Fatal(err)
		}
		if extra != nil {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			for k, v := range extra {
				m[k] = v
			}
			if raw, err = json.Marshal(m); err != nil {
				t.Fatal(err)
			}
		}
		return Scheme + "://v1#" + base64.RawURLEncoding.EncodeToString(raw)
	}

	t.Run("unknown_password_field_refused", func(t *testing.T) {
		link := payload(t, map[string]any{"password": "hunter2"})
		if _, err := Decode(link); !errors.Is(err, ErrInvalidInvite) {
			t.Errorf("Decode() error = %v, want ErrInvalidInvite", err)
		}
	})

	t.Run("query_payload_refused", func(t *testing.T) {
		link := payload(t, nil)
		moved := Scheme + "://v1?" + link[strings.IndexByte(link, '#')+1:]
		if _, err := Decode(moved); !errors.Is(err, ErrInvalidInvite) {
			t.Errorf("Decode() error = %v, want ErrInvalidInvite", err)
		}
	})

	t.Run("mismatched_host_id_refused", func(t *testing.T) {
		otherPub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		inv := validInvite(t, "")
		inv.HostID = identity.HostID(otherPub)
		if _, err := inv.Encode(); !errors.Is(err, ErrInvalidInvite) {
			t.Errorf("Encode() error = %v, want ErrInvalidInvite", err)
		}
	})

	t.Run("userinfo_address_refused", func(t *testing.T) {
		inv := validInvite(t, "")
		inv.Addresses = []string{"ws://user:pass@192.168.1.10:7700/fleet"}
		if _, err := inv.Encode(); !errors.Is(err, ErrInvalidInvite) {
			t.Errorf("Encode() error = %v, want ErrInvalidInvite", err)
		}
	})
}

// TestPairingWindowOneSlotFirstSeenAfterOpen drives the window with a fake
// clock: a link seen before the window opened does not consume a slot, the
// first link seen at open time pairs, the one slot is then exhausted, an
// expired window refuses, and Open replaces and resets the window.
func TestPairingWindowOneSlotFirstSeenAfterOpen(t *testing.T) {
	now := time.Unix(1_750_000_000, 0)
	w := NewWindow(func() time.Time { return now })

	open, _, remaining := w.Status()
	if open || remaining != 0 {
		t.Fatalf("fresh window Status() = open %t, remaining %d, want closed", open, remaining)
	}

	openedAt, expires := w.Open(0, 0)
	if got := expires.Sub(openedAt); got != DefaultWindow {
		t.Errorf("Open(0,0) duration = %s, want %s (default)", got, DefaultWindow)
	}
	open, _, remaining = w.Status()
	if !open || remaining != DefaultSlots {
		t.Fatalf("after Open Status() = open %t, remaining %d, want open with %d", open, remaining, DefaultSlots)
	}

	if w.Take(openedAt.Add(-time.Second)) {
		t.Error("Take(before openedAt) = true, want false")
	}
	if _, _, remaining = w.Status(); remaining != DefaultSlots {
		t.Errorf("a pre-window Take consumed a slot: remaining = %d, want %d", remaining, DefaultSlots)
	}

	if !w.Take(openedAt) {
		t.Error("Take(at openedAt) = false, want true")
	}
	if w.Take(openedAt.Add(time.Second)) {
		t.Error("second Take() = true, want the single slot exhausted")
	}
	if open, _, _ = w.Status(); open {
		t.Error("exhausted window Status() open = true, want false")
	}

	now = expires.Add(time.Second)
	if w.Take(openedAt.Add(2 * time.Second)) {
		t.Error("Take() after expiry = true, want false")
	}

	openedAt2, _ := w.Open(5*time.Minute, 2)
	if !w.Take(openedAt2) {
		t.Error("Take() after reopen = false, want true")
	}
	if _, _, remaining = w.Status(); remaining != 1 {
		t.Errorf("after reopen and one Take, remaining = %d, want 1", remaining)
	}

	w.Close()
	if open, _, _ = w.Status(); open {
		t.Error("closed window Status() open = true, want false")
	}
}

// TestPairingSASMatchesBothSides checks the SAS derivation is symmetric in
// its inputs' values but not in their order, and always six digits.
func TestPairingSASMatchesBothSides(t *testing.T) {
	callerPub := make([]byte, 32)
	calleePub := make([]byte, 32)
	if _, err := rand.Read(callerPub); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(calleePub); err != nil {
		t.Fatal(err)
	}
	nonceC := make([]byte, 32)
	nonceA := make([]byte, 32)
	if _, err := rand.Read(nonceC); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(nonceA); err != nil {
		t.Fatal(err)
	}

	want := SAS(callerPub, calleePub, nonceC, nonceA)
	if got := SAS(callerPub, calleePub, nonceC, nonceA); got != want {
		t.Errorf("SAS is not deterministic: %q vs %q", got, want)
	}
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(want) {
		t.Errorf("SAS %q does not match ^[0-9]{6}$", want)
	}

	differ := 0
	for i := 0; i < 5; i++ {
		nonce := make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		if got := SAS(calleePub, callerPub, nonceC, nonceA); got != want {
			differ++
		}
		if got := SAS(callerPub, calleePub, nonceC, nonce); got != want {
			differ++
		}
	}
	if differ < 4 {
		t.Errorf("changed inputs matched the original SAS %d times, want at most 1-in-10 chance (differ=%d)", 10-differ, differ)
	}
}
