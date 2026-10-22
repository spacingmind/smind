package window

import (
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Gate kinds for events that can never qualify as evidence.
const (
	kindAssistant  = "assistant"
	kindToolOutput = "tool_output"
	kindOther      = "other"
)

// Sentinel seqs for records that are not run events.
const (
	seqPrompt      int64 = -1
	seqLoadedSkill int64 = -2
)

// record is one rendered transcript record: line = prefix + " " + body.
// When gated, body is exactly the gate event's Text.
type record struct {
	prefix, body string
	key          gate.EventKey
	gated        bool
	kind         string
}

func (rc record) line() string { return rc.prefix + " " + rc.body }

// fitBody clips an already-rendered body so the whole record fits RecordBytes.
func (r renderer) fitBody(prefix, body string) string {
	return clipRunes(body, r.caps.RecordBytes-len(prefix)-1)
}

func (r renderer) textRecord(prefix, raw string, key gate.EventKey, kind string) (record, bool) {
	body := r.text(raw, r.caps.RecordBytes-len(prefix)-1)
	if strings.TrimSpace(body) == "" {
		return record{}, false
	}
	return record{prefix: prefix, body: body, key: key, gated: true, kind: kind}, true
}

// isRejection reports whether the chosen option denies the request. The
// request's own option list wins (its Kind says reject_*); otherwise the
// option id itself is inspected.
func isRejection(req *taskrunner.Event, optionID string) bool {
	if req != nil {
		for _, o := range req.PermissionOptions {
			if o.ID == optionID {
				return strings.HasPrefix(o.Kind, "reject")
			}
		}
	}
	id := strings.ToLower(optionID)
	return strings.Contains(id, "deny") || strings.Contains(id, "reject") || strings.Contains(id, "decline")
}

// promptRecord renders the run prompt. Only a human-origin prompt gets the
// user_message gate kind.
func (r renderer) promptRecord(run Run, correction bool) (record, bool) {
	origin := run.Origin
	if origin == "" {
		origin = OriginUnknown
	}
	prefix := "[USER origin=" + string(origin)
	if correction {
		prefix += " correction"
	}
	prefix += " #p]"
	kind := kindOther
	if origin == OriginHuman {
		kind = gate.EvUserMessage
	}
	text := run.Prompt
	if len(run.Attachments) > 0 {
		text += "\n" + strings.Join(run.Attachments, "\n")
	}
	return r.textRecord(prefix, text, gate.EventKey{RunID: run.ID, Seq: seqPrompt}, kind)
}

// runRecords renders one run's records in order (tool calls merged by id,
// assistant chunks merged per turn).
func (r renderer) runRecords(run Run) []record {
	var out []record
	var calls []*toolCall // in first-seen order, parallel to their records
	byID := make(map[string]*toolCall)
	recIdx := make(map[*toolCall]int)
	requests := make(map[string]*taskrunner.Event)

	var textRaw, userRaw strings.Builder
	var textSeq, userSeq int64
	flush := func() {
		if textRaw.Len() > 0 {
			if rc, ok := r.textRecord("[ASSISTANT #"+itoa64(textSeq)+"]", textRaw.String(), gate.EventKey{RunID: run.ID, Seq: textSeq}, kindAssistant); ok {
				out = append(out, rc)
			}
			textRaw.Reset()
		}
		if userRaw.Len() > 0 {
			if rc, ok := r.textRecord("[USER origin=agent #"+itoa64(userSeq)+"]", userRaw.String(), gate.EventKey{RunID: run.ID, Seq: userSeq}, kindOther); ok {
				out = append(out, rc)
			}
			userRaw.Reset()
		}
	}
	limit := r.caps.RecordBytes * 4
	appendCapped := func(b *strings.Builder, s string) {
		if b.Len() < limit {
			b.WriteString(s)
		}
	}

	for _, ev := range run.Events {
		e := ev.E
		switch e.Type {
		case taskrunner.EventTypeText:
			if userRaw.Len() > 0 {
				flush()
			}
			if textRaw.Len() == 0 {
				textSeq = ev.Seq
			}
			appendCapped(&textRaw, e.Text)
		case taskrunner.EventTypeUserMessage:
			if textRaw.Len() > 0 {
				flush()
			}
			if userRaw.Len() == 0 {
				userSeq = ev.Seq
			}
			appendCapped(&userRaw, e.Text)
		case taskrunner.EventTypeToolCall:
			flush()
			var c *toolCall
			if e.ToolCallID != "" {
				c = byID[e.ToolCallID]
			}
			if c == nil {
				c = &toolCall{id: e.ToolCallID, seq: ev.Seq}
				if e.ToolCallID != "" {
					byID[e.ToolCallID] = c
				}
				calls = append(calls, c)
				recIdx[c] = len(out)
				out = append(out, record{key: gate.EventKey{RunID: run.ID, Seq: ev.Seq}, gated: true, kind: kindToolOutput})
			}
			c.merge(ev.Seq, e)
		case taskrunner.EventTypePermissionRequest:
			ec := e
			requests[e.PermissionRequestID] = &ec
		case taskrunner.EventTypePermissionResolved:
			if e.PermissionResolution != taskrunner.PermissionResolvedByHuman || !isRejection(requests[e.PermissionRequestID], e.PermissionOptionID) {
				continue
			}
			flush()
			summary := "permission request"
			if req := requests[e.PermissionRequestID]; req != nil && req.PermissionSummary != "" {
				summary = req.PermissionSummary
			}
			prefix := "[PERMISSION #" + itoa64(ev.Seq) + " denied by human]"
			if rc, ok := r.textRecord(prefix, summary, gate.EventKey{RunID: run.ID, Seq: ev.Seq}, gate.EvPermissionDenied); ok {
				out = append(out, rc)
			}
		default:
			// thinking, raw, usage, mcp servers, notes, mode changes, done:
			// never rendered and they do not split a turn.
		}
	}
	flush()

	// Failure -> recovery pairs: a success of the same tool (and path, when
	// both carry one) within the next recoveryWindow calls consumes the
	// nearest preceding unpaired failure.
	paired := make(map[*toolCall]bool)
	recoveries := make(map[int]record) // keyed by the success call's record index
	summaries := make(map[*toolCall]string, len(calls))
	for _, c := range calls {
		summaries[c] = r.summary(c)
	}
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
			prefix := "[RECOVERY #" + itoa64(c.seq) + "]"
			name := r.line(c.name, 64)
			body := "failed: " + name + " " + summaries[f] + " -> recovered: " + name + " " + summaries[c]
			recoveries[recIdx[c]] = record{
				prefix: prefix, body: r.fitBody(prefix, body),
				key: gate.EventKey{RunID: run.ID, Seq: c.seq}, gated: true, kind: gate.EvFailureRecovery,
			}
			break
		}
	}

	for _, c := range calls {
		i := recIdx[c]
		prefix := "[TOOL #" + itoa64(c.seq) + " " + r.line(c.name, 64) + " " + c.status + "]"
		tag := ""
		if paired[c] {
			tag = recoveredTag
		}
		body := clipRunes(r.toolBody(c, summaries[c]), r.caps.RecordBytes-len(prefix)-1-len(tag)) + tag
		out[i].prefix, out[i].body = prefix, body
	}
	if len(recoveries) == 0 {
		return out
	}
	merged := make([]record, 0, len(out)+len(recoveries))
	for i, rc := range out {
		merged = append(merged, rc)
		if rec, ok := recoveries[i]; ok {
			merged = append(merged, rec)
		}
	}
	return merged
}

const (
	recoveryWindow = 10
	recoveredTag   = " [recovered]"
)

func samePath(a, b *toolCall) bool {
	pa, pb := pathOf(a.inputMap()), pathOf(b.inputMap())
	return pa == "" || pb == "" || pa == pb
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
