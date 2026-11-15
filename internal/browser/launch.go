package browser

import (
	"context"
	"fmt"
	"io"
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
	// Conn is the DevTools pipe pair: there is no TCP port for another local
	// process to attach to.
	Conn() io.ReadWriteCloser
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
		"--remote-debugging-pipe",
		"--user-data-dir=" + profileDir,
		"--host-resolver-rules=" + strings.Join(rules, ", "),
		"--js-flags=--max-old-space-size=" + strconv.Itoa(heap),
		"--no-proxy-server",
		"--use-mock-keychain",
		"--password-store=basic",
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
	conn       io.ReadWriteCloser
	cmd        *exec.Cmd
	kill       func()
	profileDir string
	once       sync.Once
	exited     chan struct{}
}

func (i *chromeInstance) Conn() io.ReadWriteCloser { return i.conn }

// Close kills the whole process tree (always, even if the leader already
// exited: its children may not have) and removes the profile. Idempotent.
func (i *chromeInstance) Close() error {
	i.once.Do(func() {
		_ = i.conn.Close() // the browser also quits on EOF of its pipe
		i.kill()
		select {
		case <-i.exited:
		case <-time.After(5 * time.Second):
		}
		for n := 0; n < 20; n++ { // files can stay busy for a moment after the kill
			if err := os.RemoveAll(i.profileDir); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return nil
}

const ownerFile = ".smind-owner"

var sweepOnce sync.Map // profile root -> *sync.Once

// Launch starts the browser. Readiness is the first protocol call: a browser
// that dies early closes its pipe and that call fails.
func (l ChromeLauncher) Launch(ctx context.Context, o LaunchOpts) (Instance, error) {
	if l.Path == "" {
		return nil, &UnavailableError{InstallStep: installStep("")}
	}
	once, _ := sweepOnce.LoadOrStore(o.ProfileRoot, &sync.Once{})
	once.(*sync.Once).Do(func() { SweepStaleProfiles(o.ProfileRoot) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A fresh, private profile per session; it is removed on Close.
	dir, err := os.MkdirTemp(o.ProfileRoot, profilePrefix)
	if err != nil {
		return nil, fmt.Errorf("browser: profile dir: %w", err)
	}
	_ = os.WriteFile(filepath.Join(dir, ownerFile), []byte(strconv.Itoa(os.Getpid())), 0o600)
	args, err := buildArgs(dir, o)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	cmd := exec.Command(l.Path, args...)
	cmd.Env = browserEnv(os.Environ())
	conn, kill, err := startBrowser(cmd)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("browser: start %s: %w", filepath.Base(l.Path), err)
	}
	inst := &chromeInstance{conn: conn, cmd: cmd, kill: kill, profileDir: dir, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(inst.exited) }()
	return inst, nil
}

const profilePrefix = "smind-browser-"

// SweepStaleProfiles removes profile directories left by a daemon that died
// without closing its sessions: those whose owner process is gone.
func SweepStaleProfiles(root string) {
	if root == "" {
		root = os.TempDir()
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), profilePrefix) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, ownerFile))
		pid, perr := strconv.Atoi(strings.TrimSpace(string(b)))
		if err == nil && perr == nil && (pid == os.Getpid() || processAlive(pid)) {
			continue
		}
		_ = os.RemoveAll(dir)
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
