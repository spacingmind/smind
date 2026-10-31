package link

import (
	"errors"
	"strings"
	"testing"
)

func TestDirectCleartextRefusedOutsideOverlay(t *testing.T) {
	allowed := []string{
		"wss://example.com:1",
		"ws://127.0.0.1:1",
		"ws://localhost:1",
		"ws://[::1]:1",
		"ws://100.64.0.1:1",
		"ws://100.127.255.254:1",
		"ws://[fd7a:115c:a1e0::1]:1",
	}
	for _, raw := range allowed {
		if warning, err := CheckDirectRoute(raw, false); err != nil || warning != "" {
			t.Errorf("CheckDirectRoute(%q, false) = %q, %v; want ok with no warning", raw, warning, err)
		}
	}

	refused := []string{
		"ws://100.128.0.1:1", // one past the CGNAT range
		"ws://192.168.1.5:4648",
		"ws://10.0.0.2:1",
		"ws://host.example:1",
	}
	for _, raw := range refused {
		warning, err := CheckDirectRoute(raw, false)
		if !errors.Is(err, ErrCleartext) {
			t.Errorf("CheckDirectRoute(%q, false) = %q, %v; want ErrCleartext", raw, warning, err)
		}
		if warning != "" {
			t.Errorf("CheckDirectRoute(%q, false) warned %q on refusal", raw, warning)
		}
		warning, err = CheckDirectRoute(raw, true)
		if err != nil || !strings.Contains(warning, "readable by anyone on the network") || !strings.Contains(warning, hostOf(raw)) {
			t.Errorf("CheckDirectRoute(%q, true) = %q, %v; want ok with a warning naming the host", raw, warning, err)
		}
	}

	// Structural errors stay errors even with allowCleartext: a wrong scheme
	// or credentials in the URL is not a privacy question.
	for _, raw := range []string{"http://x", "ws://user:pw@127.0.0.1:1", "ws://", "://"} {
		for _, allow := range []bool{false, true} {
			if _, err := CheckDirectRoute(raw, allow); err == nil {
				t.Errorf("CheckDirectRoute(%q, %v) succeeded; want an error", raw, allow)
			}
		}
	}
}

// hostOf returns the host:port of raw so a table entry's warning check does
// not repeat the URL by hand.
func hostOf(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		return raw[i+3:]
	}
	return raw
}
