package host

import (
	"path/filepath"

	"github.com/spacingmind/smind/internal/pluginagent"
)

// The environment a plugin process gets is BUILT, never inherited -- the
// building lives in internal/pluginagent (shared with the plugin-agent
// launch); the wrappers below keep the host's callers and tests working.

// safePath is the PATH a plugin gets: system directories only (see
// pluginagent.SafePath).
func safePath(environ []string) string { return pluginagent.SafePath(environ) }

// scrubbedEnv builds the child environment from the daemon's environ.
// dataDir is the plugin's data directory (HOME and the temp directory live
// under it).
func scrubbedEnv(environ []string, dataDir string) []string {
	return pluginagent.BaseEnv(environ, dataDir, filepath.Join(dataDir, "tmp"))
}

// lookPath finds a bare program name in the plugin's PATH (not the daemon's).
func lookPath(name, pathList string) (string, error) {
	return pluginagent.LookPath(name, pathList)
}
