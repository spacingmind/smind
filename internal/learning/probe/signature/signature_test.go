package signature

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type evb struct {
	seq, id int64
	out     []window.Event
}

func (b *evb) add(e taskrunner.Event) int64 {
	b.seq++
	b.id += 10
	b.out = append(b.out, window.Event{ID: b.id, Seq: b.seq, E: e})
	return b.seq
}

func raw(v any) json.RawMessage { x, _ := json.Marshal(v); return x }

func (b *evb) tool(id, name, status string, input, result any) int64 {
	e := taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: name, ToolStatus: status}
	if input != nil {
		e.ToolInput = raw(input)
	}
	if result != nil {
		e.ToolResult = raw(result)
	}
	return b.add(e)
}

func recov(seq int64) gate.Evidence {
	return gate.Evidence{Kind: gate.EvFailureRecovery, RunID: "r", Seq: seq}
}

// failThenFix builds a failing and a recovering Bash call; returns events and
// the success call's first-seen seq.
func failThenFix(msg string) ([]window.Event, int64) {
	b := &evb{}
	b.tool("a", "Bash", taskrunner.ToolStatusRunning, map[string]any{"command": "go test"}, nil)
	b.tool("a", "Bash", taskrunner.ToolStatusFailure, nil, msg)
	s := b.tool("b", "Bash", taskrunner.ToolStatusRunning, map[string]any{"command": "go test"}, nil)
	b.tool("b", "Bash", taskrunner.ToolStatusSuccess, nil, "ok")
	return b.out, s
}

func TestSignature_FailureRecoveryNormalises(t *testing.T) {
	evs, seq := failThenFix("\n  Exit status 2: /tmp/x/foo.go:42 undefined: Foo\nmore")
	sig, ok := Derive(recov(seq), evs)
	if !ok {
		t.Fatal("no signature")
	}
	if sig.Kind != gate.EvFailureRecovery || sig.Tool != "Bash" {
		t.Fatalf("sig = %+v", sig)
	}
	if sig.Detail != "exit status #: <path> undefined: foo" {
		t.Fatalf("detail = %q", sig.Detail)
	}
	if Count(evs, sig) != 1 {
		t.Fatalf("count = %d", Count(evs, sig))
	}
	if !strings.HasPrefix(sig.Label(), "Bash failed: ") {
		t.Fatalf("label = %q", sig.Label())
	}
}

func TestSignature_PermissionDeniedUsesToolAndKind(t *testing.T) {
	b := &evb{}
	b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "p1", PermissionSummary: "Run rm -rf /tmp/build123"})
	rs := b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionResolved, PermissionRequestID: "p1"})
	sig, ok := Derive(gate.Evidence{Kind: gate.EvPermissionDenied, Seq: rs}, b.out)
	if !ok || sig.Tool != "" || sig.Kind != gate.EvPermissionDenied {
		t.Fatalf("sig = %+v ok=%v", sig, ok)
	}
	if sig.Detail != "run rm -rf <path>" {
		t.Fatalf("detail = %q", sig.Detail)
	}
	if Count(b.out, sig) != 1 {
		t.Fatalf("count = %d", Count(b.out, sig))
	}
	if sig.Label() != "permission request: run rm -rf <path>" {
		t.Fatalf("label = %q", sig.Label())
	}
}

func TestSignature_NoneForUserMessageAndLoadedSkillWrong(t *testing.T) {
	evs, seq := failThenFix("boom")
	for _, k := range []string{gate.EvUserMessage, gate.EvLoadedSkillWrong} {
		if _, ok := Derive(gate.Evidence{Kind: k, Seq: seq}, evs); ok {
			t.Fatalf("kind %s derived a signature", k)
		}
	}
}

func TestSignature_StableAcrossPathsAndNumbers(t *testing.T) {
	a, sa := failThenFix("open /tmp/abc123/x.txt: pid 4242 hash deadbeef1234 line 7: no such file")
	b, sb := failThenFix("open /var/folders/zz/y.txt: pid 99 hash cafebabe9999 line 1203: no such file")
	s1, ok1 := Derive(recov(sa), a)
	s2, ok2 := Derive(recov(sb), b)
	if !ok1 || !ok2 || s1.Key() != s2.Key() {
		t.Fatalf("keys differ: %q vs %q", s1.Key(), s2.Key())
	}
	if Count(b, s1) != 1 {
		t.Fatal("count across runs should match")
	}
}

func TestSignature_SecretPathNeverDerived(t *testing.T) {
	b := &evb{}
	b.tool("a", "Read", taskrunner.ToolStatusFailure, map[string]any{"path": "/home/u/.env"}, "permission denied")
	s := b.tool("b", "Read", taskrunner.ToolStatusSuccess, map[string]any{"path": "/home/u/.env"}, "ok")
	if _, ok := Derive(recov(s), b.out); ok {
		t.Fatal("secret path derived")
	}
	sig := Signature{Kind: gate.EvFailureRecovery, Tool: "Read", Detail: "permission denied"}
	if Count(b.out, sig) != 0 {
		t.Fatal("secret-path failure counted")
	}
}

func TestSignature_NoFailureFound(t *testing.T) {
	b := &evb{}
	s := b.tool("b", "Bash", taskrunner.ToolStatusSuccess, map[string]any{"command": "ls"}, "ok")
	if _, ok := Derive(recov(s), b.out); ok {
		t.Fatal("derived without failure")
	}
	// Different tool name: not a recovery.
	c := &evb{}
	c.tool("a", "Bash", taskrunner.ToolStatusFailure, nil, "x")
	s = c.tool("b", "Edit", taskrunner.ToolStatusSuccess, nil, "ok")
	if _, ok := Derive(recov(s), c.out); ok {
		t.Fatal("derived across tools")
	}
	// Failure with empty result text.
	d := &evb{}
	d.tool("a", "Bash", taskrunner.ToolStatusFailure, nil, nil)
	s = d.tool("b", "Bash", taskrunner.ToolStatusSuccess, nil, "ok")
	if _, ok := Derive(recov(s), d.out); ok {
		t.Fatal("derived with empty detail")
	}
}

func TestSignature_RedactRunsBeforeNormalise(t *testing.T) {
	evs, seq := failThenFix("bad token SECRETSECRET1234 rejected")
	red := func(s string) string { return strings.ReplaceAll(s, "SECRETSECRET1234", "[REDACTED]") }
	sig, ok := DeriveWith(recov(seq), evs, red)
	if !ok || strings.Contains(sig.Detail, "secret") || !strings.Contains(sig.Detail, "[redacted]") {
		t.Fatalf("sig = %+v ok=%v", sig, ok)
	}
	if n := len(MatchSeqs(evs, sig, red)); n != 1 {
		t.Fatalf("matches = %d, want 1", n)
	}
}
