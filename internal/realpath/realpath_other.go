//go:build !windows

package realpath

import "path/filepath"

func resolve(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
