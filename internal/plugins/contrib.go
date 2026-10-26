package plugins

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/store"
)

// SettingsProvider hands a plugin's setting values to the host (ADR-0042
// section 6). Plugin settings storage arrives with PB.2; until then the
// substitution mechanism below is complete and the daemon wires no provider,
// so a placeholder with no provider leaves its server out of runs.
type SettingsProvider interface {
	// Setting returns the value of one declared setting of a plugin.
	Setting(pluginID, key string) (value string, ok bool)
}

// Conflict is a contribution the host did not apply and why: a user row that
// owns the name, a setting that is not set, a profile that failed validation.
type Conflict struct {
	Plugin string `json:"plugin"`
	Kind   string `json:"kind"` // mcpServer | skill | profile | command
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Skill is one skill of an active plugin.
type Skill struct {
	Plugin      string `json:"plugin"`
	Name        string `json:"name"`
	Qualified   string `json:"qualified"` // "<plugin>:<name>"
	Description string `json:"description,omitempty"`
	// Dir is the directory name the projection gives it: "<plugin>--<name>".
	Dir   string            `json:"-"`
	Files map[string][]byte `json:"-"`
}

// contribution is the full derived view of what every active plugin adds.
type contribution struct {
	mcp       []store.McpServer // read form
	mcpRun    []store.McpServer // run form (secrets substituted)
	skills    []Skill
	profiles  []store.AgentProfile
	commands  []Command
	themes    []Theme
	conflicts []Conflict
}

// Command is one prompt slash command of an active plugin. It carries text
// only: selecting it inserts Prompt into the composer and never sends it.
type Command struct {
	Plugin      string `json:"plugin"`
	Name        string `json:"name"`
	Qualified   string `json:"qualified"` // "<plugin>:<name>", always unique
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt"`
	// Alias is the bare name this command may also be typed as ("/name"),
	// set only for the first plugin by id that declares that name. Clients
	// drop it when a built-in command has the same name (built-ins win).
	Alias string `json:"alias,omitempty"`
}

// Theme is one theme of an active plugin: a complete token set per declared
// scheme.
type Theme struct {
	Plugin    string                       `json:"plugin"`
	ID        string                       `json:"id"`
	Qualified string                       `json:"qualified"` // "<plugin>:<id>"
	Label     string                       `json:"label"`
	Schemes   map[string]map[string]string `json:"schemes"`
}

type cachedPlugin struct {
	key string
	man *Manifest
}

type contribCache struct {
	mu sync.Mutex
	m  map[string]cachedPlugin
}

// loadedManifest returns the validated manifest of an active plugin, re-read
// from disk only when the plugin's row changed (any enable, update or reload
// rewrites it). A plugin whose files no longer validate contributes nothing.
func (m *Manager) loadedManifest(in Info) *Manifest {
	key := fmt.Sprintf("%s|%s|%d", in.Hash, in.Version, in.UpdatedAt.UnixNano())
	m.cache.mu.Lock()
	defer m.cache.mu.Unlock()
	if c, ok := m.cache.m[in.ID]; ok && c.key == key {
		return c.man
	}
	if m.cache.m == nil {
		m.cache.m = map[string]cachedPlugin{}
	}
	var man *Manifest
	if p, err := m.st.GetPlugin(in.ID); err == nil {
		var lerr error
		if man, _, lerr = m.loadInstalled(p); lerr != nil {
			man = nil
			slog.Warn("plugins: contributions not loaded", "plugin", in.ID, "err", lerr)
		}
	}
	m.cache.m[in.ID] = cachedPlugin{key: key, man: man}
	return man
}

// ownedID derives a stable id in the owned range from the row's identity,
// probing past an id another owned row already took.
func ownedID(kind, plugin, name string, taken map[int64]bool) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind + "\x00" + plugin + "\x00" + name))
	id := store.OwnedIDBase + int64(h.Sum64()%uint64(store.OwnedIDSpan))
	for taken[id] {
		id++
		if id >= store.OwnedIDBase+store.OwnedIDSpan {
			id = store.OwnedIDBase
		}
	}
	taken[id] = true
	return id
}

