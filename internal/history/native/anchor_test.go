package native

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// withDistinctUUIDs rewrites the fixture so every record carries its own uuid
// ("rec-<line>"), as a real transcript does; the checked-in fixture reuses one
// placeholder.
func withDistinctUUIDs(t *testing.T, e *env, keep bool) {
	t.Helper()
	b, err := os.ReadFile(e.file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if keep {
			l = strings.Replace(l, `"uuid":"a"`, fmt.Sprintf(`"uuid":"rec-%d"`, i), 1)
			l = strings.Replace(l, `"uuid":"u"`, fmt.Sprintf(`"uuid":"rec-%d"`, i), 1)
		} else {
			l = strings.Replace(l, `"uuid":"a",`, "", 1)
			l = strings.Replace(l, `"uuid":"a"`, `"x":1`, 1)
		}
		lines[i] = l
	}
	if err := os.WriteFile(e.file, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The anchor is the uuid of the last assistant record of the matched turn: not a
// sidechain record inside it, not a record of another turn.
func TestAnchor_ClaudeLastAssistantUUIDOfTheMatchedTurn(t *testing.T) {
	e := newClaude(t)
	withDistinctUUIDs(t, e, true)
	res := e.rebuild()
	if !res.OK() {
		t.Fatalf("rebuild: %q (%s)", res.Reason, res.Detail)
	}
	// Fixture line 19 (0-based, 09:10:09) is the last non-sidechain assistant
	// record of the 09:10:00 turn; line 12 is a sidechain record inside it.
	if res.Anchor != "rec-19" {
		t.Fatalf("Anchor = %q, want rec-19 (the turn's last main-thread assistant record)", res.Anchor)
	}
}

// A transcript whose records carry no uuid cannot be forked at a turn: the
// rebuild still works, the anchor is empty, and the caller refuses the fork.
func TestAnchor_MissingUUIDIsEmpty(t *testing.T) {
	e := newClaude(t)
	withDistinctUUIDs(t, e, false)
	res := e.rebuild()
	if !res.OK() {
		t.Fatalf("rebuild: %q (%s)", res.Reason, res.Detail)
	}
	if res.Anchor != "" {
		t.Fatalf("Anchor = %q, want empty for a transcript without uuids", res.Anchor)
	}
}

// Two turns with the same prompt text in the run's window cannot be told apart.
func TestAnchor_SliceNotFoundOnDuplicatePrompt(t *testing.T) {
	e := newClaude(t)
	withDistinctUUIDs(t, e, true)
	e.ref.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T08:00:00Z"), FinishedAt: at("2026-11-05T10:00:00Z")}
	b, err := os.ReadFile(e.file)
	if err != nil {
		t.Fatal(err)
	}
	// Repeat the whole target turn (lines 7..19) once more inside the window.
	lines := strings.Split(string(b), "\n")
	dup := append([]string{}, lines[7:20]...)
	out := append(append([]string{}, lines[:20]...), dup...)
	out = append(out, lines[20:]...)
	if err := os.WriteFile(e.file, []byte(strings.Join(out, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	res := e.rebuild()
	if res.OK() || res.Reason != ReasonSliceNotFound || res.Anchor != "" {
		t.Fatalf("rebuild = %q anchor %q, want slice_not_found and no anchor", res.Reason, res.Anchor)
	}
}

func TestAnchor_CodexHasNone(t *testing.T) {
	res := newCodex(t).rebuild()
	if !res.OK() || res.Anchor != "" {
		t.Fatalf("codex rebuild = %q anchor %q, want OK with no anchor", res.Reason, res.Anchor)
	}
}
