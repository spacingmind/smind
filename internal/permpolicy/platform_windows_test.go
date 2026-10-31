//go:build windows

package permpolicy

import "testing"

func TestPlatform_RealWindowsIsUnsupported(t *testing.T) {
	if Supported() {
		t.Fatal("Supported() = true on windows; want false")
	}
	if HostGOOS != "windows" {
		t.Fatalf("HostGOOS = %q; want windows", HostGOOS)
	}
}
