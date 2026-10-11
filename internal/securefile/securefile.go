// Package securefile writes files that hold secrets (the daemon's auth
// token, relay keys, MCP configs carrying credentials) so only the current
// user can read them: mode 0600 on Unix, and on Windows -- which has no
// POSIX mode bits -- an explicit protected DACL granting only the current
// user and SYSTEM (ADR-0028 D3). Inherited ACLs aren't relied on: a
// broadened parent directory (a redirected profile, a moved SMIND_HOME)
// would otherwise expose the file silently.
package securefile

import "os"

// WriteFile writes data to path and restricts it to the current user. Like
// os.WriteFile it creates or truncates path; unlike it, an existing file's
// looser permissions are tightened too.
func WriteFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return Restrict(path)
}

// Restrict makes an existing file readable and writable by the current
// user only (see the package doc for what that means per OS).
func Restrict(path string) error {
	return restrict(path)
}

// IsPrivate reports whether path is restricted the way Restrict leaves it:
// no group/other permission bits on Unix; on Windows a protected DACL whose
// entries grant access only to the current user and SYSTEM.
func IsPrivate(path string) (bool, error) {
	return isPrivate(path)
}
