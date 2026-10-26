package contrast

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/probe/signature"
	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// js escapes a path for embedding in a JSON string literal: a Windows path's
// backslashes would otherwise be invalid JSON escapes.
func js(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

func TestContrast_LoadedUsesLoadsMatch(t *testing.T) {
	proj := filepath.Join(string(filepath.Separator), "data", "proj", "1", "abc")
	skillMD := filepath.Join(proj, ".claude", "skills", "learned-x", "SKILL.md")
	cases := []struct {
		name   string
		events []window.Event
		want   bool
	}{
		{
			"skill tool call",
			[]window.Event{ev(1, taskrunner.Event{
				Type:      taskrunner.EventTypeToolCall,
				ToolName:  "Skill",
				ToolInput: json.RawMessage(`{"skill":"learned-x"}`),
			})},
			true,
		},
		{
			"read under projection",
			[]window.Event{ev(1, taskrunner.Event{
				Type:      taskrunner.EventTypeToolCall,
				ToolName:  "Read",
				ToolInput: json.RawMessage(`{"file_path":"` + js(skillMD) + `"}`),
			})},
			true,
		},
		{
			"read elsewhere",
			[]window.Event{ev(1, taskrunner.Event{
				Type:      taskrunner.EventTypeToolCall,
				ToolName:  "Read",
				ToolInput: json.RawMessage(`{"file_path":"` + js(filepath.Join(string(filepath.Separator), "elsewhere", ".claude", "skills", "learned-x", "SKILL.md")) + `"}`),
			})},
			false,
		},
		{
			"other skill name",
			[]window.Event{ev(1, taskrunner.Event{
				Type:      taskrunner.EventTypeToolCall,
				ToolName:  "Skill",
				ToolInput: json.RawMessage(`{"skill":"learned-y"}`),
			})},
			false,
		},
		{
			"dotdot in path",
			[]window.Event{ev(1, taskrunner.Event{
				Type:      taskrunner.EventTypeToolCall,
				ToolName:  "Read",
				ToolInput: json.RawMessage(`{"file_path":"` + js(proj) + `/.claude/skills/learned-x/../../learned-x/SKILL.md"}`),
			})},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Loaded(c.events, "learned-x", proj); got != c.want {
				t.Fatalf("Loaded = %v, want %v", got, c.want)
			}
		})
	}
}

func TestContrast_FailureRecurrenceUsesSignature(t *testing.T) {
	sig := &signature.Signature{Kind: gate.EvFailureRecovery, Tool: "Bash", Detail: "<path> failed exit #"}
	fail := toolEv(1, "f", "Bash", taskrunner.ToolStatusFailure, json.RawMessage(`"/repo/x.go failed exit 2"`))
	ok := toolEv(1, "s", "Bash", taskrunner.ToolStatusSuccess, json.RawMessage(`"/repo/x.go failed exit 2"`))
	if r := FailureRecurred([]window.Event{fail}, sig); r == nil || !*r {
		t.Fatalf("failed call matching signature: got %v, want true", r)
	}
	if r := FailureRecurred([]window.Event{ok}, sig); r == nil || *r {
		t.Fatalf("succeeded call: got %v, want false", r)
	}
	if r := FailureRecurred([]window.Event{fail}, nil); r != nil {
		t.Fatalf("nil signature: got %v, want nil", r)
	}
}
