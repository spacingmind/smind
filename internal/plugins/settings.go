package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Plugin settings (ADR-0042 section 4, PB.2). A plugin declares typed settings
// in its manifest; the Human sets them (plugin.settings.set, local only). The
// values live in the plugin_settings table, secrets included, the way provider
// credentials and MCP env/headers do. The only places a value goes:
//
//   - the plugin's own plugin.initialize (decrypted: it is the plugin's);
//   - ${setting.KEY} in the plugin's MCP env and headers, at run injection.
//
// No read RPC, log, event, error string or plugin.list/get ever carries a
// secret: SettingView has no field that could.

// SettingView is one setting as a read RPC shows it. Value is present for a
// non-secret that is set; a secret has only Set.
type SettingView struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Options     []string `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
	MaxLength   int      `json:"maxLength,omitempty"`
	// Set is true when a value is stored (a default alone leaves it false).
	Set   bool `json:"set"`
	Value any  `json:"value,omitempty"`
}

// SettingsView is plugin.settings.get / set's result.
type SettingsView struct {
	ID       string        `json:"id"`
	Settings []SettingView `json:"settings"`
	// Missing lists required settings with neither a value nor a default.
	Missing []string `json:"missing,omitempty"`
}

func settingMax(s Setting) int {
	if s.MaxLength > 0 {
		return s.MaxLength
	}
	return MaxSettingString
}

// ValidateSettingValue decodes raw as the type s declares. Its errors name the
// setting's type and limit, never the value (it may be a secret).
func ValidateSettingValue(s Setting, raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return nil, fmt.Errorf("%s: not valid JSON", s.Key)
	}
	switch s.Type {
	case "string", "secret":
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", s.Key)
		}
		if len(str) > settingMax(s) {
			return nil, fmt.Errorf("%s is over its %d byte limit", s.Key, settingMax(s))
		}
		if !utf8.ValidString(str) || strings.ContainsRune(str, 0) {
			return nil, fmt.Errorf("%s must be UTF-8 text without NUL", s.Key)
		}
		return str, nil
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be true or false", s.Key)
		}
		return b, nil
	case "number":
		n, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("%s must be a number", s.Key)
		}
		f, err := n.Float64()
		if err != nil || f > 1e15 || f < -1e15 {
			return nil, fmt.Errorf("%s must be a finite number within 1e15", s.Key)
		}
		return f, nil
	case "enum":
		str, ok := v.(string)
		if !ok || !contains(s.Options, str) {
			return nil, fmt.Errorf("%s must be one of: %s", s.Key, strings.Join(s.Options, ", "))
		}
		return str, nil
	}
	return nil, fmt.Errorf("%s has an unknown type", s.Key)
}

// stored returns the plugin's stored values decoded against the manifest's
// declarations; a stored key the manifest no longer declares, or one that no
// longer validates (the declaration changed), is ignored.
func (m *Manager) stored(id string, man *Manifest) map[string]any {
	rows, err := m.st.ListPluginSettings(id)
	if err != nil || len(rows) == 0 || man == nil {
		return map[string]any{}
	}
	decl := map[string]Setting{}
	for _, s := range man.Settings {
		decl[s.Key] = s
	}
	out := map[string]any{}
	for _, r := range rows {
		s, ok := decl[r.Key]
		if !ok {
			continue
		}
		if v, err := ValidateSettingValue(s, json.RawMessage(r.Value)); err == nil {
			out[r.Key] = v
		}
	}
	return out
}

// resolved is the DECRYPTED settings object a plugin receives: each declared
// setting's stored value, else its default; unset ones are absent.
func (m *Manager) resolved(id string, man *Manifest) map[string]any {
	out := map[string]any{}
	if man == nil {
		return out
	}
	vals := m.stored(id, man)
	for _, s := range man.Settings {
		if v, ok := vals[s.Key]; ok {
			out[s.Key] = v
		} else if len(s.Default) > 0 {
			if d, err := ValidateSettingValue(s, s.Default); err == nil {
				out[s.Key] = d
			}
		}
	}
	return out
}

// settingText renders a value for ${setting.KEY} substitution.
func settingText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func (m *Manager) settingsView(id string, man *Manifest) SettingsView {
	vals := m.stored(id, man)
	v := SettingsView{ID: id, Settings: []SettingView{}}
	for _, s := range man.Settings {
		sv := SettingView{
			Key: s.Key, Type: s.Type, Label: s.Label, Description: s.Description, Required: s.Required,
			Secret: s.Type == "secret", Options: s.Options,
		}
		if s.Type == "string" || s.Type == "secret" {
			sv.MaxLength = settingMax(s)
		}
		val, set := vals[s.Key]
		sv.Set = set
		if len(s.Default) > 0 {
			if d, err := ValidateSettingValue(s, s.Default); err == nil {
				sv.Default = d
			}
		}
		if set && s.Type != "secret" {
			sv.Value = val
		}
		if s.Required && !set && sv.Default == nil {
			v.Missing = append(v.Missing, s.Key)
		}
		v.Settings = append(v.Settings, sv)
	}
	sort.Strings(v.Missing)
	return v
}

// Settings returns a plugin's declared settings and which are set. Secret
// values are never part of it, whoever asks.
func (m *Manager) Settings(id string) (SettingsView, error) {
	p, err := m.row(id)
	if err != nil {
		return SettingsView{}, err
	}
	man, _, err := m.loadInstalled(p)
	if err != nil {
		return SettingsView{}, err
	}
	return m.settingsView(id, man), nil
}

// PutSettings applies a patch: a key with a JSON value sets it, a null clears
// it (back to the default). It is all or nothing: one invalid key, an unknown
// key, or clearing a required setting that has no default changes nothing.
// A running process is restarted so it initializes with the new values.
func (m *Manager) PutSettings(id string, patch map[string]json.RawMessage) (SettingsView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.row(id)
	if err != nil {
		return SettingsView{}, err
	}
	man, _, err := m.loadInstalled(p)
	if err != nil {
		return SettingsView{}, err
	}
	decl := map[string]Setting{}
	for _, s := range man.Settings {
		decl[s.Key] = s
	}
	set := map[string]string{}
	var clear []string
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		raw := patch[k]
		s, ok := decl[k]
		if !ok {
			return SettingsView{}, newErr(CodeBadSetting, "plugin %q declares no setting %q", id, clip(k, 64))
		}
		if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
			if s.Required && len(s.Default) == 0 {
				return SettingsView{}, newErr(CodeBadSetting, "%s is required and has no default: it cannot be cleared", k)
			}
			clear = append(clear, k)
			continue
		}
		v, err := ValidateSettingValue(s, raw)
		if err != nil {
			return SettingsView{}, newErr(CodeBadSetting, "%s", err.Error())
		}
		enc, _ := json.Marshal(v)
		set[k] = string(enc)
	}
	if err := m.st.ApplyPluginSettings(id, set, clear); err != nil {
		return SettingsView{}, fmt.Errorf("saving settings for %q failed", id)
	}
	if m.proc != nil {
		m.syncOne(p, man, false) // the spec changed: the process is replaced and initializes with the new values
	}
	return m.settingsView(id, man), nil
}

// Setting implements SettingsProvider for ${setting.KEY} substitution when no
// provider was injected (a test). It never takes the Manager's lock.
func (m *Manager) storedSetting(plugin string, man *Manifest, key string) (string, bool) {
	v, ok := m.resolved(plugin, man)[key]
	if !ok {
		return "", false
	}
	return settingText(v), true
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}
