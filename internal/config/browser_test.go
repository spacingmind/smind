package config

import (
	"os"
	"path/filepath"
	"testing"
)

func browserHome(t *testing.T, yaml string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SMIND_HOME", dir)
	t.Setenv(EnvBrowserDisabled, "")
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowserKillSwitch_OffByDefault(t *testing.T) {
	browserHome(t, "")
	if BrowserDisabled() {
		t.Fatal("no config file: the browser kill switch is off")
	}
	browserHome(t, "server:\n  port: 4648\n")
	if BrowserDisabled() {
		t.Fatal("a config without browser.disabled leaves it off")
	}
}

func TestBrowserKillSwitch_FromTheFileAndTheEnvironment(t *testing.T) {
	browserHome(t, "browser:\n  disabled: true\n")
	if !BrowserDisabled() {
		t.Fatal("browser.disabled: true must switch it on")
	}
	browserHome(t, "")
	for _, v := range []string{"1", "true", "ON", "yes", "enabled"} {
		t.Setenv(EnvBrowserDisabled, v)
		if !BrowserDisabled() {
			t.Fatalf("%s=%s must switch it on", EnvBrowserDisabled, v)
		}
	}
	for _, v := range []string{"0", "false", "off", "no", "banana"} {
		t.Setenv(EnvBrowserDisabled, v)
		if BrowserDisabled() {
			t.Fatalf("%s=%s must not switch it on", EnvBrowserDisabled, v)
		}
	}
}

// The environment can only turn the switch on: it never overrides a file that already says disabled.
func TestBrowserKillSwitch_EnvironmentNeverTurnsItOff(t *testing.T) {
	browserHome(t, "browser:\n  disabled: true\n")
	t.Setenv(EnvBrowserDisabled, "0")
	if !BrowserDisabled() {
		t.Fatal("an environment value of 0 must not override the file")
	}
}

// A config that cannot be read or parsed fails CLOSED for the browser.
func TestBrowserKillSwitch_FailsClosed(t *testing.T) {
	browserHome(t, "browser: [this is: not valid\n  yaml")
	if !BrowserDisabled() {
		t.Fatal("a malformed config must count as disabled")
	}
	cfg, err := LoadFile(Path())
	if err == nil || !cfg.Browser.Disabled {
		t.Fatalf("LoadFile on a malformed file must report an error and disabled: err=%v disabled=%v", err, cfg.Browser.Disabled)
	}
	// An unreadable file (a directory where the file should be).
	dir := t.TempDir()
	t.Setenv("SMIND_HOME", dir)
	if err := os.Mkdir(filepath.Join(dir, "config.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !BrowserDisabled() {
		t.Fatal("an unreadable config must count as disabled")
	}
}

// It is read on every call: no restart.
func TestBrowserKillSwitch_IsReadOnEveryCall(t *testing.T) {
	browserHome(t, "")
	if BrowserDisabled() {
		t.Fatal("off at first")
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("SMIND_HOME"), "config.yaml"), []byte("browser:\n  disabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !BrowserDisabled() {
		t.Fatal("editing the file must take effect on the next decision")
	}
}
