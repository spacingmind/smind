//go:build windows

package shimguard

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// unsafeChars are the characters Go's quoting cannot make safe through
// cmd.exe (see the package doc).
const unsafeChars = `%&|<>^` + "\r\n"

func validate(prefix string, command []string) error {
	if len(command) == 0 {
		return nil
	}
	path, err := exec.LookPath(command[0])
	if err != nil {
		return nil // the spawn will surface the lookup error itself
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
	default:
		return nil
	}
	for _, arg := range command[1:] {
		if strings.ContainsAny(arg, unsafeChars) {
			return fmt.Errorf("%s: refusing to spawn batch shim %s with unsafe argument %q: cmd.exe would reinterpret it; use the real executable or drop the metacharacter", prefix, path, arg)
		}
	}
	return nil
}
