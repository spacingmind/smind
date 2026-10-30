package plugins

import "sort"

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
