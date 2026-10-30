package plugins

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// panelsTestManifest builds a manifest with a process, one declared rpc, two
// panels (rpc-backed and file-backed) and a sidebar item; mut edits or drops
// keys. Panel-only, so it never collides with manifest_test.go's base.
func panelsTestManifest(mut func(m map[string]string)) string {
	f := map[string]string{
		"id": `"acme"`, "name": `"Acme"`, "version": `"1.0.0"`, "api": `1`,
		"engines": `{"smind":">=0.1.0"}`,
		"process": `{"command":["node","server.js"]}`,
		"contributes": `{"rpc":[{"name":"render"}],` +
			`"panels":[` +
			`{"id":"tasks","title":"Tasks","icon":"ListChecks","where":"task","view":{"rpc":"render"}},` +
			`{"id":"notes","title":"Notes","where":"sidebar","ui":1,"view":{"file":"views/notes.json"}}],` +
			`"sidebar":[{"id":"notes-item","title":"Notes","icon":"StickyNote","opens":"notes"}]}`,
	}
	mut(f)
	var parts []string
	for k, v := range f {
		if v != "" {
			parts = append(parts, fmt.Sprintf("%q:%s", k, v))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestManifest_Panels_Valid(t *testing.T) {
	m, err := ParseManifest([]byte(panelsTestManifest(func(map[string]string) {})), "0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.Panels) != 2 || len(m.Contributes.Sidebar) != 1 {
		t.Fatalf("panels %+v sidebar %+v", m.Contributes.Panels, m.Contributes.Sidebar)
	}
	tasks, notes := m.Contributes.Panels[0], m.Contributes.Panels[1]
	if tasks.ID != "tasks" || tasks.Title != "Tasks" || tasks.Icon != "ListChecks" || tasks.Where != "task" || tasks.View.RPC != "render" || tasks.View.File != "" {
		t.Fatalf("rpc panel: %+v", tasks)
	}
	// ui omitted reads as 0, the default level 1; an explicit 1 parses.
	if tasks.UI != 0 || notes.UI != 1 || notes.View.File != "views/notes.json" || notes.View.RPC != "" {
		t.Fatalf("file panel: %+v", notes)
	}
	if s := m.Contributes.Sidebar[0]; s.ID != "notes-item" || s.Title != "Notes" || s.Icon != "StickyNote" || s.Opens != "notes" {
		t.Fatalf("sidebar: %+v", s)
	}
}

func TestManifest_Panels_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(m map[string]string)
		field string
	}{
		{"bad where", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"dock","view":{"file":"p.json"}}]}`
		}, "contributes.panels[0].where"},
		{"duplicate id", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"p.json"}},{"id":"p","title":"Q","where":"task","view":{"file":"q.json"}}]}`
		}, "contributes.panels[1].id"},
		{"view both file and rpc", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"p.json","rpc":"render"}}]}`
		}, "contributes.panels[0].view"},
		{"view neither", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{}}]}`
		}, "contributes.panels[0].view"},
		{"file not json", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"views/p.txt"}}]}`
		}, "contributes.panels[0].view.file"},
		{"file path escape", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"../x.json"}}]}`
		}, "contributes.panels[0].view.file"},
		{"rpc undeclared", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"rpc":"render"}}]}`
		}, "contributes.panels[0].view.rpc"},
		{"rpc without process", func(m map[string]string) {
			m["process"] = ""
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"rpc":"render"}}]}`
		}, "contributes.panels[0].view.rpc"},
		{"icon not lucide case", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","icon":"lucide","where":"task","view":{"file":"p.json"}}]}`
		}, "contributes.panels[0].icon"},
		{"title empty", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"","where":"task","view":{"file":"p.json"}}]}`
		}, "contributes.panels[0].title"},
		{"sidebar opens unknown", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"p.json"}}],"sidebar":[{"id":"s","title":"S","opens":"ghost"}]}`
		}, "contributes.sidebar[0].opens"},
		{"sidebar duplicate", func(m map[string]string) {
			m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"p.json"}}],"sidebar":[{"id":"s","title":"S","opens":"p"},{"id":"s","title":"T","opens":"p"}]}`
		}, "contributes.sidebar[1].id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(panelsTestManifest(c.mut)), "0.15.0")
			var me *ManifestError
			if !errors.As(err, &me) {
				t.Fatalf("want ManifestError, got %v", err)
			}
			found := false
			for _, fe := range me.Errs {
				if fe.Field == c.field {
					found = true
					if fe.Problem == "" || fe.Fix == "" {
						t.Errorf("error for %s lacks problem or fix: %+v", c.field, fe)
					}
				}
			}
			if !found {
				t.Fatalf("no error named field %q: %v", c.field, me.Fields())
			}
			if CodeOf(err) != CodeBadManifest {
				t.Fatalf("code = %q", CodeOf(err))
			}
		})
	}
}

func TestManifest_PanelUiLevelAbove1_Rejected(t *testing.T) {
	_, err := ParseManifest([]byte(panelsTestManifest(func(m map[string]string) {
		m["contributes"] = `{"panels":[{"id":"p","title":"P","where":"task","ui":2,"view":{"file":"p.json"}}]}`
	})), "0.15.0")
	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("want ManifestError, got %v", err)
	}
	found := false
	for _, fe := range me.Errs {
		if fe.Field == "contributes.panels[0].ui" {
			found = true
			if fe.Fix != "this smind understands ui level 1" {
				t.Fatalf("fix = %q", fe.Fix)
			}
		}
	}
	if !found {
		t.Fatalf("no ui-level error: %v", me.Fields())
	}
}

func TestManifest_Panels_FileInDeclaredPaths(t *testing.T) {
	m, err := ParseManifest([]byte(panelsTestManifest(func(map[string]string) {})), "0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range m.DeclaredPaths() {
		seen[p] = true
	}
	if !seen["views/notes.json"] {
		t.Fatalf("a panel's view file must be a declared path: %v", m.DeclaredPaths())
	}
	if seen[""] {
		t.Fatal("an rpc panel must not add an empty path")
	}
}

// installPanelsPlugin installs and enables a plugin whose contributes is
// given verbatim; the two view files it may point at exist, as install
// requires of every declared path.
func installPanelsPlugin(t *testing.T, e *env, id, contributes string) {
	t.Helper()
	in := mustInstall(t, e, writePluginDir(t, manifestWith(id, "1.0.0", `"contributes":`+contributes), map[string]string{
		"views/main.json":   "{}",
		"views/second.json": "{}",
	}))
	if _, err := e.m.Enable(id, in.Consent.Fingerprint); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
}

func TestPanels_OnlyActive_OrderedByPluginID_GatedByLevel(t *testing.T) {
	e := newEnv(t)
	// Installed out of id order on purpose: the listing is by plugin id.
	installPanelsPlugin(t, e, "zeta", `{"panels":[{"id":"one","title":"One","where":"task","view":{"file":"views/main.json"}}],`+
		`"sidebar":[{"id":"one-item","title":"One","opens":"one"}]}`)
	installPanelsPlugin(t, e, "acme", `{"panels":[{"id":"alpha","title":"Alpha","icon":"ListChecks","where":"task","view":{"file":"views/main.json"}},`+
		`{"id":"beta","title":"Beta","where":"sidebar","ui":1,"view":{"file":"views/second.json"}}],`+
		`"sidebar":[{"id":"beta-item","title":"Beta","opens":"beta"}]}`)

	for _, level := range []int{0, -1} {
		if panels, sidebar := e.m.Panels(level); len(panels) != 0 || len(sidebar) != 0 {
			t.Fatalf("level %d must list nothing: %+v %+v", level, panels, sidebar)
		}
	}
	panels, sidebar := e.m.Panels(1)
	wantP := []Panel{
		{Plugin: "acme", ID: "alpha", Title: "Alpha", Icon: "ListChecks", Where: "task", UI: 1},
		{Plugin: "acme", ID: "beta", Title: "Beta", Where: "sidebar", UI: 1},
		{Plugin: "zeta", ID: "one", Title: "One", Where: "task", UI: 1},
	}
	if len(panels) != len(wantP) {
		t.Fatalf("panels = %+v", panels)
	}
	for i, p := range panels {
		if p != wantP[i] {
			t.Fatalf("panels[%d] = %+v, want %+v", i, p, wantP[i])
		}
	}
	wantS := []SidebarItem{
		{Plugin: "acme", ID: "beta-item", Title: "Beta", Opens: "beta"},
		{Plugin: "zeta", ID: "one-item", Title: "One", Opens: "one"},
	}
	if len(sidebar) != len(wantS) {
		t.Fatalf("sidebar = %+v", sidebar)
	}
	for i, s := range sidebar {
		if s != wantS[i] {
			t.Fatalf("sidebar[%d] = %+v, want %+v", i, s, wantS[i])
		}
	}
}

func TestPanels_DisabledPluginVanishes(t *testing.T) {
	e := newEnv(t)
	installPanelsPlugin(t, e, "acme", `{"panels":[{"id":"alpha","title":"Alpha","where":"task","view":{"file":"views/main.json"}}],`+
		`"sidebar":[{"id":"alpha-item","title":"Alpha","opens":"alpha"}]}`)
	if panels, sidebar := e.m.Panels(1); len(panels) != 1 || len(sidebar) != 1 {
		t.Fatalf("precondition: %+v %+v", panels, sidebar)
	}
	if _, err := e.m.Disable("acme"); err != nil {
		t.Fatal(err)
	}
	if panels, sidebar := e.m.Panels(1); len(panels) != 0 || len(sidebar) != 0 {
		t.Fatalf("a disabled plugin's panels must vanish: %+v %+v", panels, sidebar)
	}
}

func TestPanels_SwitchOff_Empty(t *testing.T) {
	e := newEnv(t)
	installPanelsPlugin(t, e, "acme", `{"panels":[{"id":"alpha","title":"Alpha","where":"task","view":{"file":"views/main.json"}}]}`)
	*e.on = false
	if panels, sidebar := e.m.Panels(1); len(panels) != 0 || len(sidebar) != 0 {
		t.Fatalf("the global switch off must empty the panel list: %+v %+v", panels, sidebar)
	}
}
