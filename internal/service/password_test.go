package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-0031 P4: a password-protected daemon (hash in config.yaml, or SMIND_PASSWORD
// in a container) must keep working under every service, and no backend may copy
// a secret into the unit, the plist or the task: the task's command line and XML
// are readable by every local user.
func TestNoServiceBackendCopiesAPasswordIntoTheDefinition(t *testing.T) {
	const secret = "correct-horse-battery-staple"
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			te := newTestEnv(t, goos)
			te.environ["SMIND_PASSWORD"] = secret
			te.environ["SMIND_LISTEN"] = "0.0.0.0:4648"
			te.environ["SMIND_HOSTNAMES"] = "smind.example.com"
			var def string
			switch goos {
			case "linux":
				m, _ := te.systemd(t)
				if err := m.Install(bg, Options{NoStart: true}); err != nil {
					t.Fatal(err)
				}
				def = mustRead(t, te.userUnit())
			case "darwin":
				m, _ := te.launchd(t)
				if err := m.Install(bg, Options{NoStart: true}); err != nil {
					t.Fatal(err)
				}
				def = mustRead(t, te.plist())
			default:
				m, fs, _ := te.windows(t)
				if err := m.Install(bg, Options{NoStart: true}); err != nil {
					t.Fatal(err)
				}
				def = fs.xml + strings.Join(fs.calls, "\n")
			}
			for _, banned := range []string{secret, "SMIND_PASSWORD", "SMIND_LISTEN", "SMIND_HOSTNAMES", "--password"} {
				if strings.Contains(def, banned) {
					t.Errorf("%s definition contains %q:\n%s", goos, banned, def)
				}
			}
		})
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// With server.listen set (a password-protected daemon listening beyond loopback),
// local clients dial 127.0.0.1:<that port> (the daemon keeps a loopback companion),
// so the service's probes and shutdown request must use the listen address's port.
func TestWindowsServiceUsesThePortOfServerListen(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	data := filepath.Join(te.home, "pw-home")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.yaml"), []byte("server:\n  listen: 0.0.0.0:5151\n  passwordHash: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An installed task whose action carries --home <data> (registered directly: the
	// path is a host path, not a Windows one).
	fs.xml = renderTaskXML(taskSpec{Exe: `C:\smind\smind.exe`, User: `WORKGROUP\alice`, Home: data, Conhost: `C:\Windows\System32\conhost.exe`})
	var ports []int
	te.Env.Probe = func(_ context.Context, port int) Health { ports = append(ports, port); return Health{} }
	m, _ = New(te.Env)
	_, _ = m.Status(bg, Options{})
	if len(ports) == 0 || ports[0] != 5151 {
		t.Errorf("probed ports %v, want 5151 (server.listen's port, dialled on the loopback companion)", ports)
	}
}
