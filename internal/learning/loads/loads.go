// Package loads recognises, from agent tool-call events, that a learned skill
// was actually loaded (ADR-0026 §6: the "loaded" counter, with its via).
// It is pure matching logic; counting and per-run dedupe belong to the caller.
package loads

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Load is one recognised skill load.
type Load struct {
	Skill string
	Via   string // "skill" (Skill tool) or "read" (file read in a projection)
}

// Via values.
const (
	ViaSkill = "skill"
	ViaRead  = "read"
)

var (
	skillFields = []string{"skill", "name", "command", "skill_name"}
	pathFields  = []string{"file_path", "path", "filePath", "file"}
	readTools   = map[string]bool{"read": true, "read_file": true, "view": true, "readfile": true}
)

// Match reports which learned skills an event loads. Only EventTypeToolCall
// events count. A tool call yields several events (running, then updates) and
// a partial update may carry the name/input only on a later one, so the same
// load can be reported by more than one event of a call: the caller dedupes
// per (run, skill).
//
// via=skill: tool "Skill" (case-insensitive) with a learned name in one of
// skillFields. The real Skill tool input field name is unconfirmed (ADR-0026
// open question 1; the live check is pending), so any of skill/name/command/
// skill_name is accepted, and so is the sole string value of a one-key object.
//
// via=read: a Read-like tool (or ACP kind "read") whose path field lies under
// <projectionDir>/.claude/skills/<name>/ for a provided projection dir. The
// check is lexical only: no symlink resolution, and any ".." segment in the
// raw path disqualifies it.
func Match(e taskrunner.Event, learnedNames map[string]bool, projectionDirs ...string) []Load {
	if e.Type != taskrunner.EventTypeToolCall || len(learnedNames) == 0 || len(e.ToolInput) == 0 {
		return nil
	}
	tool := strings.ToLower(strings.TrimSpace(e.ToolName))
	var obj map[string]json.RawMessage
	if json.Unmarshal(e.ToolInput, &obj) != nil {
		return nil
	}
	switch {
	case tool == "skill":
		if name := skillName(obj); learnedNames[name] {
			return []Load{{Skill: name, Via: ViaSkill}}
		}
	case readTools[tool]:
		return readLoads(obj, learnedNames, projectionDirs)
	}
	return nil
}

func stringField(obj map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := obj[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// skillName extracts the skill name from a Skill tool input, "" if none.
func skillName(obj map[string]json.RawMessage) string {
	for _, f := range skillFields {
		if s, ok := stringField(obj, f); ok {
			if n := normaliseSkill(s); n != "" {
				return n
			}
		}
	}
	if len(obj) == 1 {
		for k := range obj {
			if s, ok := stringField(obj, k); ok {
				return normaliseSkill(s)
			}
		}
	}
	return ""
}

// normaliseSkill trims, drops a leading "/" and any "plugin:" style prefix,
// and keeps only the first word (a slash-command may carry arguments).
func normaliseSkill(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	n := strings.TrimPrefix(f[0], "/")
	if i := strings.LastIndex(n, ":"); i >= 0 {
		n = n[i+1:]
	}
	return n
}

func readLoads(obj map[string]json.RawMessage, learned map[string]bool, dirs []string) []Load {
	var out []Load
	seen := map[string]bool{}
	for _, f := range pathFields {
		p, ok := stringField(obj, f)
		if !ok || p == "" || hasDotDot(p) {
			continue
		}
		p = filepath.Clean(p)
		if !filepath.IsAbs(p) {
			continue
		}
		for _, d := range dirs {
			if d == "" {
				continue
			}
			prefix := filepath.Join(filepath.Clean(d), ".claude", "skills") + string(filepath.Separator)
			rest, ok := strings.CutPrefix(p, prefix)
			if !ok || rest == "" {
				continue
			}
			name, inner, found := strings.Cut(rest, string(filepath.Separator))
			if !found || inner == "" || !learned[name] || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Load{Skill: name, Via: ViaRead})
		}
	}
	return out
}

func hasDotDot(p string) bool {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}
