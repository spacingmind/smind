package window

import (
	"encoding/json"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const secret = "SECRETSECRETSECRET"

func fakeRedact(s string) string { return strings.ReplaceAll(s, secret, "[REDACTED]") }

func defCaps() Caps { return CapsFromConfig(config.DefaultLearning()) }

func jraw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

type evBuilder struct {
	seq, id int64
	out     []Event
}

func (b *evBuilder) add(e taskrunner.Event) *evBuilder {
	b.seq++
	b.id += 10
	b.out = append(b.out, Event{ID: b.id, Seq: b.seq, E: e})
	return b
}

func (b *evBuilder) text(s string) *evBuilder {
	return b.add(taskrunner.Event{Type: taskrunner.EventTypeText, Text: s})
}

func (b *evBuilder) tool(id, name, status string, input, result any) *evBuilder {
	e := taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: name, ToolStatus: status}
	if input != nil {
		e.ToolInput = jraw(input)
	}
	if result != nil {
		e.ToolResult = jraw(result)
	}
	return b.add(e)
}

func (b *evBuilder) events() []Event { return b.out }

func humanRun(id, prompt string, evs []Event) Run {
	return Run{ID: id, Provider: "claude-native", Prompt: prompt, Status: "completed", StopReason: "end_turn", Origin: OriginHuman, Events: evs}
}

func build(runs ...Run) Output {
	return Build(Input{Runs: runs, Redact: fakeRedact, Caps: defCaps()})
}
