package doctor

import (
	"encoding/json"
	"path/filepath"
)

// autoModeKeys are the Claude Code settings keys that make the CLI silently
// start in Manual instead of the mode smind asked for (probed 2026-11-15:
// `permissions.disableAutoMode` turns a requested `auto` into `default` with
// no error). Each is a string; "disable" is the value that switches it off.
var autoModeKeys = []struct {
	id, key, mode string
}{
	{"claude.disableAutoMode", "disableAutoMode", "Auto mode"},
	{"claude.disableBypassPermissionsMode", "disableBypassPermissionsMode", "Bypass mode"},
}

// ClaudeAutoMode reports, read-only, every Claude Code settings file smind can
// see (each config root's settings.json and settings.local.json, plus
// env.ManagedSettings) that disables Auto or Bypass mode. A run that asks for
// such a mode starts in Manual, so each is a warning naming the key and the
// file. It reads nothing but those files, launches no CLI and writes nothing;
// a file that is missing, unreadable or not JSON yields no finding (the
// retention check already reports unreadable settings).
func ClaudeAutoMode(env Env) []Finding {
	var paths []string
	roots := roots(env, "claude", "CLAUDE_CONFIG_DIR", ".claude")
	for _, root := range roots {
		paths = append(paths, filepath.Join(root, "settings.json"), filepath.Join(root, "settings.local.json"))
	}
	paths = append(paths, env.ManagedSettings...)

	var out []Finding
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		b, err := env.ReadFile(path)
		if err != nil {
			continue
		}
		var m struct {
			Permissions map[string]json.RawMessage `json:"permissions"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		for _, k := range autoModeKeys {
			raw, ok := m.Permissions[k.key]
			if !ok {
				continue
			}
			var v string
			if json.Unmarshal(raw, &v) != nil || v != "disable" {
				continue
			}
			out = append(out, Finding{
				ID: k.id, Level: LevelWarn, Provider: "claude-code", Root: filepath.Dir(path),
				Message: k.mode + ` is disabled by "permissions.` + k.key + `" in ` + path + `: a run that asks for it starts in Manual and asks before every tool, with no error.`,
				Fix:     `remove "permissions.` + k.key + `" from ` + path + `, or choose another mode for the run; smind never changes Claude Code's settings`,
			})
		}
	}
	return out
}
