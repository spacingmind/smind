package runs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestEncodeDecodeEvent_RawPayload_RoundTripsLargeNestedPayload proves
// encodeEvent/decodeEvent round-trip an EventTypeRaw's RawPayload byte for
// byte (docs/decisions/0010-preserve-unknown-acp-event-kinds.md) even for a
// payload deep and large enough to expose truncation (a column/buffer
// limit) or double-encoding (RawPayload accidentally re-marshaled as a
// JSON string instead of embedded as raw JSON) -- neither of which the
// existing fakeagent-driven round-trip test exercises, since its "plan"
// payload is small and shallow.
func TestEncodeDecodeEvent_RawPayload_RoundTripsLargeNestedPayload(t *testing.T) {
	t.Parallel()

	// Deeply nested (10 levels) and large (1000 entries at the leaf),
	// plus a string value carrying characters that must survive both a
	// JSON round-trip and (in production) a SQLite TEXT column: quotes,
	// backslashes, a literal newline, and non-ASCII text.
	entries := make([]map[string]any, 1000)
	for i := range entries {
		entries[i] = map[string]any{
			"content": `line with "quotes", \backslashes\, a` + "\n" + `newline, and 日本語`,
			"status":  "pending",
			"index":   i,
		}
	}
	nested := map[string]any{"entries": entries}
	for i := 0; i < 10; i++ {
		nested = map[string]any{"level": i, "child": nested}
	}
	payload, err := json.Marshal(nested)
	if err != nil {
		t.Fatalf("json.Marshal(nested) error = %v", err)
	}

	want := taskrunner.Event{
		Type:       taskrunner.EventTypeRaw,
		RawKind:    "plan",
		RawPayload: json.RawMessage(payload),
	}

	data, err := encodeEvent(want)
	if err != nil {
		t.Fatalf("encodeEvent() error = %v", err)
	}
	// The persisted form must embed the payload as raw JSON, not
	// re-encode it as a JSON string (which would double-escape every
	// quote/backslash into unreadable mush and silently pass a naive
	// byte-equality check on the wrong representation).
	if strings.Contains(data, `\"level\"`) {
		t.Fatalf("encodeEvent() double-encoded rawPayload: %s", data)
	}

	got, err := decodeEvent(data)
	if err != nil {
		t.Fatalf("decodeEvent() error = %v", err)
	}
	if got.Type != want.Type || got.RawKind != want.RawKind {
		t.Fatalf("decodeEvent() = %+v, want Type/RawKind %+v", got, want)
	}
	if string(got.RawPayload) != string(want.RawPayload) {
		t.Fatalf("decodeEvent() RawPayload did not round-trip byte for byte\ngot  len=%d\nwant len=%d", len(got.RawPayload), len(want.RawPayload))
	}
}
