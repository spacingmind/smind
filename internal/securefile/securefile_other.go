//go:build !windows

package securefile

import "os"

func restrict(path string) error {
	return os.Chmod(path, 0o600)
}

func isPrivate(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	// Anything beyond rw for the owner (even owner exec) is "not private",
	// matching the 0600 these files have always been tightened back to.
	return info.Mode().Perm()&^0o600 == 0, nil
}
