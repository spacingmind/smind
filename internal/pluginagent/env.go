// Package pluginagent builds the launch surface of a plugin provider's ACP
// agent child: a scrubbed environment and a confined command (Wave D, PD2).
// The daemon's own environment must never reach that child.
package pluginagent

import (
	"path/filepath"
	"runtime"
	"strings"
)

// The environment an agent child gets is BUILT, never inherited: the daemon's
// own environment carries SMIND_* values, provider keys and whatever the
// Human's shell exported, and none of it is the agent's business. Only the
// names below pass through from the daemon (locale and time zone, plus the
// few Windows variables a process cannot start without); PATH is a fixed
// list of system directories; HOME and the temp directory point into the
// provider's own data/.
var passthrough = []string{"LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "TZ"}

var passthroughWindows = []string{"SystemRoot", "SYSTEMROOT", "windir", "ComSpec", "PATHEXT"}

// SafePath is the PATH an agent child gets: system directories only, so a
// program the Human put early on their own PATH (or a workspace's .bin) is
// not picked up, and no directory under the home is searched.
func SafePath(environ []string) string {
	if runtime.GOOS == "windows" {
		root := `C:\Windows`
		for _, kv := range environ {
			if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "SystemRoot") && v != "" {
				root = v
			}
		}
		return filepath.Join(root, "System32") + ";" + root
	}
	return "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
}

func nameIn(name string, list []string) bool {
	for _, n := range list {
		if name == n || runtime.GOOS == "windows" && strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

// BaseEnv builds the child environment from the daemon's environ: HOME=home
// (USERPROFILE=home on Windows), TMPDIR=tmp (TEMP/TMP=tmp on Windows), and
// only the passthrough names copied from environ. The environment is BUILT,
// never inherited.
func BaseEnv(environ []string, home, tmp string) []string {
	out := []string{"PATH=" + SafePath(environ), "HOME=" + home}
	if runtime.GOOS == "windows" {
		out = append(out, "USERPROFILE="+home, "TEMP="+tmp, "TMP="+tmp)
	} else {
		out = append(out, "TMPDIR="+tmp)
	}
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		if nameIn(name, passthrough) || runtime.GOOS == "windows" && nameIn(name, passthroughWindows) {
			out = append(out, kv)
		}
	}
	return out
}
