package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spacingmind/smind/internal/plugins/uiview"
)

// Panel is one panel of an active plugin as clients list it. UI is the view
// schema level the panel needs, with 0 already read as the default 1.
type Panel struct {
	Plugin string `json:"plugin"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Icon   string `json:"icon,omitempty"`
	Where  string `json:"where"`
	UI     int    `json:"ui"`
}

// SidebarItem is a sidebar entry of an active plugin.
type SidebarItem struct {
	Plugin string `json:"plugin"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Icon   string `json:"icon,omitempty"`
	Opens  string `json:"opens"`
}

// Panels lists panels and sidebar items of every ACTIVE plugin (empty with
// the global switch off), ordered by plugin id then declaration order. A
// panel needing a higher ui level than `level` is withheld; its sidebar
// items too. level <= 0 returns nothing.
func (m *Manager) Panels(level int) ([]Panel, []SidebarItem) {
	if level <= 0 {
		return nil, nil
	}
	infos := m.Active()
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	var panels []Panel
	var sidebar []SidebarItem
	for _, in := range infos {
		man := m.loadedManifest(in)
		if man == nil {
			continue
		}
		shown := map[string]bool{}
		for _, p := range man.Contributes.Panels {
			ui := p.UI
			if ui == 0 {
				ui = 1
			}
			if ui > level {
				continue
			}
			shown[p.ID] = true
			panels = append(panels, Panel{Plugin: in.ID, ID: p.ID, Title: p.Title, Icon: p.Icon, Where: p.Where, UI: ui})
		}
		for _, s := range man.Contributes.Sidebar {
			if !shown[s.Opens] {
				continue
			}
			sidebar = append(sidebar, SidebarItem{Plugin: in.ID, ID: s.ID, Title: s.Title, Icon: s.Icon, Opens: s.Opens})
		}
	}
	return panels, sidebar
}

// View returns the normalised view document of one panel of an ACTIVE plugin.
// level is the client's ui level, panelContext a JSON object (or empty)
// passed to an rpc view. A document is only ever returned whole and
// validated: a plugin's answer that fails validation is a typed view_invalid
// error, never a truncated or partly cleaned view.
func (m *Manager) View(ctx context.Context, pluginID, panelID string, level int, panelContext json.RawMessage) (json.RawMessage, error) {
	if m.proc == nil || !m.GlobalEnabled() {
		return nil, newErr(CodePluginUnavailable, "plugin %s: plugins are not running", pluginID)
	}
	var info *Info
	for _, in := range m.Active() {
		if in.ID == pluginID {
			in := in
			info = &in
			break
		}
	}
	if info == nil {
		return nil, newErr(CodePluginUnavailable, "plugin %s: it is not installed and enabled", pluginID)
	}
	man := m.loadedManifest(*info)
	if man == nil {
		return nil, newErr(CodePluginUnavailable, "plugin %s: it is not loaded", pluginID)
	}
	var panel *PanelContribution
	for i := range man.Contributes.Panels {
		if man.Contributes.Panels[i].ID == panelID {
			panel = &man.Contributes.Panels[i]
		}
	}
	if panel == nil {
		return nil, newErr(CodeUnknownPanel, "plugin %s does not declare panel %q", pluginID, clipText(panelID, 64))
	}
	need := panel.UI
	if need == 0 {
		need = 1
	}
	if need > level {
		return nil, newErr(CodeUILevelTooLow, "this client supports ui level %d; the panel needs %d", level, need)
	}
	if t := bytes.TrimSpace(panelContext); len(t) == 0 || string(t) == "null" {
		panelContext = json.RawMessage("{}")
	} else if t[0] != '{' || !json.Valid(t) {
		return nil, newErr(CodeBadCall, "plugin %s: the panel context must be a JSON object", pluginID)
	} else if len(t) > uiview.MaxParamsBytes {
		return nil, newErr(CodeBadCall, "plugin %s: the panel context is %d bytes, over the %d byte cap", pluginID, len(t), uiview.MaxParamsBytes)
	} else {
		panelContext = t
	}

	if panel.View.File != "" {
		doc, ok := man.panelViews[panelID]
		if !ok {
			return nil, newErr(CodePluginUnavailable, "plugin %s: the view of panel %s is not loaded", pluginID, panelID)
		}
		return append(json.RawMessage(nil), doc...), nil
	}
	params, err := json.Marshal(struct {
		PanelID string          `json:"panelId"`
		Context json.RawMessage `json:"context"`
	}{panelID, panelContext})
	if err != nil {
		return nil, newErr(CodeBadCall, "plugin %s: %v", pluginID, err)
	}
	raw, err := m.Call(ctx, pluginID, panel.View.RPC, params)
	if err != nil {
		return nil, err
	}
	out, verr := uiview.Validate(raw, viewEnv(man))
	if verr != nil {
		return nil, newErr(CodeViewInvalid, "%s", clipText(fmt.Sprintf("plugin %s panel %s: %s", pluginID, panelID, verr.Error()), 300))
	}
	return out, nil
}