// wireName is the name a run sees an owned MCP server under: the namespaced
// "<plugin>:<name>" does not survive every protocol's key rules (tool names,
// TOML keys), so the colon becomes "__".
func wireName(plugin, name string) string {
	var b strings.Builder
	b.WriteString(plugin)
	b.WriteString("__")
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func jsonStrings(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonMap(v map[string]string) string {
	if v == nil {
		v = map[string]string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// substitute replaces ${setting.KEY} in every value of vals from the plugin's
// settings: an injected provider (tests) wins, else the stored values and
// defaults of man's declarations. It returns the keys with no value; the
// result must reach only a run's env and headers, never a read path.
func (m *Manager) substitute(plugin string, man *Manifest, vals map[string]string) (map[string]string, []string) {
	if len(vals) == 0 {
		return vals, nil
	}
	out := make(map[string]string, len(vals))
	var missing []string
	var stored map[string]any // read at most once per call
	for k, v := range vals {
		out[k] = settingRefRe.ReplaceAllStringFunc(v, func(ref string) string {
			key := settingRefRe.FindStringSubmatch(ref)[1]
			if sp := m.settingsProvider(); sp != nil {
				if val, ok := sp.Setting(plugin, key); ok {
					return val
				}
			} else {
				if stored == nil {
					stored = m.resolved(plugin, man)
				}
				if val, ok := stored[key]; ok {
					return settingText(val)
				}
			}
			missing = append(missing, key)
			return ""
		})
	}
	sort.Strings(missing)
	return out, missing
}

func (m *Manager) settingsProvider() SettingsProvider {
	m.cache.mu.Lock()
	defer m.cache.mu.Unlock()
	return m.settings
}

// derive computes what every active plugin contributes right now. It is
// cheap (manifests are cached) and always follows the live plugin state and
// the global switch, which is how a disable, a remove or a switch flip takes
// effect without a restart.
func (m *Manager) derive() contribution {
	var c contribution
	infos := m.Active()
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	userProfiles := map[string]bool{}
	if m.userProfileNames != nil {
		for _, n := range m.userProfileNames() {
			userProfiles[n] = true
		}
	}
	takenMcp, takenProf := map[int64]bool{}, map[int64]bool{}
	bareOwner := map[string]string{}
	for _, in := range infos {
		man := m.loadedManifest(in)
		if man == nil {
			continue
		}
		owner := in.ID
		srvs := append([]McpContribution(nil), man.Contributes.McpServers...)
		sort.Slice(srvs, func(i, j int) bool { return srvs[i].Name < srvs[j].Name })
		for _, s := range srvs {
			id := ownedID("mcp", owner, s.Name, takenMcp)
			row := store.McpServer{
				ID: id, Name: owner + ":" + s.Name, Transport: s.Transport, URL: s.URL, Enabled: true,
				Args: "[]", Env: "{}", Headers: "{}", OwnerPlugin: owner, CreatedAt: in.InstalledAt, UpdatedAt: in.UpdatedAt,
			}
			if s.Transport == "stdio" && len(s.Command) > 0 {
				row.Command, row.Args = s.Command[0], jsonStrings(s.Command[1:])
				row.Env = jsonMap(s.Env)
			} else {
				row.Headers = jsonMap(s.Headers)
			}
			c.mcp = append(c.mcp, row)

			run := row
			run.Name = wireName(owner, s.Name)
			env, miss1 := m.substitute(owner, man, s.Env)
			hdr, miss2 := m.substitute(owner, man, s.Headers)
			if miss := append(miss1, miss2...); len(miss) > 0 {
				c.conflicts = append(c.conflicts, Conflict{Plugin: owner, Kind: "mcpServer", Name: s.Name, Reason: "not injected: setting " + strings.Join(miss, ", ") + " has no value"})
				continue
			}
			if s.Transport == "stdio" {
				run.Env = jsonMap(env)
			} else {
				run.Headers = jsonMap(hdr)
			}
			c.mcpRun = append(c.mcpRun, run)
		}

		seenDir := map[string]bool{}
		for _, sk := range man.skills {
			dir := owner + "--" + sk.Name
			if seenDir[dir] {
				continue
			}
			seenDir[dir] = true
			c.skills = append(c.skills, Skill{Plugin: owner, Name: sk.Name, Qualified: owner + ":" + sk.Name, Description: sk.Description, Dir: dir, Files: sk.Files})
		}

		for _, cc := range man.Contributes.Commands {
			text, ok := man.prompts[cc.Name]
			if !ok {
				continue // an rpc command (PB.4): not a prompt, not listed here
			}
			cmd := Command{Plugin: owner, Name: cc.Name, Qualified: owner + ":" + cc.Name, Description: cc.Description, Prompt: text}
			if first, taken := bareOwner[cc.Name]; taken {
				c.conflicts = append(c.conflicts, Conflict{Plugin: owner, Kind: "command", Name: cc.Name, Reason: fmt.Sprintf("/%s belongs to plugin %q (first by id); use /%s", cc.Name, first, cmd.Qualified)})
			} else {
				bareOwner[cc.Name] = owner
				cmd.Alias = cc.Name
			}
			c.commands = append(c.commands, cmd)
		}
		for _, ts := range man.themes {
			c.themes = append(c.themes, Theme{Plugin: owner, ID: ts.ID, Qualified: owner + ":" + ts.ID, Label: ts.Label, Schemes: ts.Schemes})
		}

		ids := map[string]int64{}
		for _, ps := range man.profiles {
			ids[ps.Name] = ownedID("profile", owner, ps.Name, takenProf)
		}
		for _, ps := range man.profiles {
			name := owner + ":" + ps.Name
			if userProfiles[name] {
				c.conflicts = append(c.conflicts, Conflict{Plugin: owner, Kind: "profile", Name: ps.Name, Reason: fmt.Sprintf("hidden: a profile named %q already exists", name)})
				continue
			}
			p, err := m.profileFor(owner, in, ps, ids)
			if err != nil {
				c.conflicts = append(c.conflicts, Conflict{Plugin: owner, Kind: "profile", Name: ps.Name, Reason: "not available: " + err.Error()})
				continue
			}
			c.profiles = append(c.profiles, p)
		}
	}
	return c
}

// profileFor builds the owned profile row of one contributed profile.
func (m *Manager) profileFor(owner string, in Info, ps ProfileSpec, ids map[string]int64) (store.AgentProfile, error) {
	ow := owner
	p := store.AgentProfile{
		ID: ids[ps.Name], Name: owner + ":" + ps.Name, Provider: ps.Provider,
		PermissionMode: ps.PermissionMode, AutoAccept: ps.AutoAccept, ThinkingLevel: ps.ThinkingLevel,
		Model: ps.Model, Notes: ps.Notes, OnRateLimit: ps.OnRateLimit, OrchestrationTools: ps.OrchestrationTools,
		Instructions: ps.Instructions, Skills: append([]string(nil), ps.Skills...), ChildPolicy: ps.ChildPolicy,
		CreatedAt: in.InstalledAt, UpdatedAt: in.UpdatedAt, Owner: &ow, ReadOnly: true,
	}
	if p.OnRateLimit == "" {
		p.OnRateLimit = "none"
	}
	for _, c := range ps.ChildProfiles {
		p.ChildProfileIDs = append(p.ChildProfileIDs, ids[c])
	}
	if p.OrchestrationTools != nil && !*p.OrchestrationTools && (p.ChildPolicy == "leaf" || p.ChildPolicy == "profiles") {
		return store.AgentProfile{}, fmt.Errorf("a profile without orchestration tools cannot launch children")
	}
	// Same normalization as a user profile: a seat contract with no explicit
	// childPolicy launches nothing.
	if p.ChildPolicy == "" && (p.OrchestrationTools != nil || p.Instructions != "" || len(p.Skills) > 0) {
		p.ChildPolicy = "none"
	}
	if m.validateProfile != nil {
		if err := m.validateProfile(p); err != nil {
			return store.AgentProfile{}, err
		}
	}
	return p, nil
}

// SetSettings installs the settings provider (plugin settings arrive with
// PB.2; until then only a test wires one, so a placeholder never resolves in
// a real daemon). Safe to call while the manager serves requests.
func (m *Manager) SetSettings(p SettingsProvider) {
	m.cache.mu.Lock()
	m.settings = p
	m.cache.mu.Unlock()
}

// OwnedProfile implements store.Overlay.
func (m *Manager) OwnedProfile(id int64) (store.AgentProfile, bool) {
	for _, p := range m.derive().profiles {
		if p.ID == id {
			return p, true
		}
	}
	return store.AgentProfile{}, false
}

// OwnedProfiles implements store.Overlay.
func (m *Manager) OwnedProfiles() []store.AgentProfile { return m.derive().profiles }

// OwnedMcp implements store.Overlay.
func (m *Manager) OwnedMcp() []store.McpServer { return m.derive().mcp }

// OwnedMcpForRun implements store.Overlay.
func (m *Manager) OwnedMcpForRun() []store.McpServer { return m.derive().mcpRun }

var _ store.Overlay = (*Manager)(nil)

// Skills lists the skills of every active plugin, by plugin id then name.
func (m *Manager) Skills() []Skill { return m.derive().skills }

// PluginSkill resolves a "<plugin>:<name>" reference against the ACTIVE
// plugins; a disabled, removed or switched-off plugin's skills do not resolve.
func (m *Manager) PluginSkill(qualified string) (Skill, bool) {
	for _, s := range m.derive().skills {
		if s.Qualified == qualified {
			return s, true
		}
	}
	return Skill{}, false
}

// Commands lists the prompt slash commands of every active plugin, by plugin
// id then declaration order. Empty when the global switch is off.
func (m *Manager) Commands() []Command { return m.derive().commands }

// Themes lists the themes of every active plugin, by plugin id then
// declaration order. Empty when the global switch is off.
func (m *Manager) Themes() []Theme { return m.derive().themes }

// Conflicts lists what active plugins contributed but the host did not apply.
func (m *Manager) Conflicts() []Conflict { return m.derive().conflicts }

// Resolver adapts a Manager to the profile registry's skill resolution: it
// answers for ACTIVE plugins only, so a profile may name a skill whose plugin
// is later disabled and the profile still loads.
type Resolver struct{ M *Manager }

// PluginSkill reports whether an active plugin has the "<plugin>:<skill>".
func (r Resolver) PluginSkill(qualified string) (string, bool) {
	s, ok := r.M.PluginSkill(qualified)
	return s.Plugin, ok
}

// PluginActive reports whether the plugin is installed, enabled and the
// global switch is on.
func (r Resolver) PluginActive(id string) bool {
	for _, in := range r.M.Active() {
		if in.ID == id {
			return true
		}
	}
	return false
}
