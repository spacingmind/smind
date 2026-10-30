package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins"
)

// panelsEnv is a real API with plugins switched on and one data-only plugin
// ("paneldemo": two file-backed panels, one sidebar item) installed and
// enabled. A second plugin ("late") is installed but never enabled, so the
// lists prove OnlyActive.
type panelsEnv struct {
	api  *API
	home string
}

func newPanelsEnv(t *testing.T) *panelsEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("plugins:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	e := &panelsEnv{api: api, home: home}

	manifest := func(id string) string {
		return `{"id":"` + id + `","name":"P","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},` +
			`"contributes":{"panels":[{"id":"notes","title":"Notes","icon":"StickyNote","where":"sidebar","view":{"file":"views/notes.json"}}],` +
			`"sidebar":[{"id":"notes-item","title":"Notes","icon":"StickyNote","opens":"notes"}]}}`
	}
	for _, id := range []string{"paneldemo", "late"} {
		src := t.TempDir()
		files := map[string]string{
			"smind-plugin.json": manifest(id),
			"views/notes.json":  "{}",
		}
		for name, body := range files {
			full := filepath.Join(src, name)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		res, err := e.call("plugin.install", localRC(), map[string]any{"source": src})
		if err != nil {
			t.Fatalf("install %s: %v", id, err)
		}
		if id == "paneldemo" {
			if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": id, "confirm": res.(plugins.Info).Consent.Fingerprint}); err != nil {
				t.Fatalf("enable %s: %v", id, err)
			}
		}
	}
	return e
}

func (e *panelsEnv) call(method string, rc *requestContext, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return e.api.hs[method](context.Background(), rc, raw)
}

func (e *panelsEnv) list(t *testing.T, rc *requestContext, params any) (panels []plugins.Panel, sidebar []plugins.SidebarItem, raw string) {
	t.Helper()
	res, err := e.call("plugin.panels", rc, params)
	if err != nil {
		t.Fatalf("plugin.panels: %v", err)
	}
	b, _ := json.Marshal(res)
	raw = string(b)
	var got struct {
		Panels  []plugins.Panel       `json:"panels"`
		Sidebar []plugins.SidebarItem `json:"sidebar"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("plugin.panels result %s: %v", raw, err)
	}
	return got.Panels, got.Sidebar, raw
}

func TestPluginPanels_List_OnlyActive_GatedByUiLevel(t *testing.T) {
	e := newPanelsEnv(t)

	// No ui level reported: the daemon withholds every panel.
	panels, sidebar, raw := e.list(t, localRC(), nil)
	if len(panels) != 0 || len(sidebar) != 0 {
		t.Fatalf("ui omitted must list nothing: %s", raw)
	}
	if raw != `{"panels":[],"sidebar":[]}` {
		t.Fatalf("empty lists must be [] not null: %s", raw)
	}
	panels, sidebar, raw = e.list(t, relayRC(), map[string]any{"ui": 1})
	if len(panels) != 1 || panels[0].Plugin != "paneldemo" || panels[0].ID != "notes" || panels[0].Title != "Notes" || panels[0].Icon != "StickyNote" || panels[0].Where != "sidebar" || panels[0].UI != 1 {
		t.Fatalf("panels = %s", raw)
	}
	if len(sidebar) != 1 || sidebar[0].Plugin != "paneldemo" || sidebar[0].ID != "notes-item" || sidebar[0].Opens != "notes" {
		t.Fatalf("sidebar = %s", raw)
	}
	if strings.Contains(raw, "late") {
		t.Fatalf("an installed but disabled plugin must not appear: %s", raw)
	}
}

func TestPluginPanels_DisabledPluginVanishes(t *testing.T) {
	e := newPanelsEnv(t)
	if panels, _, raw := e.list(t, localRC(), map[string]any{"ui": 1}); len(panels) != 1 {
		t.Fatalf("precondition: %s", raw)
	}
	if _, err := e.call("plugin.disable", localRC(), map[string]any{"id": "paneldemo"}); err != nil {
		t.Fatal(err)
	}
	if panels, sidebar, raw := e.list(t, localRC(), map[string]any{"ui": 1}); len(panels) != 0 || len(sidebar) != 0 {
		t.Fatalf("a disabled plugin's panels must vanish: %s", raw)
	}
}

func TestPluginPanels_RunScopedRefused(t *testing.T) {
	e := newPanelsEnv(t)
	for name, rc := range map[string]*requestContext{"agent": agentRC(), "nil context": nil} {
		_, err := e.call("plugin.panels", rc, map[string]any{"ui": 1})
		ce, ok := err.(*rpcCodeError)
		if !ok || ce.code != codeForbidden || !strings.Contains(err.Error(), "not available to agents") {
			t.Fatalf("%s: err = %v, want forbidden", name, err)
		}
	}
}
