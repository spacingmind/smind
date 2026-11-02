package taskrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/permpolicy"
)

// ToolCallElider turns tool-call events into their client-delivery form for
// one stream (a run.attach subscription or one run.logs read). It learns, from
// the event that carries a call's input, which heredoc bodies that call's
// command elides, so a LATER update of the same call (which usually omits the
// input) is elided too: its title, and its result, which can echo a body when
// the command prints it. Use one elider per ordered pass over a run's events.
type ToolCallElider struct {
	calls map[string]*elidedCall
}

type elidedCall struct {
	cmds   []string
	bodies []permpolicy.HeredocBody
}

// NewToolCallElider returns an empty elider.
func NewToolCallElider() *ToolCallElider {
	return &ToolCallElider{calls: map[string]*elidedCall{}}
}

// Elide returns what a client may see of one tool-call event. Calls without
// heredoc bodies come back byte for byte.
func (el *ToolCallElider) Elide(toolCallID string, input json.RawMessage, title string, result json.RawMessage) (json.RawMessage, string, json.RawMessage, []HeredocRef) {
	outInput, outTitle, refs := ElideToolCall(input, title)
	if toolCallID != "" {
		if cmds, bodies := commandBodies(input); len(bodies) > 0 {
			el.calls[toolCallID] = &elidedCall{cmds: cmds, bodies: bodies}
		}
	}
	call := el.calls[toolCallID]
	if call == nil || toolCallID == "" {
		return outInput, outTitle, result, refs
	}
	for _, cmd := range call.cmds {
		outTitle = elideCommandInText(outTitle, cmd)
	}
	return outInput, outTitle, elideToolResult(result, call.bodies), refs
}

// commandBodies returns the raw command strings of an input (command, cmd) and
// the heredoc bodies of the first one that has any, in the order ElideToolCall
// numbers them.
func commandBodies(input json.RawMessage) (cmds []string, bodies []permpolicy.HeredocBody) {
	if in := string(input); len(input) == 0 || !strings.Contains(in, "<") && !strings.Contains(in, "\\u003c") {
		return nil, nil
	}
	for _, key := range []string{"command", "cmd"} {
		cmd, ok := StringField(input, key)
		if !ok {
			continue
		}
		_, b := permpolicy.ElideHeredocs(cmd)
		if len(b) == 0 {
			continue
		}
		cmds = append(cmds, cmd)
		if bodies == nil {
			bodies = b
		}
	}
	return cmds, bodies
}

// elideToolResult scrubs a result of a call whose command had heredoc bodies:
// every string in it has the exact body (and, for bodies of 8+ bytes, the body
// without its trailing newline, which is how output usually echoes it)
// replaced by the same `[heredoc N bytes]` marker the command shows, and then
// goes through the redactor the audit and the reveal use. Scoped to these
// calls on purpose: it is the echo of an elided body this closes, not a
// rewrite of every tool's output.
func elideToolResult(result json.RawMessage, bodies []permpolicy.HeredocBody) json.RawMessage {
	if len(result) == 0 || len(bodies) == 0 {
		return result
	}
	type pair struct{ from, to string }
	var pairs []pair
	for _, b := range bodies {
		if b.Text == "" {
			continue
		}
		marker := fmt.Sprintf("[heredoc %d bytes]", b.Bytes)
		pairs = append(pairs, pair{b.Text, marker})
		if t := strings.TrimRight(b.Text, "\n"); len(t) >= 8 && t != b.Text {
			pairs = append(pairs, pair{t, marker})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].from) > len(pairs[j].from) })
	home, _ := os.UserHomeDir()
	var homes []string
	if home != "" {
		homes = []string{home}
	}
	rd := redact.New(nil, homes)
	scrub := func(s string) string {
		for _, p := range pairs {
			s = strings.ReplaceAll(s, p.from, p.to)
		}
		return rd.String(s)
	}
	dec := json.NewDecoder(bytes.NewReader(result))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return result
	}
	out, err := json.Marshal(walkStrings(v, scrub))
	if err != nil {
		return result
	}
	return out
}

// walkStrings applies f to every string leaf of a decoded JSON value, keys
// included only as values (keys are tool-defined field names, not output).
func walkStrings(v any, f func(string) string) any {
	switch t := v.(type) {
	case string:
		return f(t)
	case []any:
		for i := range t {
			t[i] = walkStrings(t[i], f)
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = walkStrings(t[k], f)
		}
		return t
	default:
		return v
	}
}
