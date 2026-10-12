//go:build windows

package wsapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProviderTest_ReportsShim_Windows: a shim-only install (npx.cmd on
// PATH, no npx.exe -- exactly how npm installs npx) is reported as ok,
// resolved to the .cmd itself the same way the real spawn would start it
// (exec.LookPath honouring PATHEXT).
func TestProviderTest_ReportsShim_Windows(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "npx.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\nexit /b 0\r\n"), 0o666); err != nil {
		t.Fatalf("write npx.cmd: %v", err)
	}
	t.Setenv("PATH", dir)

	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	result := callProviderTest(t, ws, "1", "glm")
	if !result.OK {
		t.Fatalf("provider.test ok = false, want true (npx.cmd shim on PATH): detail = %q", result.Detail)
	}
	if !strings.HasSuffix(result.Detail, "npx.cmd") {
		t.Fatalf("provider.test detail = %q, want it to name the resolved npx.cmd shim", result.Detail)
	}
}
