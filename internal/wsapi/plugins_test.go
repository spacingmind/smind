package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/store"
)

type pluginHarness struct {
	home string
	hs   map[string]handlerFunc
	src  string
	on   *bool
}

func newPluginHarness(t *testing.T) *pluginHarness {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	on := true
	mgr := plugins.New(plugins.Options{Home: home, Store: st, Version: "0.15.0", Enabled: func() bool { return on }})
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, plugins.ManifestFile),
		[]byte(`{"id":"demo","name":"Demo","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return &pluginHarness{home: home, hs: pluginHandlers(mgr, nil), src: src, on: &on}
}

func (h *pluginHarness) call(method string, rc *requestContext, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.hs[method](context.Background(), rc, raw)
}

// pluginParamsFor gives each mutating method a params object that would
// succeed for a human, so a refusal is the guard and not a bad-params error.
func (h *pluginHarness) pluginParamsFor(method string) map[string]any {
	switch method {
	case "plugin.install":
		return map[string]any{"source": h.src}
	case "plugin.link":
		return map[string]any{"path": h.src}
	default:
		return map[string]any{"id": "demo", "confirm": "x"}
	}
}

func wantPluginForbidden(t *testing.T, method string, err error) {
	t.Helper()
	var ce *rpcCodeError
	if err == nil {
		t.Fatalf("%s: want forbidden, got success", method)
	}
	if e, ok := err.(*rpcCodeError); ok {
		ce = e
	}
	if ce == nil || ce.RPCCode() != codeForbidden {
		t.Fatalf("%s: want code forbidden, got %v", method, err)
	}
}

func TestPluginRPC_RunScopedCallersForbidden(t *testing.T) {
	h := newPluginHarness(t)
	agent := &requestContext{runScoped: true, local: true}
	for _, m := range pluginMutating {
		_, err := h.call(m, agent, h.pluginParamsFor(m))
		wantPluginForbidden(t, m, err)
		// A nil request context fails closed too.
		_, err = h.call(m, nil, h.pluginParamsFor(m))
		wantPluginForbidden(t, m+" (nil rc)", err)
	}
	if _, err := os.Stat(filepath.Join(h.home, "plugins")); err == nil {
		t.Fatal("a refused call touched the plugins dir")
	}
	// None of the plugin methods is on the run-scoped allowlist either, so an
	// identified agent connection is refused before any handler runs.
	for _, m := range append(append([]string{}, pluginMutating...), "plugin.list", "plugin.get") {
		if runScopedSet[m] {
			t.Errorf("%s is on RunScopedMethods", m)
		}
	}
	// The same human request with a local, non-agent context succeeds.
	if _, err := h.call("plugin.install", &requestContext{local: true}, h.pluginParamsFor("plugin.install")); err != nil {
		t.Fatalf("local human install: %v", err)
	}
}

func TestPluginRPC_RelaySessionsForbidden_ListGetAllowed(t *testing.T) {
	h := newPluginHarness(t)
	// Install as the local human so list/get have something to return.
	if _, err := h.call("plugin.install", &requestContext{local: true}, h.pluginParamsFor("plugin.install")); err != nil {
		t.Fatal(err)
	}
	relay := &requestContext{local: false, deviceID: "dev-1", channel: ChannelE2EE}
	for _, m := range pluginMutating {
		_, err := h.call(m, relay, h.pluginParamsFor(m))
		wantPluginForbidden(t, m, err)
	}
	// A legacy relay client has no device id; still not local.
	legacy := &requestContext{local: false}
	for _, m := range pluginMutating {
		_, err := h.call(m, legacy, h.pluginParamsFor(m))
		wantPluginForbidden(t, m+" (legacy relay)", err)
	}
	res, err := h.call("plugin.list", relay, nil)
	if err != nil {
		t.Fatalf("plugin.list over the relay: %v", err)
	}
	l := res.(plugins.Listing)
	if !l.GlobalEnabled || len(l.Plugins) != 1 || l.Plugins[0].Enabled {
		t.Fatalf("%+v", l)
	}
	if _, err := h.call("plugin.get", relay, map[string]any{"id": "demo"}); err != nil {
		t.Fatalf("plugin.get over the relay: %v", err)
	}
	// Nothing the relay tried changed state.
	if got, _ := h.call("plugin.get", relay, map[string]any{"id": "demo"}); got.(plugins.Info).Enabled {
		t.Fatal("relay enabled a plugin")
	}
	if _, err := h.call("plugin.get", relay, map[string]any{"id": "ghost"}); err == nil {
		t.Fatal("unknown id")
	} else if ce, ok := err.(*rpcCodeError); !ok || ce.RPCCode() != plugins.CodeNotFound {
		t.Fatalf("not_found code lost: %v", err)
	}
}

func TestPluginRPC_LocalFlow_TypedErrorsAndSwitch(t *testing.T) {
	h := newPluginHarness(t)
	local := &requestContext{local: true}
	res, err := h.call("plugin.install", local, h.pluginParamsFor("plugin.install"))
	if err != nil {
		t.Fatal(err)
	}
	in := res.(plugins.Info)
	if in.Enabled {
		t.Fatal("install enabled the plugin")
	}
	// Enabling needs the fingerprint confirmation.
	_, err = h.call("plugin.enable", local, map[string]any{"id": "demo"})
	if ce, ok := err.(*rpcCodeError); !ok || ce.RPCCode() != plugins.CodeConsent {
		t.Fatalf("enable without confirm: %v", err)
	}
	if res, err = h.call("plugin.enable", local, map[string]any{"id": "demo", "confirm": in.Consent.Fingerprint}); err != nil || !res.(plugins.Info).Enabled {
		t.Fatalf("%v %v", res, err)
	}
	// Unpinned git is a typed error on the wire.
	_, err = h.call("plugin.install", local, map[string]any{"source": "git+https://example.invalid/r.git@main"})
	if ce, ok := err.(*rpcCodeError); !ok || ce.RPCCode() != plugins.CodeUnpinnedRef {
		t.Fatalf("unpinned_ref lost: %v", err)
	}
	// With the switch off, install is refused with plugins_disabled and list says so.
	*h.on = false
	_, err = h.call("plugin.install", local, map[string]any{"source": h.src})
	if ce, ok := err.(*rpcCodeError); !ok || ce.RPCCode() != plugins.CodeSwitchOff {
		t.Fatalf("switch off: %v", err)
	}
	res, _ = h.call("plugin.list", local, nil)
	if l := res.(plugins.Listing); l.GlobalEnabled || l.Plugins[0].Active {
		t.Fatalf("%+v", l)
	}
	if _, err := h.call("plugin.remove", local, map[string]any{"id": "demo"}); err != nil {
		t.Fatalf("remove works with the switch off: %v", err)
	}
}

// Over the real dispatch: an identified (run-scoped) connection is refused
// every plugin method by the allowlist gate, relay sessions (ServeDevice and the
// legacy ServeTransport) are refused every mutating method, and the daemon's own
// switch is OFF by default so a local list reports it.
func TestPluginRPC_OverWS_AgentRefused_SwitchDefaultOff(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	e := newIdentEnv(t, "hang")
	agent := e.identified(t)
	for _, m := range append([]string{"plugin.list", "plugin.get"}, pluginMutating...) {
		wantForbidden(t, callWS(t, agent, m, map[string]any{"id": "demo", "source": t.TempDir(), "path": t.TempDir()}), "not available to agents")
	}
	// The real relay paths (a paired device, and the legacy relay client) are
	// not local: every mutating method is forbidden, the reads work.
	for _, dev := range []string{"dev-1", ""} {
		for _, m := range pluginMutating {
			env := callDevice(t, e.api, dev, m, mustJSON(t, map[string]any{"id": "demo", "source": t.TempDir(), "path": t.TempDir()}))
			wantCode(t, env, codeForbidden)
		}
		for _, m := range []string{"plugin.list", "plugin.get"} {
			env := callDevice(t, e.api, dev, m, mustJSON(t, map[string]any{"id": "demo"}))
			if m == "plugin.list" && env.Error != nil {
				t.Fatalf("%s over the relay (device %q): %+v", m, dev, env.Error)
			}
			if m == "plugin.get" {
				wantCode(t, env, plugins.CodeNotFound) // reaches the handler, nothing installed
			}
		}
	}
	resp := callWS(t, e.admin, "plugin.list", nil)
	wantOK(t, resp, "plugin.list")
	var l plugins.Listing
	if err := json.Unmarshal(resp.Result, &l); err != nil || l.GlobalEnabled || len(l.Plugins) != 0 {
		t.Fatalf("%+v %v", l, err)
	}
	resp = callWS(t, e.admin, "plugin.install", map[string]any{"source": t.TempDir()})
	if resp.Error == nil || resp.Error.Code != plugins.CodeSwitchOff {
		t.Fatalf("install with the switch off: %+v", resp.Error)
	}
}
