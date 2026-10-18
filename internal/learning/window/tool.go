package window

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	readFailureBytes = 200
	withheldNote     = "result withheld (secret path)"
)

var secretPathRe = regexp.MustCompile(`(^|/)(\.env[^/]*|[^/]*\.pem|[^/]*\.key|id_[a-z0-9_]+|[^/]*credentials[^/]*|[^/]*\.p12|\.netrc|\.npmrc|[^/]*keychain[^/]*)$`)

// toolCall is one tool call merged from its events by ToolCallID.
type toolCall struct {
	id, name, title, status string
	input, result           json.RawMessage
	seq                     int64 // first-seen seq
	resultSeq               int64
}

func (c *toolCall) merge(seq int64, e taskrunner.Event) {
	if e.ToolName != "" {
		c.name = e.ToolName
	}
	if e.ToolTitle != "" {
		c.title = e.ToolTitle
	}
	if e.ToolStatus != "" {
		c.status = e.ToolStatus
	}
	if len(e.ToolInput) > 0 {
		c.input = e.ToolInput
	}
	if len(e.ToolResult) > 0 {
		c.result = e.ToolResult
	}
}

func (c *toolCall) inputMap() map[string]any {
	var m map[string]any
	if len(c.input) > 0 {
		_ = json.Unmarshal(c.input, &m)
	}
	return m
}

func lowerName(n string) string { return strings.ToLower(n) }

func isEditTool(name string) bool {
	switch n := lowerName(name); n {
	case "edit", "multiedit", "write", "notebookedit", "edit_file", "write_file":
		return true
	default:
		return strings.HasPrefix(n, "str_replace")
	}
}

func isReadTool(name string) bool {
	switch lowerName(name) {
	case "read", "read_file", "view":
		return true
	}
	return false
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// pathOf returns the path the call's input carries, or "".
func pathOf(m map[string]any) string {
	return strField(m, "file_path", "path", "notebook_path")
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

var (
	oldKeys = []string{"old_string", "old_text", "oldText", "old_str", "oldString"}
	newKeys = []string{"new_string", "new_text", "newText", "new_str", "newString", "content", "new_source"}
)

// editCounts returns the added/removed line counts of an edit input; contents
// are never rendered.
func editCounts(m map[string]any) (added, removed int) {
	one := func(e map[string]any) {
		removed += lineCount(strField(e, oldKeys...))
		added += lineCount(strField(e, newKeys...))
	}
	one(m)
	if edits, ok := m["edits"].([]any); ok {
		for _, e := range edits {
			if em, ok := e.(map[string]any); ok {
				one(em)
			}
		}
	}
	return
}

// isSecretPath reports whether s names a secret-shaped path.
func isSecretPath(s string) bool {
	if strings.Contains(s, "/.ssh/") || strings.Contains(s, "/.aws/") || strings.Contains(s, "/.gnupg/") {
		return true
	}
	return secretPathRe.MatchString(s)
}

func stringsIn(v any, visit func(string)) {
	switch x := v.(type) {
	case string:
		visit(x)
	case []any:
		for _, e := range x {
			stringsIn(e, visit)
		}
	case map[string]any:
		for _, e := range x {
			stringsIn(e, visit)
		}
	}
}

// touchesSecretPath reports whether the call's input (or title) mentions a
// secret-shaped path, whole or as a whitespace-separated token (shell commands).
func (c *toolCall) touchesSecretPath() bool {
	hit := false
	check := func(s string) {
		if hit {
			return
		}
		if isSecretPath(s) {
			hit = true
			return
		}
		for _, tok := range strings.Fields(s) {
			if isSecretPath(strings.Trim(tok, "\"'`;()<>|&")) {
				hit = true
				return
			}
		}
	}
	if len(c.input) > 0 {
		var v any
		if json.Unmarshal(c.input, &v) == nil {
			stringsIn(v, check)
		} else {
			check(string(c.input))
		}
	}
	check(c.title)
	return hit
}

// resultText flattens a provider-native tool result into plain text.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	var parts []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			parts = append(parts, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			if t, ok := x["text"].(string); ok {
				parts = append(parts, t)
			}
			if c, ok := x["content"]; ok {
				walk(c)
			}
		}
	}
	walk(v)
	return strings.Join(parts, "\n")
}

var summaryKeys = []string{"command", "file_path", "path", "pattern", "url", "query", "description"}

// summary is the one-line input summary: path and line counts for edits,
// else the tool's key field, else compact JSON.
func (r renderer) summary(c *toolCall) string {
	m := c.inputMap()
	if isEditTool(c.name) && m != nil {
		a, d := editCounts(m)
		p := r.line(pathOf(m), r.caps.InputBytes)
		if p == "" {
			p = "?"
		}
		return "path=" + p + " +" + strconv.Itoa(a) + "/-" + strconv.Itoa(d) + " lines"
	}
	if m != nil {
		for _, k := range summaryKeys {
			if s, ok := m[k].(string); ok && s != "" {
				return r.line(s, r.caps.InputBytes)
			}
		}
	}
	if len(c.input) > 0 && string(c.input) != "null" {
		return r.line(string(c.input), r.caps.InputBytes)
	}
	return r.line(c.title, r.caps.InputBytes)
}

// toolBody is the record body: the summary plus, for failures only, a capped
// result (never for secret paths).
func (r renderer) toolBody(c *toolCall, summary string) string {
	body := summary
	switch {
	case c.touchesSecretPath():
		body += " " + withheldNote
	case c.status == taskrunner.ToolStatusFailure:
		cap := r.caps.FailureResultBytes
		if isReadTool(c.name) && cap > readFailureBytes {
			cap = readFailureBytes
		}
		if res := r.line(resultText(c.result), cap); res != "" {
			body += " error: " + res
		}
	}
	return body
}
