package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The environment a plugin process gets is BUILT, never inherited: the daemon's
// own environment carries SMIND_* values, provider keys and whatever the Human's
// shell exported, and none of it is the plugin's business. Only the names below
// pass through from the daemon (locale and time zone, plus the few Windows
// variables a process cannot start without); PATH is a fixed list of system
// directories; HOME and the temp directory point into the plugin's own data/.
var passthrough = []string{"LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "TZ"}

var passthroughWindows = []string{"SystemRoot", "SYSTEMROOT", "windir", "ComSpec", "PATHEXT"}

// safePath is the PATH a plugin gets: system directories only, so a program
// the Human put early on their own PATH (or a workspace's .bin) is not picked
// up, and no directory under the home is searched.
func safePath(environ []string) string {
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

// scrubbedEnv builds the child environment from the daemon's environ. dataDir
// is the plugin's data directory (HOME and the temp directory live under it).
func scrubbedEnv(environ []string, dataDir string) []string {
	tmp := filepath.Join(dataDir, "tmp")
	out := []string{"PATH=" + safePath(environ), "HOME=" + dataDir}
	if runtime.GOOS == "windows" {
		out = append(out, "USERPROFILE="+dataDir, "TEMP="+tmp, "TMP="+tmp)
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

// lookPath finds a bare program name in the plugin's PATH (not the daemon's).
func lookPath(name, pathList string) (string, error) {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ".com", ".cmd", ".bat"}
	}
	for _, dir := range filepath.SplitList(pathList) {
		for _, e := range exts {
			p := filepath.Join(dir, name+e)
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
				return p, nil
			}
		}
	}
	return "", os.ErrNotExist
}
