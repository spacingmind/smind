package native

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Codex rollout (verified 2026-11-05 on a real machine, CLI 0.166.x):
// <CODEX_HOME>/sessions/YYYY/MM/DD/rollout-<ts>-<threadId>.jsonl (or
// archived_sessions/), one JSON record per line:
//
//	{"type":"session_meta","payload":{"id":"<threadId>","cli_version":".."}}
//	{"type":"event_msg","payload":{"type":"task_started","turn_id":"..","started_at":<unix s>}}
//	{"type":"event_msg","payload":{"type":"item_completed","turn_id":"..",
//	   "item":{"type":"UserMessage"|"AgentMessage","content":[{"type":"text"|"Text","text":".."}]}}}
//	{"type":"event_msg","payload":{"type":"task_complete","turn_id":".."}}
//
// A turn is task_started .. task_complete. smind's live path turns only the
// agent's message text into events for a Codex run (no tool cards), so that is
// all this reader returns: it must not show more than a live read would.
type codexRec struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type      string `json:"type"`
		TurnID    string `json:"turn_id"`
		StartedAt int64  `json:"started_at"`
		Item      struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"item"`
		CLIVersion string `json:"cli_version"`
	} `json:"payload"`
}

func parseCodex(ctx context.Context, r io.Reader, ref Ref, out *int64, limit int64) (events []taskrunner.Event, st sliceStats, matches int, err error) {
	type turn struct {
		id      string
		started time.Time
		prompts []string
		agent   []string
	}
	var cur *turn
	var kept *turn
	closeTurn := func() {
		if cur == nil {
			return
		}
		ok := inWindowTime(cur.started, ref.TurnWindow)
		hit := false
		for _, p := range cur.prompts {
			if promptMatches(p, ref.PromptSha256) {
				hit = true
			}
		}
		if hit && ok {
			matches++
			if matches == 1 {
				kept = cur
			}
		}
		cur = nil
	}
	err = scanLines(ctx, r, func(line []byte) error {
		var rec codexRec
		if json.Unmarshal(line, &rec) != nil {
			st.undecodable++
			return nil
		}
		st.decoded++
		switch {
		case rec.Type == "session_meta":
			st.version = rec.Payload.CLIVersion
		case rec.Type == "event_msg" && rec.Payload.Type == "task_started":
			closeTurn()
			t := time.Time{}
			if rec.Payload.StartedAt > 0 {
				t = time.Unix(rec.Payload.StartedAt, 0).UTC()
			} else if p, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
				t = p
			}
			cur = &turn{id: rec.Payload.TurnID, started: t}
		case rec.Type == "event_msg" && rec.Payload.Type == "task_complete":
			if cur != nil && (rec.Payload.TurnID == "" || rec.Payload.TurnID == cur.id) {
				closeTurn()
			}
		case rec.Type == "event_msg" && rec.Payload.Type == "item_completed" && cur != nil:
			var b strings.Builder
			for _, c := range rec.Payload.Item.Content {
				if strings.EqualFold(c.Type, "text") {
					b.WriteString(c.Text)
				}
			}
			switch rec.Payload.Item.Type {
			case "UserMessage":
				cur.prompts = append(cur.prompts, b.String())
			case "AgentMessage":
				*out += int64(b.Len())
				if *out > limit {
					return errTooLarge
				}
				cur.agent = append(cur.agent, b.String())
			}
		}
		return nil
	})
	closeTurn()
	if err != nil {
		return nil, st, matches, err
	}
	if kept != nil {
		for _, t := range kept.agent {
			events = append(events, taskrunner.Event{Type: taskrunner.EventTypeText, Text: t})
		}
		return events, st, matches, nil
	}
	return nil, st, matches, nil
}

func inWindowTime(t time.Time, w TurnWindow) bool {
	if t.IsZero() || w.StartedAt.IsZero() || w.FinishedAt.IsZero() {
		return false
	}
	return !t.Before(w.StartedAt.Add(-TimeTolerance)) && !t.After(w.FinishedAt.Add(TimeTolerance))
}
