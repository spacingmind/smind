//go:build windows

package permpolicy

import "testing"

func TestPlatform_RealWindowsIsUnsupported(t *testing.T) {
	if realHostGOOS != "windows" {
		t.Fatalf("realHostGOOS = %q; want windows", realHostGOOS)
	}
	if supportedOn(realHostGOOS) {
		t.Fatal("supportedOn(windows) = true; want false")
	}
}
