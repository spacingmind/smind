package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
)

// installRPC installs and enables a process plugin (the hosttest helper) that
// declares the given contributes.rpc entries (a JSON array) and settings.
func (e *procEnv) installRPC(t *testing.T, id, rpcs, settings string) {
	t.Helper()
	cmd, _ := json.Marshal(hosttest.Command("serve"))
	if settings == "" {
		settings = "[]"
	}
	man := `{"id":"` + id + `","name":"P","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},"process":{"command":` + string(cmd) + `},` +
		`"capabilities":["tasks.read"],"settings":` + settings + `,"contributes":{"rpc":` + rpcs + `}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "smind-plugin.json"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := e.call("plugin.install", localRC(), map[string]any{"source": dir})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": id, "confirm": res.(plugins.Info).Consent.Fingerprint}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	e.waitRunning(t, id)
}

func (e *procEnv) pcall(rc *requestContext, id, rpc string, params any) (json.RawMessage, error) {
	res, err := e.call("plugin.call", rc, map[string]any{"pluginId": id, "rpc": rpc, "params": params})
	if err != nil {
		return nil, err
	}
	return res.(json.RawMessage), nil
}

func wantCallCode(t *testing.T, err error, code string) {
	t.Helper()
	var ce *rpcCodeError
	if !errors.As(err, &ce) || ce.code != code {
		t.Fatalf("err = %v, want code %q", err, code)
	}
}

const stdRPCs = `[{"name":"echo"},{"name":"slow"},{"name":"huge"},{"name":"fail"},{"name":"die"}]`

func TestPluginCall_RoutesDeclaredRPC(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", stdRPCs, "")
	for name, rc := range map[string]*requestContext{"local": localRC(), "relay": relayRC(), "remote-login": {local: true, channel: ChannelPlain}} {
		got, err := e.pcall(rc, "alpha", "echo", map[string]any{"who": name, "n": 3})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var v struct {
			Who string
			N   int
		}
		if err := json.Unmarshal(got, &v); err != nil || v.Who != name || v.N != 3 {
			t.Fatalf("%s: result %s", name, got)
		}
	}
	// no params at all is {}
	if got, err := e.pcall(localRC(), "alpha", "echo", nil); err != nil || string(got) != "{}" {
		t.Fatalf("nil params: %s %v", got, err)
	}
}

func TestPluginCall_UndeclaredRPC_NeverForwarded(t *testing.T) {
	e := newProcEnv(t)
	// the helper would exit on rpc.die: declared nowhere, so it must never arrive
	e.installRPC(t, "alpha", `[{"name":"echo"}]`, "")
	before, _ := e.api.plugins.ProcessStatus("alpha")
	for _, name := range []string{"die", "slow", "../die", "plugin.shutdown", "echo ", "ECHO", "initialize"} {
		_, err := e.pcall(localRC(), "alpha", name, map[string]any{})
		wantCallCode(t, err, plugins.CodeUnknownRPC)
		if !strings.Contains(err.Error(), "alpha") {
			t.Errorf("error does not name the plugin: %v", err)
		}
	}
	after, ok := e.api.plugins.ProcessStatus("alpha")
	if !ok || after.State != host.StateRunning || after.PID != before.PID || after.Restarts != 0 {
		t.Fatalf("the process was disturbed by undeclared calls: %+v -> %+v", before, after)
	}
}

func TestPluginCall_PluginUnavailable_Typed(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", stdRPCs, "")
	_, err := e.pcall(localRC(), "nosuch", "echo", nil)
	wantCallCode(t, err, plugins.CodePluginUnavailable)

	// killed mid-flight: the call in progress fails typed, and the plugin is
	// unavailable until the supervisor brings it back
	_, err = e.pcall(localRC(), "alpha", "die", nil)
	wantCallCode(t, err, plugins.CodePluginUnavailable)

	// disabled
	if _, err := e.call("plugin.disable", localRC(), map[string]any{"id": "alpha"}); err != nil {
		t.Fatal(err)
	}
	_, err = e.pcall(localRC(), "alpha", "echo", nil)
	wantCallCode(t, err, plugins.CodePluginUnavailable)

	// switch off
	e.installRPC(t, "beta", stdRPCs, "")
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), []byte("plugins:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = e.pcall(localRC(), "beta", "echo", nil)
	wantCallCode(t, err, plugins.CodePluginUnavailable)
}

func TestPluginCall_TimeoutAndSizeCaps(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", `[{"name":"echo"},{"name":"slow","timeoutMs":300},{"name":"huge"}]`, "")

	if _, err := e.pcall(localRC(), "alpha", "slow", map[string]any{"ms": 10}); err != nil {
		t.Fatalf("a fast call under the timeout: %v", err)
	}
	start := time.Now()
	_, err := e.pcall(localRC(), "alpha", "slow", map[string]any{"ms": 1500})
	wantCallCode(t, err, plugins.CodeCallTimeout)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the manifest timeout was not enforced: took %s", d)
	}

	big := map[string]any{"pad": strings.Repeat("a", plugins.MaxCallParams+1)}
	_, err = e.pcall(localRC(), "alpha", "echo", big)
	wantCallCode(t, err, plugins.CodeCallTooLarge)

	_, err = e.pcall(localRC(), "alpha", "huge", map[string]any{"bytes": plugins.MaxCallResult + 10})
	wantCallCode(t, err, plugins.CodeCallTooLarge)
	if got, err := e.pcall(localRC(), "alpha", "huge", map[string]any{"bytes": plugins.MaxCallResult - 1024}); err != nil || len(got) < plugins.MaxCallResult-2048 {
		t.Fatalf("a result under the cap: %d bytes, %v", len(got), err)
	}

	raw, _ := json.Marshal(map[string]any{"pluginId": "alpha", "rpc": "echo", "params": json.RawMessage(`{"a":`)})
	if _, err := e.api.hs["plugin.call"](context.Background(), localRC(), raw); err == nil {
		t.Fatal("malformed request accepted")
	}
}

func TestPluginCall_ResultRedactedWhereLogged(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", `[{"name":"echo"},{"name":"fail"}]`,
		`[{"key":"tok","type":"secret","label":"Token"}]`)
	const secret = "Zq9-correct-horse-battery"
	if _, err := e.call("plugin.settings.set", localRC(), map[string]any{"id": "alpha", "values": map[string]any{"tok": secret}}); err != nil {
		t.Fatal(err)
	}
	e.waitRunning(t, "alpha")
	const shape = "sk-abcdefghijklmnopqrstuvwxyz0123456789"

	if _, err := e.pcall(localRC(), "alpha", "echo", map[string]any{"a": shape, "b": secret}); err != nil {
		t.Fatal(err)
	}
	// a failure message is untrusted: control characters out, clipped, redacted
	_, err := e.pcall(localRC(), "alpha", "fail", map[string]any{"msg": "bad " + shape + " and " + secret + "\x1b[31m" + strings.Repeat("z", 5000)})
	wantCallCode(t, err, plugins.CodePluginError)
	msg := err.Error()
	if strings.Contains(msg, shape) || strings.Contains(msg, secret) || strings.Contains(msg, "\x1b") || len(msg) > 700 {
		t.Fatalf("error text not scrubbed/clipped (%d bytes): %q", len(msg), msg)
	}
	if !strings.Contains(msg, "alpha") {
		t.Fatalf("error does not name the plugin: %v", err)
	}

	logs, err := e.call("plugin.logs", localRC(), map[string]any{"id": "alpha", "limit": 1000})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(logs)
	if strings.Contains(string(b), shape) || strings.Contains(string(b), secret) {
		t.Fatalf("a result or error reached the plugin log unredacted: %s", b)
	}
	if !strings.Contains(string(b), "rpc echo ok") || !strings.Contains(string(b), "rpc fail failed") {
		t.Fatalf("calls were not logged: %s", b)
	}
}

func TestPluginCall_GuardsPerCaller(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", stdRPCs, "")
	for name, rc := range map[string]*requestContext{"agent": agentRC(), "nil": nil} {
		_, err := e.pcall(rc, "alpha", "echo", nil)
		wantCallCode(t, err, "forbidden")
		if err == nil || !strings.Contains(err.Error(), "agent") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// plugin.call is not a method an agent connection is allowed or a plugin may
	// list: the drift guards behind the handler's own check
	if runScopedSet["plugin.call"] {
		t.Fatal("plugin.call is on RunScopedMethods")
	}
	if isTable("plugin.call") {
		t.Fatal("plugin.call is in the plugin capability table")
	}
	// human callers: loopback, a remote login (allowRemoteManage stays off:
	// calling is not managing) and the relay (the phone)
	for name, rc := range map[string]*requestContext{"loopback": localRC(), "remote": {local: true, channel: ChannelPlain}, "relay": relayRC()} {
		if _, err := e.pcall(rc, "alpha", "echo", map[string]any{}); err != nil {
			t.Errorf("%s caller refused: %v", name, err)
		}
	}
	// a plugin-scoped session reaches nothing under plugin.*
	_, rpcErr := e.hostCall(t, "alpha", "plugin.call", map[string]any{"pluginId": "alpha", "rpc": "echo"})
	if rpcErr == nil || rpcErr.Code != host.ErrCodeForbidden {
		t.Fatalf("plugin-scoped plugin.call = %+v, want forbidden", rpcErr)
	}
}

func TestPluginCall_NoPluginToPlugin(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", stdRPCs, "")
	e.installRPC(t, "beta", stdRPCs, "")
	before, _ := e.api.plugins.ProcessStatus("beta")
	// beta's `die` would kill it if the call reached it
	_, rpcErr := e.hostCall(t, "alpha", "plugin.call", map[string]any{"pluginId": "beta", "rpc": "die"})
	if rpcErr == nil || rpcErr.Code != host.ErrCodeForbidden {
		t.Fatalf("alpha -> beta plugin.call = %+v, want forbidden", rpcErr)
	}
	time.Sleep(100 * time.Millisecond)
	after, ok := e.api.plugins.ProcessStatus("beta")
	if !ok || after.State != host.StateRunning || after.PID != before.PID || after.Restarts != 0 {
		t.Fatalf("beta was reached: %+v -> %+v", before, after)
	}
}

func TestPluginCall_ConcurrencyCap(t *testing.T) {
	e := newProcEnv(t)
	e.installRPC(t, "alpha", stdRPCs, "")
	var wg sync.WaitGroup
	errs := make(chan error, plugins.MaxCallsPerPlugin)
	for i := 0; i < plugins.MaxCallsPerPlugin; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.pcall(localRC(), "alpha", "slow", map[string]any{"ms": 700})
			errs <- err
		}()
	}
	// wait until the cap is actually held, then one more must be refused at once
	time.Sleep(250 * time.Millisecond)
	start := time.Now()
	_, err := e.pcall(localRC(), "alpha", "echo", nil)
	wantCallCode(t, err, plugins.CodePluginBusy)
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("the over-cap call waited %s instead of failing at once", time.Since(start))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("an in-cap call failed: %v", err)
		}
	}
	if _, err := e.pcall(localRC(), "alpha", "echo", nil); err != nil {
		t.Fatalf("the gate did not release: %v", err)
	}
}
