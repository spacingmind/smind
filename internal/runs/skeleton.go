package runs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Skeleton form (ADR-0053 D1-D3). The finalizer collapses a finished run's
// rows in place:
//
//   - thinking text is not kept (H1): the row says how big it was;
//   - adjacent answer-text rows become one row; a longer one than
//     store.TextRowCap keeps head and tail, as does a user message over
//     store.UserRowCap;
//   - a tool payload (input or result) over store.ExcerptInline moves to
//     run_excerpts (whole up to store.ExcerptWhole, else head and tail) and its
//     row keeps a summary and a descriptor; heredoc bodies of an input are
//     described (index, size, hash) and elided in the summary;
//   - every other row (permissions, usage, done, notes...) is untouched.
//
// store.Store.rehydrate puts the excerpts back on read, so a reader that does
// not ask for the skeleton sees the events it always saw (payloads over
// store.ExcerptWhole as head + marker + tail).

// Summary caps of a tool row (ADR-0053 section 1).
const (
	argsSummaryCap   = 512
	resultSummaryCap = 200
	titleCap         = 512
)

// skelInfo is the "skel" object of a v2 row; the store package reads it back
// (store/run_skeleton.go documents the shape).
type skelInfo struct {
	In    *skelField `json:"in,omitempty"`
	Out   *skelField `json:"out,omitempty"`
	Think *skelField `json:"think,omitempty"`
	Txt   *skelField `json:"txt,omitempty"`
}

type skelField struct {
	N   int64      `json:"n"`
	SHA string     `json:"sha,omitempty"`
	Ex  bool       `json:"ex,omitempty"`
	Doc []skelBody `json:"doc,omitempty"`
}

