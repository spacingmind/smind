package loads

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

var learned = map[string]bool{"learned-x": true, "learned-y": true}

// js escapes a path for embedding in a JSON string literal: a Windows path's
// backslashes would otherwise be invalid JSON escapes.
func js(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

func call(tool, input string) taskrunner.Event {
	return taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolName: tool, ToolInput: json.RawMessage(input)}
}

func TestMatch_SkillToolCall(t *testing.T) {
	cases := []struct {
		name, tool, input string
		want              []Load
	}{
		{"skill field", "Skill", `{"skill":"learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"name field", "Skill", `{"name":"learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"command field", "Skill", `{"command":"learned-y"}`, []Load{{"learned-y", "skill"}}},
		{"skill_name", "Skill", `{"skill_name":"learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"plugin prefix", "Skill", `{"skill":"plugin:learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"slash", "Skill", `{"skill":"/learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"slash and plugin and space", "Skill", `{"skill":"  /plugin:learned-x  "}`, []Load{{"learned-x", "skill"}}},
		{"with args", "Skill", `{"skill":"learned-x some args"}`, []Load{{"learned-x", "skill"}}},
		{"case-insensitive tool", "skill", `{"skill":"learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"single unknown key", "Skill", `{"whatever":"learned-x"}`, []Load{{"learned-x", "skill"}}},
		{"two unknown keys", "Skill", `{"a":"learned-x","b":"learned-y"}`, nil},
		{"non-string value", "Skill", `{"skill":42}`, nil},
		{"unknown skill", "Skill", `{"skill":"learned-zzz"}`, nil},
		{"bad json", "Skill", `{"skill":`, nil},
		{"empty input", "Skill", ``, nil},
		{"array input", "Skill", `["learned-x"]`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Match(call(c.tool, c.input), learned)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestMatch_ReadUnderProjectionDir(t *testing.T) {
	// A temp dir, not "/data/...": a rooted path with no drive letter is not
	// absolute on Windows, and Match only looks at absolute paths.
	d1 := filepath.Join(t.TempDir(), "data", "proj", "1", "abc")
	d2 := filepath.Join(t.TempDir(), "data", "proj", "1", "def")
	skillMD := filepath.Join(d2, ".claude", "skills", "learned-y", "SKILL.md")
	cases := []struct {
		tool, input string
		want        []Load
	}{
		{"Read", `{"file_path":"` + js(filepath.Join(d1, ".claude/skills/learned-x/SKILL.md")) + `"}`, []Load{{"learned-x", "read"}}},
		{"read_file", `{"path":"` + js(skillMD) + `"}`, []Load{{"learned-y", "read"}}},
		{"view", `{"filePath":"` + js(filepath.Join(d1, ".claude/skills/learned-x/refs/notes.md")) + `"}`, []Load{{"learned-x", "read"}}},
		{"ReadFile", `{"file":"` + js(skillMD) + `"}`, []Load{{"learned-y", "read"}}},
		{"read", `{"path":"` + js(skillMD) + `"}`, []Load{{"learned-y", "read"}}},
		{"Read", `{"file_path":"` + js(filepath.Join(d1, ".claude", "skills", "learned-x")) + `/./SKILL.md"}`, []Load{{"learned-x", "read"}}},
	}
	for i, c := range cases {
		if got := Match(call(c.tool, c.input), learned, d1, d2); !reflect.DeepEqual(got, c.want) {
			t.Errorf("case %d (%s): got %+v want %+v", i, c.tool, got, c.want)
		}
	}
}

func TestMatch_IgnoresOtherToolsAndHumanSkills(t *testing.T) {
	d := filepath.Join(t.TempDir(), "data", "proj", "1", "abc")
	other := filepath.Join(t.TempDir(), "data", "proj", "2", "abc")
	cases := []taskrunner.Event{
		call("Skill", `{"skill":"human-skill"}`),
		call("Read", `{"file_path":"`+js(filepath.Join(d, ".claude/skills/human/SKILL.md"))+`"}`),
		call("Read", `{"file_path":"`+js(filepath.Join(other, ".claude/skills/learned-x/SKILL.md"))+`"}`),
		call("Read", `{"file_path":"/repo/.claude/skills/learned-x/SKILL.md"}`),
		call("Bash", `{"command":"cat `+js(filepath.Join(d, ".claude/skills/learned-x/SKILL.md"))+`"}`),
		call("Edit", `{"file_path":"`+js(filepath.Join(d, ".claude/skills/learned-x/SKILL.md"))+`"}`),
		call("Bash", `{"skill":"learned-x"}`),
		call("Read", `{"file_path":"relative/.claude/skills/learned-x/SKILL.md"}`),
		call("Read", `{"file_path":"`+js(filepath.Join(d, ".claude/skills"))+`"}`),
		call("Read", `{"file_path":"`+js(filepath.Join(d, ".claude/skills/learned-x"))+`"}`),
		call("Read", `{"file_path":"`+js(filepath.Join(d, ".claude/skills/learned-xx/SKILL.md"))+`"}`),
	}
	for i, e := range cases {
		if got := Match(e, learned, d); len(got) != 0 {
			t.Errorf("case %d matched: %+v", i, got)
		}
	}
	if got := Match(call("Read", `{"file_path":"`+js(filepath.Join(d, ".claude/skills/learned-x/SKILL.md"))+`"}`), learned); got != nil {
		t.Errorf("no projection dirs must not match: %+v", got)
	}
	if got := Match(call("Skill", `{"skill":"learned-x"}`), nil); got != nil {
		t.Errorf("nil learnedNames matched: %+v", got)
	}
}

func TestMatch_IgnoresNonToolEvents(t *testing.T) {
	for _, ty := range []taskrunner.EventType{taskrunner.EventTypeRaw} {
		e := taskrunner.Event{Type: ty, ToolName: "Skill", ToolInput: json.RawMessage(`{"skill":"learned-x"}`)}
		if got := Match(e, learned); got != nil {
			t.Errorf("type %v matched: %+v", ty, got)
		}
	}
	if got := Match(taskrunner.Event{ToolName: "Skill", ToolInput: json.RawMessage(`{"skill":"learned-x"}`)}, learned); got != nil {
		t.Errorf("zero-type event matched: %+v", got)
	}
}

func TestMatch_PathTraversalDoesNotMatch(t *testing.T) {
	d := filepath.Join(t.TempDir(), "data", "proj", "1", "abc")
	for _, p := range []string{
		d + "/.claude/skills/learned-x/../../../../../etc/passwd",
		d + "/.claude/skills/../skills/learned-x/SKILL.md",
		d + "/.claude/skills/learned-x/../learned-y/SKILL.md",
		d + "/x/../.claude/skills/learned-x/SKILL.md",
		"../" + d + "/.claude/skills/learned-x/SKILL.md",
	} {
		in, _ := json.Marshal(map[string]string{"file_path": p})
		if got := Match(call("Read", string(in)), learned, d); len(got) != 0 {
			t.Errorf("%q matched: %+v", p, got)
		}
	}
}
