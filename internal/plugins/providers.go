package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/fsutil"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// PluginProviders is what an installed plugin's providers look like to the
// registry (Wave D, docs/plans/active/plugins-wave-d.md).
type PluginProviders struct {
	// Specs are the plugin's providers in manifest order.
	Specs []providers.Spec
	// Err is set when the plugin's providers could not be built (a missing or
	// unsafe command file); Specs is then empty.
	Err error
}

// ProviderSpecs returns the providers of every ACTIVE plugin (enabled,
// consented, global switch on), keyed by plugin id: derived from the
// validated manifests on every call, never stored, so they vanish with the
// plugin. A plugin whose command file is missing or unsafe appears with an
// Err and no specs.
func (m *Manager) ProviderSpecs() map[string]PluginProviders {
	out := map[string]PluginProviders{}
	for _, in := range m.Active() {
		man := m.loadedManifest(in)
		if man == nil || len(man.Contributes.Providers) == 0 {
			continue
		}
		specs, err := m.buildProviderSpecs(in.ID, man)
		out[in.ID] = PluginProviders{Specs: specs, Err: err}
	}
	return out
}

// InactiveProviders lists the providers of installed plugins that are NOT
// active (disabled, or the global switch is off), for clients that ask for
// them (provider.list includeInactive). They are never registered.
func (m *Manager) InactiveProviders() []InactiveProvider {
	l, err := m.list()
	if err != nil {
		return nil
	}
	var out []InactiveProvider
	for _, in := range l.Plugins {
		if in.Active {
			continue
		}
		p, err := m.st.GetPlugin(in.ID)
		if err != nil {
			continue
		}
		man, _, lerr := m.loadInstalled(p)
		if lerr != nil || man == nil {
			continue
		}
		state := "disabled"
		if !l.GlobalEnabled {
			state = "global_off"
		}
		for _, pc := range man.Contributes.Providers {
			out = append(out, InactiveProvider{Plugin: in.ID, ID: ProviderID(in.ID, pc.ID), Label: pc.Label, Icon: pc.Icon, State: state})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })
	return out
}

// InactiveProvider is a declared provider of a plugin that is installed but
// not active.
type InactiveProvider struct {
	Plugin string `json:"plugin"`
	ID     string `json:"id"`
	Label  string `json:"label"`
	Icon   string `json:"icon,omitempty"`
	// State is "disabled" or "global_off".
	State string `json:"state"`
}

// SyncProviders replaces reg's plugin layer with the providers of the active
// plugins and reports whether it changed. A plugin whose providers could not
// be built or registered is left out and its problem is shown on the plugin
// (lastError, its log) until the next sync that succeeds; the others are
// unaffected.
func (m *Manager) SyncProviders(reg *providers.Registry) (changed bool) {
	derived := m.ProviderSpecs()
	byPlugin := map[string][]providers.Spec{}
	for id, pp := range derived {
		if pp.Err != nil {
			m.notePlugin(id, "providers", pp.Err.Error())
			continue
		}
		byPlugin[id] = pp.Specs
	}
	before := pluginLayerSig(reg)
	problems := reg.SetPlugins(byPlugin)
	bad := map[string]bool{}
	for _, p := range problems {
		bad[p.Plugin] = true
		m.notePlugin(p.Plugin, "providers", "providers not registered: "+p.Err.Error())
	}
	for id, pp := range derived {
		if pp.Err == nil && !bad[id] {
			m.clearProblem(id, "providers")
		}
	}
	return pluginLayerSig(reg) != before
}

func pluginLayerSig(reg *providers.Registry) string {
	var plug []providers.Spec
	for _, s := range reg.List() {
		if s.Plugin != "" {
			plug = append(plug, s)
		}
	}
	b, _ := json.Marshal(plug)
	return string(b)
}

func (m *Manager) buildProviderSpecs(pluginID string, man *Manifest) ([]providers.Spec, error) {
	root := m.dir(pluginID)
	if p, err := m.st.GetPlugin(pluginID); err == nil {
		root = pluginRoot(m, p)
	}
	var out []providers.Spec
	for _, pc := range man.Contributes.Providers {
		argv := append([]string(nil), pc.ACP.Command...)
		if form, _ := commandForm(argv[0]); form == "path" {
			abs, err := confinedExecutable(root, argv[0])
			if err != nil {
				return nil, fmt.Errorf("provider %s: command %q: %w", ProviderID(pluginID, pc.ID), argv[0], err)
			}
			argv[0] = abs
		}
		s := providers.Spec{
			ID: ProviderID(pluginID, pc.ID), Label: pc.Label, Plugin: pluginID, Icon: pc.Icon,
			Runner: providers.RunnerACP, Command: argv, HealthCommand: []string{argv[0]},
			Vendor: pc.Vendor, DefaultMode: pc.DefaultMode,
			Health: providers.Health{Install: pc.Health.Install, Login: pc.Health.Login, VersionArg: pc.Health.VersionArg, MinVersion: pc.Health.MinVersion},
		}
		if pc.ACP.SupportsMcpServers != nil && !*pc.ACP.SupportsMcpServers {
			s.NoMCPServers = true
		}
		if len(pc.ACP.Env) > 0 {
			s.Env = map[string]string{}
			for k, v := range pc.ACP.Env {
				s.Env[k] = v
			}
		}
		for _, sl := range pc.ACP.SecretEnv {
			if s.SecretEnv == nil {
				s.SecretEnv = map[string]string{}
			}
			s.SecretEnv[sl.Name] = sl.Name
			s.SecretSlots = append(s.SecretSlots, providers.SecretSlot{Name: sl.Name, Label: sl.Label, Required: sl.Required})
		}
		for _, md := range pc.Models {
			s.Models = append(s.Models, providers.Model{ID: md.ID, Label: orDefault(md.Label, md.ID), Description: md.Description, Default: md.Default})
		}
		for _, md := range pc.Modes {
			s.Modes = append(s.Modes, providers.Mode{ID: md.ID, Label: orDefault(md.Label, md.ID), Description: md.Description, AutoApproves: md.AutoApproves})
		}
		out = append(out, s)
	}
	return out, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func pluginRoot(m *Manager, p store.Plugin) string {
	if p.SourceKind == string(SourceLink) {
		return p.Source
	}
	return m.dir(p.ID)
}

// confinedExecutable resolves rel under root and checks the file: inside the
// plugin after symlinks, a regular file, and executable (the bit is not
// checked on Windows). A symlink anywhere in the path is refused.
func confinedExecutable(root, rel string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("plugin directory: %w", err)
	}
	full := filepath.Join(realRoot, filepath.FromSlash(rel))
	cur := realRoot
	for _, seg := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", fmt.Errorf("not found in the plugin")
		}
		if fsutil.IsLink(fi.Mode()) {
			return "", fmt.Errorf("a symlink in the path is refused")
		}
	}
	fi, err := os.Stat(full)
	if err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("is not a regular file")
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("is not executable (chmod +x)")
	}
	return full, nil
}
