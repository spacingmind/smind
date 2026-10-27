package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
)

// examplePython finds python3 where a plugin process will look for it (the
// host's fixed PATH: a plugin never inherits the daemon's). Missing means the
// example cannot run on this machine: the test says so and skips, loudly.
func examplePython(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		for _, d := range []string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"} {
			if st, err := os.Stat(filepath.Join(d, "python3")); err == nil && !st.IsDir() {
				return
			}
		}
	}
	t.Skip("SKIPPED, NOT VERIFIED: plugin-examples/hello-process needs python3 in /usr/local/bin, /opt/homebrew/bin or /usr/bin (the plugin host's fixed PATH); install Python 3 to run it")
}

// TestExamplePlugin_EndToEnd installs the shipped example through the same
// RPCs the CLI uses, against the real host: process, the RPC-backed slash
// command, the typed setting and the run.finished hook.
func TestExamplePlugin_EndToEnd(t *testing.T) {
	examplePython(t)
	e := newProcEnv(t)
	src, err := filepath.Abs("../../plugin-examples/hello-process")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.call("plugin.install", localRC(), map[string]any{"source": src})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": "hello-process", "confirm": res.(plugins.Info).Consent.Fingerprint}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	e.waitRunning(t, "hello-process")

	// the RPC-backed command is listed, with the rpc and no prompt text
	cmds, err := e.call("plugin.commands", relayRC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cmds)
	if !strings.Contains(string(b), `"qualified":"hello-process:hello"`) || !strings.Contains(string(b), `"rpc":"echoPrompt"`) || strings.Contains(string(b), `"prompt"`) {
		t.Fatalf("plugin.commands = %s", b)
	}

	prompt := func(args string) string {
		t.Helper()
		got, err := e.pcall(relayRC(), "hello-process", "echoPrompt", map[string]any{"command": "hello-process:hello", "args": args})
		if err != nil {
			t.Fatalf("echoPrompt: %v", err)
		}
		var r struct{ Prompt string }
		if err := json.Unmarshal(got, &r); err != nil {
			t.Fatal(err)
		}
		return r.Prompt
	}
	if got := prompt("review the diff"); got != "Hello! review the diff" {
		t.Fatalf("prompt = %q", got)
	}

	// the typed setting reaches the process (a change restarts it)
	if _, err := e.call("plugin.settings.set", localRC(), map[string]any{"id": "hello-process", "values": map[string]any{"greeting": "Ahoy"}}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	e.waitRunning(t, "hello-process")
	if got := prompt(""); !strings.HasPrefix(got, "Ahoy! ") {
		t.Fatalf("after the setting changed: %q", got)
	}

	// an undeclared method never reaches the script
	if _, err := e.pcall(localRC(), "hello-process", "nope", nil); err == nil {
		t.Fatal("undeclared rpc accepted")
	}

	// the run.finished hook logs
	e.api.plugins.NotifyRunFinished(plugins.RunFinishedInput{RunID: "run-42", TaskID: 7, Status: "done", Origin: "human"})
	end := time.Now().Add(10 * time.Second)
	for {
		logs, err := e.call("plugin.logs", localRC(), map[string]any{"id": "hello-process", "limit": 500})
		if err != nil {
			t.Fatal(err)
		}
		lb, _ := json.Marshal(logs)
		if strings.Contains(string(lb), "run finished: run=run-42 task=7 status=done") {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("the hook never logged; log = %s", lb)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
