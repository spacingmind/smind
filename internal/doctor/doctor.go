// Package doctor checks, read-only, how long the provider CLIs keep the
// session files smind rebuilds a finished run's history from.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Level string

const (
	LevelOK   Level = "ok"
	LevelInfo Level = "info"
	LevelWarn Level = "warn"
)

// claudeDefaultDays is Claude Code's own default for cleanupPeriodDays.
const claudeDefaultDays = 30

type Finding struct {
	ID       string `json:"id"`
	Level    Level  `json:"level"`
	Provider string `json:"provider"`
	Root     string `json:"root"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// Env is everything doctor reads from the machine, injected for tests.
type Env struct {
	SmindHome     string
	Home          string
	Getenv        func(string) string
	ReadFile      func(string) ([]byte, error)
	ReadDir       func(string) ([]os.DirEntry, error)
	RetentionDays int
}

// ProviderRetention inspects every Claude Code and Codex config root that
// exists. It never writes.
func ProviderRetention(env Env) []Finding {
	var out []Finding
	for _, root := range roots(env, "claude", "CLAUDE_CONFIG_DIR", ".claude") {
		out = append(out, claudeFindings(env, root)...)
	}
	for _, root := range roots(env, "codex", "CODEX_HOME", ".codex") {
		out = append(out, Finding{
			ID: "codex.retention", Level: LevelInfo, Provider: "codex", Root: root,
			Message: "Codex has no automatic session expiry: sessions stay until you delete them with `codex delete`. Archived sessions (in archived_sessions/) are still readable by smind.",
		})
	}
	return out
}

// roots lists the existing, de-duplicated config roots of one provider: the
// user's own, then each provider instance's private one.
func roots(env Env, sub, envVar, homeDir string) []string {
	var cands []string
	if d := env.Getenv(envVar); d != "" {
		cands = append(cands, d)
	} else if env.Home != "" {
		cands = append(cands, filepath.Join(env.Home, homeDir))
	}
	if env.SmindHome != "" {
		pdir := filepath.Join(env.SmindHome, "providers")
		if ents, err := env.ReadDir(pdir); err == nil {
			for _, e := range ents {
				cands = append(cands, filepath.Join(pdir, e.Name(), sub))
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		c = filepath.Clean(c)
		if seen[c] {
			continue
		}
		seen[c] = true
		if _, err := env.ReadDir(c); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func claudeFindings(env Env, root string) []Finding {
	var out []Finding
	days, from := 0, ""
	unsetNote := ""
	for _, name := range []string{"settings.json", "settings.local.json"} {
		path := filepath.Join(root, name)
		b, err := env.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				out = append(out, settingsInfo(root, path, err))
			}
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			out = append(out, settingsInfo(root, path, err))
			continue
		}
		raw, ok := m["cleanupPeriodDays"]
		if !ok {
			continue
		}
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil || n < 1 || n != float64(int(n)) {
			unsetNote = fmt.Sprintf(" (cleanupPeriodDays in %s is not a whole number of 1 or more, so Claude Code's default applies)", path)
			days, from = 0, ""
			continue
		}
		days, from, unsetNote = int(n), path, ""
	}
	source := "the default"
	if from == "" {
		days = claudeDefaultDays
	} else {
		source = "set in " + from
	}
	r := env.RetentionDays
	f := Finding{ID: "claude.cleanupPeriodDays", Provider: "claude-code", Root: root}
	switch {
	case r == 0:
		f.Level = LevelInfo
		f.Message = fmt.Sprintf("smind keeps its own summaries and large tool output for good; Claude Code deletes transcripts after %d days of inactivity (%s), so older runs can no longer be rebuilt from it.%s", days, source, unsetNote)
	case days <= r:
		f.Level = LevelWarn
		f.Message = fmt.Sprintf("Claude Code deletes session transcripts after %d days (%s). smind removes its own copies of large tool output after %d days, so after that a run's full history can only come from the transcript, and it is already gone by then.%s", days, source, r, unsetNote)
		f.Fix = fmt.Sprintf(`set "cleanupPeriodDays" to a larger number (for example 90) in %s; smind never changes Claude Code's settings`, filepath.Join(root, "settings.json"))
	default:
		f.Level = LevelOK
		f.Message = fmt.Sprintf("Claude Code keeps transcripts for %d days, longer than smind keeps its own large tool output (%d days).%s", days, r, unsetNote)
	}
	out = append(out, f)
	return out
}

func settingsInfo(root, path string, err error) Finding {
	reason := strings.TrimSpace(err.Error())
	return Finding{
		ID: "claude.settings", Level: LevelInfo, Provider: "claude-code", Root: root,
		Message: fmt.Sprintf("could not read %s: %s", path, reason),
	}
}
