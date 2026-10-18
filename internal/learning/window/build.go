package window

import (
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func (c Caps) withDefaults() Caps {
	d := CapsFromConfig(config.DefaultLearning())
	if c.RecordBytes <= 0 {
		c.RecordBytes = d.RecordBytes
	}
	if c.InputBytes <= 0 {
		c.InputBytes = d.InputBytes
	}
	if c.ResultBytes <= 0 {
		c.ResultBytes = d.ResultBytes
	}
	if c.FailureResultBytes <= 0 {
		c.FailureResultBytes = d.FailureResultBytes
	}
	if c.WindowBytes <= 0 {
		c.WindowBytes = d.WindowBytes
	}
	return c
}

// Build renders the window. Without a Redact function it fails closed and
// returns an empty Output rather than render unredacted text.
func Build(in Input) Output {
	if in.Redact == nil {
		return Output{}
	}
	r := renderer{redact: in.Redact, caps: in.Caps.withDefaults()}
	out := Output{Window: gate.Window{}}
	var sb strings.Builder
	for _, run := range in.Runs {
		out.ToolCalls += countRunToolCalls(run)
		for _, ev := range run.Events {
			if ev.ID > out.LastEventID {
				out.LastEventID = ev.ID
			}
		}
		var recs []record
		if rc, ok := r.promptRecord(run, false); ok {
			recs = append(recs, rc)
		}
		recs = append(recs, r.runRecords(run)...)
		if len(recs) == 0 {
			continue
		}
		sb.WriteString("## run " + r.line(run.ID, 128) + " (" + r.line(run.Provider, 64) + ", " + r.line(run.Status, 64) + ", stop=" + r.line(run.StopReason, 64) + ")\n")
		for _, rc := range recs {
			sb.WriteString(rc.line() + "\n")
			if rc.gated {
				out.Window[rc.key] = gate.WindowEvent{Kind: rc.kind, Text: rc.body}
			}
		}
	}
	out.Text = sb.String()
	return out
}

func countRunToolCalls(run Run) int {
	evs := make([]taskrunner.Event, 0, len(run.Events))
	for _, e := range run.Events {
		evs = append(evs, e.E)
	}
	return CountToolCalls(evs)
}
