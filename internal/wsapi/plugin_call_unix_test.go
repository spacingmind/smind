//go:build unix

package wsapi

import (
	"syscall"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
)

func TestPluginCall_Kill9MidCall_Typed(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", stdRPCs, "")
	st, _ := e.api.plugins.ProcessStatus("alpha")
	done := make(chan error, 1)
	go func() {
		_, err := e.pcall(localRC(), "alpha", "slow", map[string]any{"ms": 20000})
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(st.PID, syscall.SIGKILL); err != nil {
		t.Skipf("cannot kill: %v", err)
	}
	select {
	case err := <-done:
		wantCallCode(t, err, plugins.CodePluginUnavailable)
	case <-time.After(10 * time.Second):
		t.Fatal("the call did not fail after the process was killed")
	}
}
