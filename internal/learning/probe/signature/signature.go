// Package signature derives a failure signature from a learned-skill
// proposal's evidence and counts its occurrences in other runs (ADR-0037
// stage 0). It is pure: callers load and decode the run events.
package signature

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// recoveryWindow mirrors the window renderer's failure -> recovery distance.
const recoveryWindow = 10

const maxDetailRunes = 120

// Signature identifies a recurring failure or permission request.
type Signature struct {
	Kind, Tool, Detail string
}

// Key is a stable identity for deduplication.
func (s Signature) Key() string {
	return s.Kind + "\x00" + strings.ToLower(s.Tool) + "\x00" + s.Detail
}

// Label is the human text for the signature.
func (s Signature) Label() string {
	if s.Kind == gate.EvPermissionDenied {
		return "permission request: " + s.Detail
	}
	return s.Tool + " failed: " + s.Detail
}

var (
	pathRe = regexp.MustCompile(`(?:[a-z]:\\|~/|/)\S*`)
	hexRe  = regexp.MustCompile(`0x[0-9a-f]+|[0-9a-f]{8,}`)
	numRe  = regexp.MustCompile(`[0-9]+`)
	wsRe   = regexp.MustCompile(`\s+`)
)

// Normalise reduces s to its first non-empty line with volatile parts
// (paths, hashes, numbers) replaced by placeholders.
func Normalise(s string) string {
	line := ""
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			line = l
			break
		}
	}
	line = strings.ToLower(line)
	line = pathRe.ReplaceAllString(line, "<path>")
	line = hexRe.ReplaceAllString(line, "<h>")
	line = numRe.ReplaceAllString(line, "#")
	line = strings.TrimSpace(wsRe.ReplaceAllString(line, " "))
	if r := []rune(line); len(r) > maxDetailRunes {
		line = string(r[:maxDetailRunes])
	}
	return line
}

// call is one tool call merged from its events by ToolCallID.
type call struct {
	name, title, status string
	input, result       json.RawMessage
	seq                 int64 // first-seen seq
}

func mergeCalls(events []window.Event) []*call {
	var calls []*call
	byID := map[string]*call{}
	for _, ev := range events {
		e := ev.E
		if e.Type != taskrunner.EventTypeToolCall {
			continue
		}
		var c *call
		if e.ToolCallID != "" {
			c = byID[e.ToolCallID]
		}
		if c == nil {
			c = &call{seq: ev.Seq}
			if e.ToolCallID != "" {
				byID[e.ToolCallID] = c
			}
			calls = append(calls, c)
		}
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
	return calls
}

func (c *call) path() string {
	var m map[string]any
	if len(c.input) > 0 {
		_ = json.Unmarshal(c.input, &m)
	}
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func samePath(a, b *call) bool {
	pa, pb := a.path(), b.path()
	return pa == "" || pb == "" || pa == pb
}

func (c *call) secret() bool { return window.TouchesSecretPath(c.input, c.title) }

// Derive computes the signature of ev from its run's events (ordered by
// seq). ok is false when the evidence carries no signature.
func Derive(ev gate.Evidence, events []window.Event) (Signature, bool) {
	switch ev.Kind {
	case gate.EvFailureRecovery:
		return deriveFailure(ev, events)
	case gate.EvPermissionDenied:
		return derivePermission(ev, events)
	}
	return Signature{}, false
}

func deriveFailure(ev gate.Evidence, events []window.Event) (Signature, bool) {
	calls := mergeCalls(events)
	// Replay the renderer's pairing so the failure is the one it consumed.
	paired := map[*call]bool{}
	for j, c := range calls {
		if c.status != taskrunner.ToolStatusSuccess {
			continue
		}
		for i := j - 1; i >= 0 && j-i <= recoveryWindow; i-- {
			f := calls[i]
			if f.status != taskrunner.ToolStatusFailure || paired[f] || f.name != c.name || !samePath(f, c) {
				continue
			}
			paired[f] = true
			if c.seq != ev.Seq {
				break
			}
			if f.secret() {
				return Signature{}, false
			}
			detail := Normalise(window.ResultText(f.result))
			if detail == "" {
				return Signature{}, false
			}
			return Signature{Kind: gate.EvFailureRecovery, Tool: f.name, Detail: detail}, true
		}
	}
	return Signature{}, false
}

func derivePermission(ev gate.Evidence, events []window.Event) (Signature, bool) {
	var resolved *taskrunner.Event
	for i := range events {
		if events[i].Seq == ev.Seq && events[i].E.Type == taskrunner.EventTypePermissionResolved {
			resolved = &events[i].E
			break
		}
	}
	if resolved == nil {
		return Signature{}, false
	}
	for _, e := range events {
		if e.E.Type == taskrunner.EventTypePermissionRequest && e.E.PermissionRequestID == resolved.PermissionRequestID {
			if d := Normalise(e.E.PermissionSummary); d != "" {
				return Signature{Kind: gate.EvPermissionDenied, Detail: d}, true
			}
			return Signature{}, false
		}
	}
	return Signature{}, false
}

// Count returns how many times sig occurs in events.
func Count(events []window.Event, sig Signature) int {
	n := 0
	switch sig.Kind {
	case gate.EvFailureRecovery:
		for _, c := range mergeCalls(events) {
			if c.status == taskrunner.ToolStatusFailure && strings.EqualFold(c.name, sig.Tool) &&
				!c.secret() && Normalise(window.ResultText(c.result)) == sig.Detail {
				n++
			}
		}
	case gate.EvPermissionDenied:
		for _, e := range events {
			if e.E.Type == taskrunner.EventTypePermissionRequest && Normalise(e.E.PermissionSummary) == sig.Detail {
				n++
			}
		}
	}
	return n
}