type skelBody struct {
	I   int    `json:"i"`
	N   int    `json:"n"`
	SHA string `json:"sha"`
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// cutText returns s unchanged when it fits cap, else head + marker + tail with
// half of cap on each side, cut on rune boundaries.
func cutText(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	head, tail := store.CutRunes(s, limit/2)
	return head + store.OmittedMarker(int64(len(s)-len(head)-len(tail))) + tail, true
}

// skeletonRows decides, for every row of a run in seq order, what the
// finalizer writes. It is pure: rows that need no change are not returned, and
// a v2 row (an earlier, interrupted pass) is never touched again, so the
// function is idempotent over a mixed run.
func skeletonRows(rows []store.RunEvent) ([]store.SkeletonRow, error) {
	var out []store.SkeletonRow
	// open answer-text run: the first row's decision, grown by each adjacent text row
	var run *textRun
	flush := func() {
		if run != nil {
			if r, changed := run.result(); changed {
				out = append(out, r)
			}
			out = append(out, run.deleted...)
			run = nil
		}
	}
	for _, row := range rows {
		var p persistedEvent
		if err := json.Unmarshal([]byte(row.EventData), &p); err != nil {
			flush()
			continue // an undecodable row is left exactly as it is
		}
		if p.V == 2 {
			flush()
			continue
		}
		switch {
		case p.Type == taskrunner.EventTypeText && textOnly(p):
			if run == nil {
				run = &textRun{first: row, p: p}
			} else {
				run.add(row, p)
			}
			continue
		case p.Type == taskrunner.EventTypeThinking && textOnly(p):
			flush()
			n := int64(len(p.Text))
			f := &skelField{N: n}
			if n > store.ExcerptInline {
				f.SHA = sha256Hex(p.Text)
			}
			p.Text, p.V, p.Skel = "", 2, &skelInfo{Think: f}
			data, err := json.Marshal(p)
			if err != nil {
				return nil, err
			}
			out = append(out, store.SkeletonRow{ID: row.ID, Seq: row.Seq, EventData: string(data)})
		case p.Type == taskrunner.EventTypeUserMessage:
			flush()
			if text, cut := cutText(p.Text, store.UserRowCap); cut {
				f := &skelField{N: int64(len(p.Text)), SHA: sha256Hex(p.Text)}
				p.Text, p.V, p.Skel = text, 2, &skelInfo{Txt: f}
				data, err := json.Marshal(p)
				if err != nil {
					return nil, err
				}
				out = append(out, store.SkeletonRow{ID: row.ID, Seq: row.Seq, EventData: string(data)})
			}
		case p.Type == taskrunner.EventTypeToolCall:
			flush()
			if r, changed, err := toolRow(row, p); err != nil {
				return nil, err
			} else if changed {
				out = append(out, r)
			}
		default:
			flush()
		}
	}
	flush()
	return out, nil
}

// textOnly reports whether p carries nothing but its type and text.
func textOnly(p persistedEvent) bool {
	return chunkOnly(Event{Type: p.Type, Text: p.Text, StopReason: p.StopReason, PermissionRequestID: p.PermissionRequestID,
		PermissionSummary: p.PermissionSummary, PermissionOptions: p.PermissionOptions, PermissionOptionID: p.PermissionOptionID,
		PermissionResolution: p.PermissionResolution, PermissionRuleID: p.PermissionRuleID, PermissionDetail: p.PermissionDetail,
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, ToolTitle: p.ToolTitle, ToolStatus: p.ToolStatus, ToolInput: p.ToolInput,
		ToolResult: p.ToolResult, RawKind: p.RawKind, RawPayload: p.RawPayload, Usage: p.Usage, McpServers: p.McpServers})
}

// textRun merges adjacent answer-text rows into the first.
type textRun struct {
	first   store.RunEvent
	p       persistedEvent
	text    string
	merged  bool
	deleted []store.SkeletonRow
}

func (t *textRun) add(row store.RunEvent, p persistedEvent) {
	if !t.merged {
		t.text, t.merged = t.p.Text, true
	}
	t.text += p.Text
	t.deleted = append(t.deleted, store.SkeletonRow{ID: row.ID, Seq: row.Seq, Delete: true})
}

func (t *textRun) result() (store.SkeletonRow, bool) {
	text, whole := t.p.Text, t.p.Text
	if t.merged {
		text, whole = t.text, t.text
	}
	p := t.p
	capped, cut := cutText(text, store.TextRowCap)
	if !t.merged && !cut {
		return store.SkeletonRow{}, false
	}
	p.Text = capped
	if cut {
		p.V, p.Skel = 2, &skelInfo{Txt: &skelField{N: int64(len(whole)), SHA: sha256Hex(whole)}}
	}
	data, _ := json.Marshal(p)
	return store.SkeletonRow{ID: t.first.ID, Seq: t.first.Seq, EventData: string(data)}, true
}

// toolRow collapses the payloads of one tool_call row.
func toolRow(row store.RunEvent, p persistedEvent) (store.SkeletonRow, bool, error) {
	sk := &skelInfo{}
	var excerpts []store.Excerpt
	changed := false

	if n := len(p.ToolInput); n > store.ExcerptInline {
		raw := string(p.ToolInput)
		f, x := excerptOf("in", raw)
		f.Doc = heredocDescriptors(p.ToolInput)
		sk.In = f
		excerpts = append(excerpts, x)
		elided, _, _ := taskrunner.ElideToolCall(p.ToolInput, "")
		p.ToolInput = capJSON(elided, argsSummaryCap)
		changed = true
	}
	if n := len(p.ToolResult); n > store.ExcerptInline {
		f, x := excerptOf("out", string(p.ToolResult))
		sk.Out = f
		excerpts = append(excerpts, x)
		p.ToolResult = capJSON(p.ToolResult, resultSummaryCap)
		changed = true
	}
	if !changed {
		return store.SkeletonRow{}, false, nil
	}
	if title, cut := cutText(p.ToolTitle, titleCap); cut {
		p.ToolTitle = title
	}
	p.V, p.Skel = 2, sk
	data, err := json.Marshal(p)
	if err != nil {
		return store.SkeletonRow{}, false, fmt.Errorf("skeleton row %d: %w", row.Seq, err)
	}
	return store.SkeletonRow{ID: row.ID, Seq: row.Seq, EventData: string(data), Excerpts: excerpts}, true, nil
}

// excerptOf is the descriptor and the excerpt-tier row of one payload over
// the inline cap: whole up to ExcerptWhole, else head and tail.
func excerptOf(field, raw string) (*skelField, store.Excerpt) {
	x := store.Excerpt{Field: field, Head: raw, Bytes: int64(len(raw)), SHA256: sha256Hex(raw)}
	if len(raw) > store.ExcerptWhole {
		// A JSON string is cut as text, so the head and tail a reader sees have
		// real line breaks, not "\n" escapes.
		var text string
		if len(raw) > 0 && raw[0] == '"' && json.Unmarshal([]byte(raw), &text) == nil {
			// A string whose text fits head + tail is kept whole: cutting it
			// would omit nothing (its quotes and escapes pushed it over 8 KiB).
			if len(text) > 2*store.ExcerptHalf {
				x.Enc, x.Bytes = "text", int64(len(text))
				x.Head, x.Tail = store.CutRunes(text, store.ExcerptHalf)
			}
		} else {
			x.Head, x.Tail = store.CutRunes(raw, store.ExcerptHalf)
		}
	}
	return &skelField{N: int64(len(raw)), SHA: x.SHA256, Ex: true}, x
}

// capJSON keeps raw when it fits limit, else a JSON string of its head. The
// result is always valid JSON.
func capJSON(raw json.RawMessage, limit int) json.RawMessage {
	if len(raw) <= limit {
		return raw
	}
	head, _ := store.CutRunes(string(raw), limit-8)
	b, _ := json.Marshal(head + "…")
	return b
}

// heredocDescriptors describes the heredoc bodies of a command input so a
// rebuilt body can be checked against them (never the bodies themselves).
func heredocDescriptors(input json.RawMessage) []skelBody {
	for _, key := range []string{"command", "cmd"} {
		cmd, ok := taskrunner.StringField(input, key)
		if !ok {
			continue
		}
		if _, bodies := permpolicy.ElideHeredocs(cmd); len(bodies) > 0 {
			out := make([]skelBody, len(bodies))
			for i, b := range bodies {
				out[i] = skelBody{I: i, N: b.Bytes, SHA: sha256Hex(b.Text)}
			}
			return out
		}
	}
	return nil
}

// skeletonMaxBytes bounds one run's conversion: it reads the run's rows into
// memory and rewrites them in one transaction. The largest run on the real
// database measured 6 MiB; a run over this cap stays in full form (logged).
const skeletonMaxBytes = 64 << 20

// finalizeRun collapses a finished run's rows to skeleton form, logging a
// failure: best effort, the run stays in full form for the next pass.
func (reg *Registry) finalizeRun(id string) {
	if err := Skeletonize(reg.st, id); err != nil {
		log.Printf("runs: skeleton: %v", err)
	}
}

// Skeletonize collapses runID's rows to skeleton form in one transaction. It is
// idempotent and never touches a running, pruned or pre-skeleton run (the store
// re-checks in the transaction); a run over skeletonMaxBytes is left in full
// form. Exported for the packages whose readers must be tested on both forms.
func Skeletonize(st *store.Store, id string) error {
	size, err := st.RunEventBytes(id)
	if err != nil {
		return err
	}
	if size > skeletonMaxBytes {
		return fmt.Errorf("run's rows (%d bytes) are over the %d byte cap; left in full form", size, skeletonMaxBytes)
	}
	rows, err := st.ListRunEventsRaw(id)
	if err != nil {
		return err
	}
	plan, err := skeletonRows(rows)
	if err != nil {
		return err
	}
	_, err = st.ApplySkeleton(id, plan, time.Now())
	return err
}

// FinalizePending converts every eligible finished run not yet in skeleton
// form: runs that were running at an upgrade or were interrupted by a crash
// (finish never ran for them). It returns how many it looked at.
func (reg *Registry) FinalizePending(ctx context.Context) int {
	n := 0
	for ctx.Err() == nil {
		ids, err := reg.st.SkeletonCandidates(50)
		if err != nil || len(ids) == 0 {
			return n
		}
		progressed := false
		for _, id := range ids {
			if ctx.Err() != nil {
				return n
			}
			before, _ := reg.st.GetRunSkeleton(id)
			reg.finalizeRun(id)
			n++
			if after, err := reg.st.GetRunSkeleton(id); err == nil && after.SkeletonAt != nil && before.SkeletonAt == nil {
				progressed = true
			}
		}
		if !progressed {
			return n // the rest could not be converted (over the cap); do not spin
		}
	}
	return n
}
