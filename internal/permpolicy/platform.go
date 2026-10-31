package permpolicy

import "runtime"

// HostGOOS is the OS the engine runs on. It is a variable only so tests can
// pretend to be Windows; nothing else may assign it.
var HostGOOS = runtime.GOOS

// UnsupportedMessage is what every surface says when the engine is asked to run on Windows.
const UnsupportedMessage = "the permission policy engine is not supported on Windows yet"

// Supported reports whether the engine can run on this OS. The floor and the
// path checks assume POSIX paths; until the Windows follow-up lands they can
// fail open there, so on Windows the engine stays off.
func Supported() bool { return HostGOOS != "windows" }
