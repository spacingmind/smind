package taskrunner

import (
	"encoding/json"
	"testing"
)

// A rebuilt history is already in client form; eliding it again would mangle it,
// so the client-form elider must hand it back unchanged while the live one elides.
func TestClientFormElidersPassesElidedCallsThrough(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"command": "cat > f <<'EOF'\nsecret body\nEOF\necho done"})
	live := NewToolCallElider()
	elided, title, result, _ := live.Elide("c", in, "cat > f <<'EOF'\nsecret body\nEOF", json.RawMessage(`"ok"`))
	if string(elided) == string(in) {
		t.Fatal("setup: the live elider did not elide")
	}
	cf := NewClientFormElider()
	got, gtitle, gresult, refs := cf.Elide("c", elided, title, result)
	if string(got) != string(elided) || gtitle != title || string(gresult) != string(result) || refs != nil {
		t.Fatalf("client-form elider changed an elided call: %s | %q", got, gtitle)
	}
	// it must not hide what it was never given to hide
	if raw, _, _, _ := NewClientFormElider().Elide("c", in, "", nil); string(raw) != string(in) {
		t.Fatal("a client-form elider is a pass-through, not a second elider")
	}
}
