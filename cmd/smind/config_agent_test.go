package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// agentHome isolates SMIND_HOME, seeds a config.yaml and marks the process as
// an agent run.
func agentHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	t.Setenv(taskrunner.AgentRunEnv, "1")
	t.Setenv(taskrunner.RunIDEnv, "")
	cfg := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfg, []byte("# mine\nserver:\n  port: 4648\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func Test_config_cli_set_refused_in_agent_run(t *testing.T) {
	cfg := agentHome(t)
	before, _ := os.ReadFile(cfg)
	for _, key := range config.KnownKeys() {
		for _, args := range [][]string{
			{"config", "set", key, "1"},
			{"config", "unset", key},
			{"--flag", "config", "set", key, "1"},
			{"--flag", "config", "unset", key},
		} {
			var code int
			stderr := captureStderr(t, func() { code = run(args) })
			if code != 1 || !strings.Contains(stderr, "is for people, not agents") {
				t.Errorf("run(%v) = %d, stderr %q; want exit 1 and the refusal", args, code, stderr)
			}
			if after, _ := os.ReadFile(cfg); string(after) != string(before) {
				t.Fatalf("run(%v) changed config.yaml:\n%s", args, after)
			}
		}
	}
}

func Test_config_cli_get_allowed_in_agent_run(t *testing.T) {
	agentHome(t)
	for _, args := range [][]string{{"config", "get"}, {"config", "get", "server.port"}} {
		var code int
		stderr := captureStderr(t, func() { _ = captureStdout(t, func() { code = run(args) }) })
		if code != 0 {
			t.Errorf("run(%v) = %d, stderr %q; want 0", args, code, stderr)
		}
	}
	var code int
	stderr := captureStderr(t, func() { code = run([]string{"config"}) })
	if code != 2 || strings.Contains(stderr, "for people") || !strings.Contains(stderr, "smind config get") {
		t.Errorf("run(config) = %d, stderr %q; want exit 2 and the usage", code, stderr)
	}
	var out string
	out = captureStdout(t, func() { code = run([]string{"config", "--help"}) })
	if code != 0 || !strings.Contains(out, "smind config get") {
		t.Errorf("run(config --help) = %d, stdout %q; want 0 and the usage", code, out)
	}
	for _, h := range []string{"-h", "help"} {
		if out = captureStdout(t, func() { code = run([]string{"config", h}) }); code != 0 || !strings.Contains(out, "settable paths") {
			t.Errorf("config %s = %d, %q", h, code, out)
		}
	}
}

func Test_config_cli_refusal_names_the_scratch_daemon_alternative(t *testing.T) {
	agentHome(t)
	stderr := captureStderr(t, func() { _ = run([]string{"config", "set", "server.port", "1"}) })
	for _, want := range []string{"SMIND_HOME", config.EnvListen, "smind serve"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("refusal %q lacks %q", stderr, want)
		}
	}
}
