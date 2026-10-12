package claudecli

import "strings"

// LaunchEnv carries the absolute path of the real claude.exe from the smind
// daemon to the smind.exe process it starts as the CLI's launcher. Its mere
// presence switches smind.exe into launcher mode, so it is stripped from the
// environment of everything the launcher starts: a nested `smind ...` run by
// the agent (its MCP server, a task CLI call) must never see it.
const LaunchEnv = "SMIND_CLAUDE_LAUNCH"

// WithoutEnv returns env without any entry for key (matched
// case-insensitively, like the Windows environment). The input is not
// modified.
func WithoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	prefix := key + "="
	for _, kv := range env {
		if len(kv) >= len(prefix) && strings.EqualFold(kv[:len(prefix)], prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
