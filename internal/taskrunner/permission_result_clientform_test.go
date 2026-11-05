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
	elided, title, result, liveRefs := live.Elide("c", in, "cat > f <<'EOF'\nsecret body\nEOF", json.RawMessage(`"ok"`))
	if string(elided) == string(in) {
		t.Fatal("setup: the live elider did not elide")
	}
	cf := NewClientFormElider(map[string][]HeredocRef{"c": liveRefs})
	got, gtitle, gresult, refs := cf.Elide("c", elided, title, result)
	if string(got) != string(elided) || gtitle != title || string(gresult) != string(result) || len(refs) != len(liveRefs) || len(refs) == 0 {
		t.Fatalf("client-form elider changed an elided call or lost its refs: %s | %q | %v vs %v", got, gtitle, refs, liveRefs)
	}
	// the refs ride only with the event that carries the input, as live
	if _, _, _, r2 := cf.Elide("c", nil, "", json.RawMessage(`"ok"`)); r2 != nil {
		t.Fatalf("a result event returned refs: %v", r2)
	}
	// it must not hide what it was never given to hide
	if raw, _, _, _ := NewClientFormElider(nil).Elide("c", in, "", nil); string(raw) != string(in) {
		t.Fatal("a client-form elider is a pass-through, not a second elider")
	}
}
