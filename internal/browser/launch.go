package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LaunchOpts describes one headless browser process.
type LaunchOpts struct {
	// ProfileRoot is the directory a fresh user-data-dir is made under; ""
	// uses the system temp dir. A profile is never shared between sessions.
	ProfileRoot string
	// Persistent keeps the profile dir after Close (a later, human-chosen mode;
	// the default is ephemeral and the dir is removed).
	Persistent bool
	// ProfileDir, when set, is used as the user-data-dir instead of a fresh
	// one (only for Persistent).
	ProfileDir string
	// AllowHosts are the exact hosts the person added; everything else that
	// needs name resolution is mapped to nothing by the browser itself.
	AllowHosts []string
	// HeapMB caps the JS heap of every renderer (0 uses a safe default).
	HeapMB int
}

// Instance is a running browser reachable over the DevTools protocol.
type Instance interface {
	// WSURL is the browser-level websocket endpoint.
	WSURL() string
	// Close kills the whole browser process tree and removes an ephemeral
	// profile. It is idempotent.
	Close() error
}

// Launcher starts a browser. The real one drives a system Chromium; tests use
// fakes, so no unit test needs a browser.
type Launcher interface {
	Launch(ctx context.Context, o LaunchOpts) (Instance, error)
}

const defaultHeapMB = 512

// buildArgs is the pinned command line. There is deliberately no
// --no-sandbox, no remote-allow-origins and no proxy.
func buildArgs(profileDir string, o LaunchOpts) []string {
	heap := o.HeapMB
	if heap <= 0 {
		heap = defaultHeapMB
	}
	rules := []string{"MAP * ~NOTFOUND", "EXCLUDE localhost", "EXCLUDE *.localhost", "EXCLUDE 127.0.0.1", "EXCLUDE [::1]"}
	for _, h := range o.AllowHosts {
		rules = append(rules, "EXCLUDE "+h)
	}
	return []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--user-data-dir=" + profileDir,
		"--host-resolver-rules=" + strings.Join(rules, ", "),
		"--js-flags=--max-old-space-size=" + strconv.Itoa(heap),
		"--no-proxy-server",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-sync",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-breakpad",
		"--mute-audio",
		"about:blank",
	}
}

// ChromeLauncher launches a system Chrome, Chromium or Edge.
type ChromeLauncher struct {
	Path string // from DetectChrome
}

type chromeInstance struct {
	wsURL      string
	cmd        *exec.Cmd
	profileDir string
	remove     bool
	once       sync.Once
	exited     chan struct{}
}

func (i *chromeInstance) WSURL() string { return i.wsURL }

func (i *chromeInstance) Close() error {
	i.once.Do(func() {
		killTree(i.cmd)
		select {
		case <-i.exited:
		case <-time.After(5 * time.Second):
		}
		if i.remove {
			for n := 0; n < 20; n++ { // files can stay busy for a moment after the kill
				if err := os.RemoveAll(i.profileDir); err == nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	})
	return nil
}

// Launch starts the browser and waits for its DevToolsActivePort file.
func (l ChromeLauncher) Launch(ctx context.Context, o LaunchOpts) (Instance, error) {
	if l.Path == "" {
		return nil, &UnavailableError{InstallStep: installStep("")}
	}
	dir := o.ProfileDir
	remove := false
	if dir == "" || !o.Persistent {
		d, err := os.MkdirTemp(o.ProfileRoot, "smind-browser-")
		if err != nil {
			return nil, fmt.Errorf("browser: profile dir: %w", err)
		}
		dir, remove = d, !o.Persistent
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("browser: profile dir: %w", err)
	}
	_ = os.Remove(filepath.Join(dir, "DevToolsActivePort"))
	cmd := exec.Command(l.Path, buildArgs(dir, o)...)
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		if remove {
			_ = os.RemoveAll(dir)
		}
		return nil, fmt.Errorf("browser: start %s: %w", filepath.Base(l.Path), err)
	}
	inst := &chromeInstance{cmd: cmd, profileDir: dir, remove: remove, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(inst.exited) }()

	ws, err := waitDevToolsPort(ctx, dir, inst.exited)
	if err != nil {
		_ = inst.Close()
		return nil, err
	}
	inst.wsURL = ws
	return inst, nil
}

func waitDevToolsPort(ctx context.Context, dir string, exited <-chan struct{}) (string, error) {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort")); err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) >= 2 {
				port, perr := strconv.Atoi(strings.TrimSpace(lines[0]))
				path := strings.TrimSpace(lines[1])
				if perr == nil && port > 0 && strings.HasPrefix(path, "/devtools/") {
					return fmt.Sprintf("ws://127.0.0.1:%d%s", port, path), nil
				}
			}
		}
		select {
		case <-exited:
			return "", errors.New("browser: the browser exited before it was ready")
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("browser: the browser did not become ready in 20 seconds")
		case <-tick.C:
		}
	}
}
