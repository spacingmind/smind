package permpolicy

import (
	"runtime"
	"testing"
)

// skipPOSIXPathsOnWindows skips a test whose paths assume POSIX semantics
// (/etc/x absolute, / separators). Windows path support is the follow-up PR
// ("permpolicy: real Windows path support"); until then the engine is off there.
func skipPOSIXPathsOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics; Windows path support lands in the permpolicy Windows follow-up PR (PR B)")
	}
}
