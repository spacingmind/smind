package sandbox

import (
	"testing"
	"time"
)

// AttestationForTest returns a valid Attestation minted at now, for tests of
// packages that consume one (the probe projection builder, the probe
// service) and cannot run a canary. It needs a live testing.TB, so
// production code cannot call it by accident: the nil TB a production caller
// would have to pass panics here. Whether an Attestation is minted by a real
// canary is still guaranteed in production by Canary.Verify being the only
// other minting site, and by a repo test that greps for this function's use
// outside _test.go files.
func AttestationForTest(tb testing.TB, now time.Time) Attestation {
	tb.Helper()
	return Attestation{armDir: tb.TempDir(), settingsSHA: "test", at: now, minted: true}
}
