package automation

import (
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/store"
)

// Create validates def (defaults applied) and stores it DISABLED.
func Create(st *store.Store, def Def, lim Limits) (store.Automation, error) {
	def = def.WithDefaults()
	if err := def.Validate(lim); err != nil {
		return store.Automation{}, err
	}
	if _, err := st.GetWorkspace(def.WorkspaceID); err != nil {
		return store.Automation{}, fmt.Errorf("workspace %d not found", def.WorkspaceID)
	}
	if err := checkProfile(st, def, lim); err != nil {
		return store.Automation{}, err
	}
	return st.InsertAutomation(toRow(def, 0))
}

// Update validates def and replaces the human-authored fields of id; the
// engine's state (status, counters, cursors) is untouched, except that the
// schedule re-primes and a budget block clears.
func Update(st *store.Store, id int64, def Def, lim Limits) (store.Automation, error) {
	def = def.WithDefaults()
	if err := def.Validate(lim); err != nil {
		return store.Automation{}, err
	}
	if _, err := st.GetWorkspace(def.WorkspaceID); err != nil {
		return store.Automation{}, fmt.Errorf("workspace %d not found", def.WorkspaceID)
	}
	if err := checkProfile(st, def, lim); err != nil {
		return store.Automation{}, err
	}
	if err := st.UpdateAutomationDefinition(toRow(def, id)); err != nil {
		return store.Automation{}, err
	}
	return st.GetAutomation(id)
}

func toRow(d Def, id int64) store.Automation {
	return store.Automation{
		ID: id, Name: d.Name, WorkspaceID: d.WorkspaceID, SignalKind: d.Signal.Kind,
		Signal: mustJSON(d.Signal), Filter: "{}", Template: mustJSON(d.Template), Rails: mustJSON(d.Rails),
	}
}

// DefOf decodes a stored automation's definition.
func DefOf(a store.Automation) (Def, error) {
	d := Def{Name: a.Name, WorkspaceID: a.WorkspaceID}
	if err := json.Unmarshal([]byte(a.Signal), &d.Signal); err != nil {
		return d, fmt.Errorf("decode signal: %w", err)
	}
	if err := json.Unmarshal([]byte(a.Template), &d.Template); err != nil {
		return d, fmt.Errorf("decode template: %w", err)
	}
	if err := json.Unmarshal([]byte(a.Rails), &d.Rails); err != nil {
		return d, fmt.Errorf("decode rails: %w", err)
	}
	return d, nil
}

// checkProfile refuses a profile that would run unattended: a profile with
// autoAccept, or an unattended permission mode the template inherits, is only
// allowed with automations.allowUnattendedModes (ADR-0044 section 4).
func checkProfile(st *store.Store, def Def, lim Limits) error {
	p, err := st.GetAgentProfile(def.Template.ProfileID)
	if err != nil {
		return fmt.Errorf("profile %d not found", def.Template.ProfileID)
	}
	mode := def.Template.Mode
	if mode == "" {
		mode = p.PermissionMode
	}
	return RefuseUnattended(p.Name, p.AutoAccept, mode, lim)
}

// RefuseUnattended is the one rule for "this would run unattended": a profile
// with autoAccept or an unattended permission mode needs
// automations.allowUnattendedModes. The daemon adapter repeats it at launch, so
// a row edited behind the RPCs' back still cannot start such a task.
func RefuseUnattended(profileName string, autoAccept bool, mode string, lim Limits) error {
	if (autoAccept || unattendedModes[mode]) && !lim.AllowUnattendedModes {
		return fmt.Errorf("profile %q runs unattended (autoAccept or mode %q): set automations.allowUnattendedModes: true in config.yaml to allow it", profileName, mode)
	}
	return nil
}
