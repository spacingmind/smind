package devices

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestPendingDeviceFromBeforeTheWindowDoesNotTakeASlot: a device that was
// already waiting when the owner printed the offer is not the one being
// paired (an attacker who registered early must not be first in line); the
// device that appears after the window opened is.
func TestPendingDeviceFromBeforeTheWindowDoesNotTakeASlot(t *testing.T) {
	r, clk, _ := openReg(t)
	early := newDev(t, "early")
	if dec := admit(t, r, early); dec.Approved {
		t.Fatal("no window yet: must be pending")
	}
	clk.Advance(time.Minute)
	if err := r.OpenWindow(10*time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	if dec := admit(t, r, early); dec.Approved {
		t.Fatal("a device pending from before the window took the pairing slot")
	}
	clk.Advance(time.Second)
	fresh := newDev(t, "fresh")
	if dec := admit(t, r, fresh); !dec.Approved {
		t.Fatalf("the device that appeared after the window opened must be approved: %+v", dec)
	}
}

func TestTooManyPendingDevicesAreRefused(t *testing.T) {
	r, _, _ := openReg(t)
	for i := 0; i < MaxPending; i++ {
		admit(t, r, newDev(t, fmt.Sprintf("d%d", i)))
	}
	extra := newDev(t, "one-too-many")
	if _, err := r.Admit(extra.id, extra.pub, extra.label); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("Admit past MaxPending = %v, want ErrTooManyPending", err)
	}
	// A known pending device is unaffected, and approving frees room.
	first := r.List()[0]
	if _, err := r.Approve(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Admit(extra.id, extra.pub, extra.label); err != nil {
		t.Fatalf("Admit after room was freed: %v", err)
	}
}

func TestLabelsWithInvisibleOrSpoofingCharactersAreRejected(t *testing.T) {
	for name, label := range map[string]string{
		"bidi override":   "phone‮evil",
		"zero width":      "ph​one",
		"line separator":  "a b",
		"private use":     "ab",
		"newline":         "a\nb",
		"escape sequence": "a\x1b[31mb",
	} {
		if err := ValidateLabel(label); !errors.Is(err, ErrBadLabel) {
			t.Errorf("%s: ValidateLabel = %v, want ErrBadLabel", name, err)
		}
	}
	if err := ValidateLabel("Long’s MacBook Pro — office"); err != nil {
		t.Errorf("an ordinary label with punctuation and accents was rejected: %v", err)
	}
}
