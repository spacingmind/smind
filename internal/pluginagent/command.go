package pluginagent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spacingmind/smind/internal/fsutil"
)

// LookPath finds a bare program name in the fixed plugin PATH (not the
// daemon's).
func LookPath(name, pathList string) (string, error) {
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

// ConfinedExecutable resolves rel under root and checks the file: inside the
// plugin after symlinks, a regular file, and executable (the bit is not
// checked on Windows). A symlink anywhere in the path is refused.
func ConfinedExecutable(root, rel string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("plugin directory: %w", err)
	}
	full := filepath.Join(realRoot, filepath.FromSlash(rel))
	cur := realRoot
	for _, seg := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", fmt.Errorf("not found in the plugin")
		}
		if fsutil.IsLink(fi.Mode()) {
			return "", fmt.Errorf("a symlink in the path is refused")
		}
	}
	fi, err := os.Stat(full)
	if err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("is not a regular file")
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("is not executable (chmod +x)")
	}
	return full, nil
}
