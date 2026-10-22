package runs

import (
	"encoding/json"
	"reflect"
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

// TestEventCodec_McpServersRoundTrip: an EventTypeMcpServers event's
// McpServers survive encode/decode byte for byte (ADR-0018's per-run
// indicator, M4).
func TestEventCodec_McpServersRoundTrip(t *testing.T) {
	t.Parallel()
	want := taskrunner.Event{
		Type: taskrunner.EventTypeMcpServers,
		McpServers: []taskrunner.McpServerState{
			{Name: "pw", Transport: "stdio", Status: taskrunner.McpStatusActive},
			{Name: "remote", Transport: "http", Status: taskrunner.McpStatusDropped, Reason: taskrunner.McpReasonTransportUnsupported},
		},
	}
	data, err := encodeEvent(want)
	if err != nil {
		t.Fatalf("encodeEvent() error = %v", err)
	}
	if !strings.Contains(data, `"mcpServers"`) {
		t.Fatalf("encodeEvent() = %s, want an mcpServers field", data)
	}
	got, err := decodeEvent(data)
	if err != nil {
		t.Fatalf("decodeEvent() error = %v", err)
	}
	if !reflect.DeepEqual(got.McpServers, want.McpServers) {
		t.Fatalf("decodeEvent() McpServers = %+v, want %+v", got.McpServers, want.McpServers)
	}
}

func TestDecodeStoredEvent_RoundTrip(t *testing.T) {
	t.Parallel()
	in := Event{Type: taskrunner.EventTypeText, Text: "héllo \"x\"\n"}
	data, err := encodeEvent(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeStoredEvent(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || out.Text != in.Text {
		t.Fatalf("DecodeStoredEvent = %+v, want %+v", out, in)
	}
	if _, err := DecodeStoredEvent("{not json"); err == nil {
		t.Fatal("DecodeStoredEvent(garbage) succeeded")
	}
}

// TestEncodeDecodeEvent_PermissionResolution_RoundTrips: how a permission was
// decided (a person, Auto-accept, a timeout) is part of the event. Before it
// was persisted, a finished run replayed from run_events (run.logs, the
// learning window) lost it: the timeline showed no badge and a human refusal
// was indistinguishable from an auto-resolution.
func TestEncodeDecodeEvent_PermissionResolution_RoundTrips(t *testing.T) {
	t.Parallel()

	for _, res := range []taskrunner.PermissionResolution{
		taskrunner.PermissionResolvedByHuman,
		taskrunner.PermissionResolvedByAutoAccept,
		taskrunner.PermissionResolvedByTimeout,
	} {
		want := taskrunner.Event{
			Type:                 taskrunner.EventTypePermissionResolved,
			PermissionRequestID:  "req-1",
			PermissionOptionID:   "deny",
			PermissionResolution: res,
		}
		data, err := encodeEvent(want)
		if err != nil {
			t.Fatalf("encodeEvent(%s) error = %v", res, err)
		}
		got, err := decodeEvent(data)
		if err != nil {
			t.Fatalf("decodeEvent(%s) error = %v", res, err)
		}
		if got.PermissionResolution != res || got.PermissionOptionID != "deny" || got.PermissionRequestID != "req-1" {
			t.Fatalf("round trip of %q = %+v, want resolution %q", res, got, res)
		}
	}
}
