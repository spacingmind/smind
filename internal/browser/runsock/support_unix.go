//go:build linux || darwin

package runsock

// platformSupported: this platform can read a peer's credentials and a process's
// ancestry, so the per-run socket can be built.
const platformSupported = true
