package acp

import (
	"strings"
	"testing"
)

func TestRPCErrorIncludesData(t *testing.T) {
	t.Parallel()
	if got := (&RPCError{Code: -32603, Message: "Internal error"}).Error(); got != "acp: rpc error -32603: Internal error" {
		t.Errorf("no data: Error() = %q, want the unchanged message", got)
	}
	if got := (&RPCError{Code: -32603, Message: "Internal error", Data: "rate limited:\nretry after 17:35"}).Error(); got != "acp: rpc error -32603: Internal error (rate limited: retry after 17:35)" {
		t.Errorf("string data: Error() = %q", got)
	}
	if got := (&RPCError{Code: 1, Message: "m", Data: map[string]any{"reason": "quota"}}).Error(); got != `acp: rpc error 1: m ({"reason":"quota"})` {
		t.Errorf("object data: Error() = %q", got)
	}
	long := strings.Repeat("y", 500)
	if got := (&RPCError{Code: 1, Message: "m", Data: long}).Error(); !strings.HasSuffix(got, "…)") || len([]rune(got)) > 400 {
		t.Errorf("long data not bounded: %d runes", len([]rune(got)))
	}
}
