package taskrunner

import (
	"encoding/json"
	"strings"
	"testing"
)

func echoInput() json.RawMessage {
	in, _ := json.Marshal(map[string]any{"command": "cat <<'EOF' > notes.md\nline one SECRET_TOKEN_123\nline two\nEOF\ncat notes.md"})
	return in
}

const echoBody = "line one SECRET_TOKEN_123\nline two\n"

func TestToolCallElider_ResultEchoOfABodyBecomesTheMarker(t *testing.T) {
	t.Parallel()
	for name, result := range map[string]string{
		"plain string":     `"` + strings.ReplaceAll(echoBody, "\n", `\n`) + `"`,
		"echo without eol": `"out: line one SECRET_TOKEN_123\nline two"`,
		"content blocks":   `[{"type":"content","content":{"type":"text","text":"line one SECRET_TOKEN_123\nline two\n"}}]`,
		"nested + extra":   `{"stdout":"line one SECRET_TOKEN_123\nline two\ndone","exit":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			el := NewToolCallElider()
			// The event carrying the input comes first; the result arrives on a later one that omits it.
			el.Elide("c1", echoInput(), "Run cat", nil)
			_, _, got, _ := el.Elide("c1", nil, "Run cat <<'EOF' > notes.md\nline one SECRET_TOKEN_123", json.RawMessage(result))
			if strings.Contains(string(got), "SECRET_TOKEN_123") || !strings.Contains(string(got), "[heredoc 35 bytes]") {
				t.Errorf("result = %s", got)
			}
		})
	}
}

func TestToolCallElider_LaterTitleWithoutInputIsElidedToo(t *testing.T) {
	t.Parallel()
	el := NewToolCallElider()
	el.Elide("c1", echoInput(), "x", nil)
	_, title, _, _ := el.Elide("c1", nil, "Run cat <<'EOF' > notes.md\nline one SECRET_TOKEN_123\nline two\nEOF\ncat notes.md", nil)
	if strings.Contains(title, "SECRET_TOKEN_123") {
		t.Errorf("title = %q", title)
	}
}

func TestToolCallElider_ResultIsRedactedAndOtherCallsUntouched(t *testing.T) {
	t.Parallel()
	el := NewToolCallElider()
	el.Elide("c1", echoInput(), "x", nil)
	// A token-shaped string that is not the body is redacted in a heredoc call's result...
	_, _, got, _ := el.Elide("c1", nil, "", json.RawMessage(`"token ghp_abcdefghijklmnopqrstuvwxyz0123456789 done"`))
	if strings.Contains(string(got), "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("redaction missing: %s", got)
	}
	// ...but a call without heredocs, or an unknown call id, is returned byte for byte.
	plain := json.RawMessage(`{"command":"go test ./..."}`)
	res := json.RawMessage(`"token ghp_abcdefghijklmnopqrstuvwxyz0123456789 and  spacing"`)
	_, _, same, _ := el.Elide("c2", plain, "t", res)
	_, _, same2, _ := el.Elide("unknown", nil, "t", res)
	if string(same) != string(res) || string(same2) != string(res) {
		t.Errorf("untouched results changed: %s / %s", same, same2)
	}
}

func TestToolCallElider_NumbersAndShortBodies(t *testing.T) {
	t.Parallel()
	in, _ := json.Marshal(map[string]any{"command": "cat <<EOF\nok\nEOF\n"})
	el := NewToolCallElider()
	el.Elide("c1", in, "", nil)
	// A short body is replaced only as the exact body text (with its newline), never as a bare word.
	_, _, got, _ := el.Elide("c1", nil, "", json.RawMessage(`{"n":12345678901234567890,"msg":"all ok here"}`))
	if !strings.Contains(string(got), "12345678901234567890") || !strings.Contains(string(got), "all ok here") {
		t.Errorf("result = %s", got)
	}
}
