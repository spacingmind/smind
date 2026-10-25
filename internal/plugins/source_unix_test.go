//go:build unix

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInstall_FifoArchive_DoesNotHang(t *testing.T) {
	e := newEnv(t)
	fifo := filepath.Join(t.TempDir(), "p.tgz")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := e.m.Install(bg, fifo+"#sha256="+strings.Repeat("a", 64))
		done <- err
	}()
	select {
	case err := <-done:
		wantCode(t, err, CodeBadSource)
	case <-time.After(5 * time.Second):
		t.Fatal("install blocked opening a FIFO")
	}
}

func TestInstall_DataSymlinkIsNotAdopted(t *testing.T) {
	e := newEnv(t)
	outside := t.TempDir()
	dst := filepath.Join(e.home, "plugins", "demo")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "data")); err != nil {
		t.Skip(err)
	}
	_, err := e.m.Install(bg, writePluginDir(t, goodManifest, nil))
	wantCode(t, err, CodeLayout)
}
