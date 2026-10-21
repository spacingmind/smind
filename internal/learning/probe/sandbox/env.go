package sandbox

import "os"

// DefaultEnvAllowlist is the set of variable NAMES the launcher passes to the
// real CLI. Measured (claude 2.1.303, macOS): with HOME and CLAUDE_CONFIG_DIR
// (the user's normal login) plus USER the CLI authenticates; without USER it
// answers "Not logged in". SHELL, TERM, LOGNAME were not needed. Only names
// the parent process has are passed. Values are never logged.
var DefaultEnvAllowlist = []string{"PATH", "LANG", "TMPDIR", "HOME", "CLAUDE_CONFIG_DIR", "USER"}

// fixedEnv are constant, non-secret entries added beside the passthrough
// allowlist: the connector opt-out keeps claude.ai account connectors out of
// the run, and the git pair lets plain git work in the arm although the real
// $HOME (and its ~/.gitconfig) is unreadable.
var fixedEnv = []string{
	"ENABLE_CLAUDEAI_MCP_SERVERS=false",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_NOSYSTEM=1",
}

// AllowlistEnv returns the NAME=VALUE entries for WriteLauncher: every name in
// DefaultEnvAllowlist that lookup reports as set (nil = os.LookupEnv), plus
// the fixed extras. NEVER add a token or key here: the values land in the
// launcher file on disk.
func AllowlistEnv(lookup func(string) (string, bool)) []string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var env []string
	for _, n := range DefaultEnvAllowlist {
		if v, ok := lookup(n); ok {
			env = append(env, n+"="+v)
		}
	}
	return append(env, fixedEnv...)
}
