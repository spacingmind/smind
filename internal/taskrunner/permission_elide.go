package taskrunner

import (
	"encoding/json"
	"strings"

	"github.com/spacingmind/smind/internal/permpolicy"
)

// HeredocRef is the wire form of one elided heredoc body: its position among
// the command's bodies (source order, also the index the reveal calls take)
// and its length. The body itself never travels with it.
type HeredocRef struct {
	Index int `json:"index"`
	Bytes int `json:"bytes"`
}

// setExecuteCommand fills d from a provider's raw command. Command is the
// elided form (heredoc bodies become size markers), capped for the card; the
// raw bodies and the capped raw command stay in unexported-by-JSON fields so
// the policy engine judges exactly what it judged before and a human can reveal
// a body later. A card never carries a body whatever the policy engine does.
func (d *PermissionDetail) setExecuteCommand(cmd string) {
	shown, bodies := permpolicy.ElideHeredocs(cmd)
	var capped bool
	d.Command, capped = capBytes(shown, detailCommandMaxBytes)
	d.PolicyCommand, d.PolicyTruncated = capBytes(cmd, detailCommandMaxBytes)
	d.Truncated = d.PolicyTruncated || capped
	for i, b := range bodies {
		d.Heredocs = append(d.Heredocs, HeredocRef{Index: i, Bytes: b.Bytes})
		d.RevealBodies = append(d.RevealBodies, b.Text)
	}
}

// elideCommandInText replaces cmd inside text (a provider's title or summary,
// which usually embeds the command) with its elided form. A title that was
// cut mid-command no longer contains cmd, so when the command has heredoc
// bodies and the text still shows the head of the first line of any of them, the whole
// text is replaced by the elided command.
func elideCommandInText(text, cmd string) string {
	shown, bodies := permpolicy.ElideHeredocs(cmd)
	if len(bodies) == 0 && shown == cmd {
		return text
	}
	if cmd != "" && strings.Contains(text, cmd) {
		return strings.ReplaceAll(text, cmd, shown)
	}
	for _, b := range bodies {
		line, _, _ := strings.Cut(strings.TrimSpace(b.Text), "\n")
		if len(line) > 12 {
			line = line[:12] // a cut title keeps only the head of the body
		}
		if line != "" && strings.Contains(text, line) {
			return shown
		}
	}
	return text
}

// ElideToolCall is the client-delivery form of a tool call: when its input
// carries a string command (command or cmd) with heredoc bodies, the bodies
// are replaced by size markers in the input and the title, and refs lists
// them. Anything else is returned byte for byte. The raw call stays in the
// daemon's own run history and log; a client only ever sees this form.
func ElideToolCall(input json.RawMessage, title string) (json.RawMessage, string, []HeredocRef) {
	// Cheap pre-check: a heredoc needs "<<", which JSON may spell \u003c.
	if in := string(input); len(input) == 0 || !strings.Contains(in, "<") && !strings.Contains(in, "\\u003c") {
		return input, title, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(input, &obj) != nil {
		return input, title, nil
	}
	var refs []HeredocRef
	changed := false
	for _, key := range []string{"command", "cmd"} {
		var cmd string
		if raw, ok := obj[key]; !ok || json.Unmarshal(raw, &cmd) != nil {
			continue
		}
		shown, bodies := permpolicy.ElideHeredocs(cmd)
		if len(bodies) == 0 && shown == cmd {
			continue
		}
		enc, _ := json.Marshal(shown)
		obj[key] = enc
		title = elideCommandInText(title, cmd)
		if len(refs) == 0 {
			for i, b := range bodies {
				refs = append(refs, HeredocRef{Index: i, Bytes: b.Bytes})
			}
		}
		changed = true
	}
	if !changed {
		return input, title, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return input, title, nil
	}
	return out, title, refs
}
