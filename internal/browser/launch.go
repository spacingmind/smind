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
	// AllowHosts are the exact hosts the person added (each is validated with
	// NormalizeAllowHost); everything else that needs name resolution is
	// mapped to nothing by the browser itself.
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
func buildArgs(profileDir string, o LaunchOpts) ([]string, error) {
	heap := o.HeapMB
	if heap <= 0 {
		heap = defaultHeapMB
	}
	rules := []string{"MAP * ~NOTFOUND", "EXCLUDE localhost", "EXCLUDE *.localhost", "EXCLUDE 127.0.0.1", "EXCLUDE [::1]"}
	for _, h := range o.AllowHosts {
		n, err := NormalizeAllowHost(h) // never a comma, space or wildcard in a rule
		if err != nil {
			return nil, err
		}
		rules = append(rules, "EXCLUDE "+n)
	}
	return []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--user-data-dir=" + profileDir,
		"--host-resolver-rules=" + strings.Join(rules, ", "),
		"--js-flags=--max-old-space-size=" + strconv.Itoa(heap),
		"--no-proxy-server",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
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
	}, nil
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
		select {
		case <-i.exited: // already gone and reaped: its pid may be reused
		default:
			killTree(i.cmd)
		}
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
	// A fresh, private profile per session; it is removed on Close.
	dir, err := os.MkdirTemp(o.ProfileRoot, "smind-browser-")
	if err != nil {
		return nil, fmt.Errorf("browser: profile dir: %w", err)
	}
	remove := true
	args, err := buildArgs(dir, o)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	cmd := exec.Command(l.Path, args...)
	cmd.Env = browserEnv(os.Environ())
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

// browserEnv passes the browser only what it needs, never the daemon's
// secrets and tokens.
func browserEnv(env []string) []string {
	keep := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "TEMP": true, "TMP": true, "LANG": true, "LC_ALL": true,
		"DISPLAY": true, "XDG_RUNTIME_DIR": true, "SYSTEMROOT": true, "USERPROFILE": true, "LOCALAPPDATA": true, "APPDATA": true}
	var out []string
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && keep[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return out
}
