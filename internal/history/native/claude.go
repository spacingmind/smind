package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Claude Code transcript (verified 2026-11-05 on a real machine, CLI 2.1.x):
// <config dir>/projects/<cwd dir>/<sessionId>.jsonl, one JSON record per line.
// The records this reader uses:
//
//	{"type":"user","isSidechain":false,"timestamp":"...Z","version":"2.1.x",
//	 "message":{"role":"user","content":"<prompt>"}}                   a prompt
//	{"type":"user","message":{"content":[{"type":"tool_result",
//	 "tool_use_id":"..","content":<string|blocks>,"is_error":false}]}} a result
//	{"type":"assistant","message":{"content":[{"type":"text","text":".."} |
//	 {"type":"thinking","thinking":".."} | {"type":"tool_use","id":"..",
//	 "name":"..","input":{..}} | {"type":"server_tool_use",..}]}}
//
// Every other record type (queue-operation, attachment, api-request, ai-title,
// mode, cost-state, ...) is skipped: the file is an internal format, so the
// reader is tolerant of what it does not know and strict about what it keeps
// (see selfCheck).
type claudeRec struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	// IsCompactSummary marks the synthetic user record an auto- or manual
	// compaction writes (string content, the summary of the earlier turns);
	// IsVisibleInTranscriptOnly marks other records the CLI keeps only for its
	// own transcript view. Neither is something the human typed.
	IsCompactSummary          bool   `json:"isCompactSummary"`
	IsVisibleInTranscriptOnly bool   `json:"isVisibleInTranscriptOnly"`
	Timestamp                 string `json:"timestamp"`
	Version                   string `json:"version"`
	Message                   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// synthetic reports a user record the CLI wrote itself (meta, compaction
// summary, transcript-only): never a prompt and never part of a turn's events.
func (r claudeRec) synthetic() bool {
	return r.IsMeta || r.IsCompactSummary || r.IsVisibleInTranscriptOnly
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// sliceStats is what a reader saw in the turn it matched, for the self-check.
type sliceStats struct {
	uses, results, paired int
	version               string
	decoded, undecodable  int
}

// parseClaude reads the transcript r and returns the events of the one turn whose
// prompt hash and time match ref's run. matches is how many turns matched (0 or
// more than 1 is slice_not_found).
func parseClaude(ctx context.Context, r io.Reader, ref Ref, out *int64, limit int64) (events []taskrunner.Event, st sliceStats, matches int, err error) {
	type segment struct {
		matched bool
		events  []taskrunner.Event
		uses    map[string]bool
		results int
		paired  int
		version string
	}
	var cur *segment
	var kept *segment
	finish := func() {
		if cur != nil && cur.matched {
			matches++
			if matches == 1 {
				kept = cur
			}
		}
		cur = nil
	}
	err = scanLines(ctx, r, func(line []byte) error {
		var rec claudeRec
		if json.Unmarshal(line, &rec) != nil {
			st.undecodable++
			return nil
		}
		st.decoded++
		if rec.Type != "user" && rec.Type != "assistant" {
			return nil
		}
		if rec.IsSidechain {
			return nil
		}
		if rec.Type == "user" && rec.synthetic() {
			// A compaction can land in the middle of a turn: read as a prompt it
			// would end the matched turn there and truncate the rebuilt run.
			return nil
		}
		if rec.Type == "user" {
			if text, ok := claudePrompt(rec.Message.Content); ok {
				finish()
				cur = &segment{uses: map[string]bool{}, version: rec.Version}
				cur.matched = promptMatches(text, ref.PromptSha256) && inWindow(rec.Timestamp, ref.TurnWindow)
				return nil
			}
		}
		if cur == nil || !cur.matched {
			return nil
		}
		var blocks []claudeBlock
		if json.Unmarshal(rec.Message.Content, &blocks) != nil {
			return nil
		}
		for _, b := range blocks {
			var e *taskrunner.Event
			switch b.Type {
			case "text":
				if rec.Type == "assistant" {
					e = &taskrunner.Event{Type: taskrunner.EventTypeText, Text: b.Text}
				}
			case "thinking":
				if b.Thinking != "" {
					e = &taskrunner.Event{Type: taskrunner.EventTypeThinking, Text: b.Thinking}
				}
			case "tool_use", "server_tool_use":
				cur.uses[b.ID] = false
				e = &taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: b.ID, ToolName: b.Name, ToolStatus: taskrunner.ToolStatusRunning, ToolInput: normalizeInput(b.Input)}
			case "tool_result", "server_tool_result":
				status := taskrunner.ToolStatusSuccess
				if b.IsError {
					status = taskrunner.ToolStatusFailure
				}
				cur.results++
				if done, known := cur.uses[b.ToolUseID]; known && !done {
					cur.uses[b.ToolUseID] = true
					cur.paired++
				}
				e = &taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: b.ToolUseID, ToolStatus: status, ToolResult: b.Content}
			}
			if e != nil {
				*out += int64(len(e.Text) + len(e.ToolInput) + len(e.ToolResult))
				if *out > limit {
					return errTooLarge
				}
				cur.events = append(cur.events, *e)
			}
		}
		return nil
	})
	finish()
	if err != nil {
		return nil, st, matches, err
	}
	if kept != nil {
		st.uses, st.results, st.paired, st.version = len(kept.uses), kept.results, kept.paired, kept.version
		return kept.events, st, matches, nil
	}
	return nil, st, matches, nil
}

// claudePrompt returns the text of a user record that is a prompt (a plain
// string, or text blocks only), false for a record carrying tool results.
func claudePrompt(content json.RawMessage) (string, bool) {
	c := strings.TrimSpace(string(content))
	if strings.HasPrefix(c, `"`) {
		var s string
		if json.Unmarshal(content, &s) == nil {
			return s, true
		}
		return "", false
	}
	var blocks []claudeBlock
	if json.Unmarshal(content, &blocks) != nil || len(blocks) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type != "text" {
			return "", false
		}
		b.WriteString(blk.Text)
	}
	return b.String(), true
}

// normalizeInput re-encodes a tool input the way the live path does (a decoded
// map marshalled again), so a rebuilt call matches a stored one byte for byte.
func normalizeInput(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

func promptMatches(text, wantSha string) bool {
	if wantSha == "" {
		return false
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:]) == wantSha
}

// inWindow reports whether ts (RFC 3339) lies within the run's window widened
// by TimeTolerance. An unparsable or zero time never matches: smind does not
// guess.
func inWindow(ts string, w TurnWindow) bool {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil || w.StartedAt.IsZero() || w.FinishedAt.IsZero() {
		return false
	}
	return !t.Before(w.StartedAt.Add(-TimeTolerance)) && !t.After(w.FinishedAt.Add(TimeTolerance))
}
