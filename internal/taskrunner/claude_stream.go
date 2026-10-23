package taskrunner

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// claudeStreamFlushInterval is how long streamed text deltas are held and
// merged before they become one event. The CLI emits a delta per few tokens;
// one event each would write a run_events row, a wire message and a UI
// re-render per delta. 60ms keeps the text visibly live (about 16 updates a
// second at most) at a small fraction of that cost.
const claudeStreamFlushInterval = 60 * time.Millisecond

// claudeStreamFlushTimeout bounds the final flush after the turn's context has
// been cancelled (a Stop): the consumer keeps draining until the events
// channel is closed, so the send normally succeeds at once, but a stuck
// consumer must not hang the run.
const claudeStreamFlushTimeout = 2 * time.Second

// claudeStreamer turns a claude-native turn's message stream into Events with
// the assistant's text and thinking delivered as it is generated, not when the
// message completes.
//
// With --include-partial-messages the CLI sends raw API stream events
// (content_block_delta ...) and then the complete AssistantMessage for each
// finished content block, repeating the text already streamed. The streamer
// emits the deltas (coalesced) and, for the complete message, only whatever
// of its text/thinking was NOT already streamed -- so a delta stream that lost
// its tail, or a CLI that sent no deltas at all, still ends with the full
// text, and a normal one never shows it twice.
type claudeStreamer struct {
	// pending is the coalescing buffer: text/thinking deltas not yet emitted.
	pendingKind EventType
	pending     strings.Builder
	// streamedText/streamedThinking are what deltas have already carried for
	// the message in progress, consumed as its complete blocks arrive.
	streamedText     string
	streamedThinking string
}

// handle returns the events to emit now for msg (possibly none: a delta is
// held in the coalescing buffer until flush).
func (s *claudeStreamer) handle(msg claudecode.Message) []Event {
	if se, ok := msg.(claudecode.StreamEvent); ok {
		return s.handleStreamEvent(se)
	}
	// Anything else is ordered after the text before it.
	out := s.flush()
	switch m := msg.(type) {
	case claudecode.AssistantMessage:
		out = append(out, s.dedupe(m)...)
	default:
		out = append(out, claudeEvents(msg)...)
	}
	return out
}

func (s *claudeStreamer) handleStreamEvent(se claudecode.StreamEvent) []Event {
	if se.ParentToolUseID != "" {
		// A sub-agent's own stream: the main timeline shows the parent's
		// tool call, not the sub-agent's prose (the complete messages are
		// not surfaced either).
		return nil
	}
	var ev struct {
		Type  string `json:"type"`
		Delta struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	}
	if json.Unmarshal(se.Event, &ev) != nil {
		return nil
	}
	switch ev.Type {
	case "message_start":
		// A new message: whatever an earlier one left unmatched is stale.
		out := s.flush()
		s.streamedText, s.streamedThinking = "", ""
		return out
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			return s.add(EventTypeText, ev.Delta.Text)
		case "thinking_delta":
			return s.add(EventTypeThinking, ev.Delta.Thinking)
		}
	}
	return nil
}

// add buffers a delta, first emitting the buffer if it holds the other kind
// (text and thinking must keep their order).
func (s *claudeStreamer) add(kind EventType, text string) []Event {
	if text == "" {
		return nil
	}
	var out []Event
	if s.pending.Len() > 0 && s.pendingKind != kind {
		out = s.flush()
	}
	s.pendingKind = kind
	s.pending.WriteString(text)
	if kind == EventTypeText {
		s.streamedText += text
	} else {
		s.streamedThinking += text
	}
	return out
}

// hasPending reports whether a delta is waiting for flush.
func (s *claudeStreamer) hasPending() bool { return s.pending.Len() > 0 }

// flush emits the coalescing buffer as one event.
func (s *claudeStreamer) flush() []Event {
	if s.pending.Len() == 0 {
		return nil
	}
	e := Event{Type: s.pendingKind, Text: s.pending.String()}
	s.pending.Reset()
	return []Event{e}
}

// dedupe is claudeEvents for an AssistantMessage with the already-streamed
// prefix of its text and thinking blocks removed.
func (s *claudeStreamer) dedupe(m claudecode.AssistantMessage) []Event {
	if s.streamedText == "" && s.streamedThinking == "" {
		return claudeEvents(m)
	}
	blocks := make([]claudecode.ContentBlock, 0, len(m.Content))
	for _, block := range m.Content {
		switch b := block.(type) {
		case claudecode.TextBlock:
			rest, left := unstreamed(b.Text, s.streamedText)
			s.streamedText = left
			if rest == "" {
				continue
			}
			b.Text = rest
			blocks = append(blocks, b)
		case claudecode.ThinkingBlock:
			rest, left := unstreamed(b.Thinking, s.streamedThinking)
			s.streamedThinking = left
			if rest == "" {
				continue
			}
			b.Thinking = rest
			blocks = append(blocks, b)
		default:
			blocks = append(blocks, block)
		}
	}
	m.Content = blocks
	return claudeEvents(m)
}

// unstreamed splits a complete block's text against the streamed text: the
// part not yet streamed, and what is left of the streamed text for the next
// block. The common case is streamed == full (nothing left to emit).
func unstreamed(full, streamed string) (rest, left string) {
	switch {
	case streamed == "":
		return full, ""
	case strings.HasPrefix(full, streamed):
		return full[len(streamed):], ""
	case strings.HasPrefix(streamed, full):
		return "", streamed[len(full):]
	default:
		// The stream and the message disagree (a CLI that rewrote its text):
		// trust the complete message.
		return full, ""
	}
}

// forwardClaudeMessages reads a turn's messages until updates is closed and
// emits their events, coalescing streamed deltas on a flush timer. onMessage
// sees every message first (the session-id and MCP bookkeeping the caller
// does). On exit -- including after ctx is cancelled by a Stop -- it flushes
// what was streamed so far, so the transcript keeps the partial answer.
func forwardClaudeMessages(ctx context.Context, updates <-chan claudecode.Message, events chan<- Event, onMessage func(claudecode.Message)) {
	var s claudeStreamer
	emit := func(evs []Event) {
		for _, e := range evs {
			select {
			case events <- e:
			case <-ctx.Done():
			}
		}
	}
	var tick <-chan time.Time
	var timer *time.Timer
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer, tick = nil, nil
		}
	}
	defer stopTimer()

	for {
		select {
		case msg, ok := <-updates:
			if !ok {
				stopTimer()
				flushFinal(s.flush(), events)
				return
			}
			if onMessage != nil {
				onMessage(msg)
			}
			evs := s.handle(msg)
			if len(evs) > 0 {
				// A non-delta message (or a kind switch) flushed the buffer.
				stopTimer()
			}
			emit(evs)
			if s.hasPending() && timer == nil {
				timer = time.NewTimer(claudeStreamFlushInterval)
				tick = timer.C
			}
		case <-tick:
			timer, tick = nil, nil
			emit(s.flush())
		}
	}
}

// flushFinal sends the last buffered text even if the turn's context is
// already cancelled (that is exactly when a Stop needs it), bounded so a
// consumer that is gone cannot block the run.
func flushFinal(evs []Event, events chan<- Event) {
	for _, e := range evs {
		select {
		case events <- e:
		case <-time.After(claudeStreamFlushTimeout):
			return
		}
	}
}
