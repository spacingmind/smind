// Package plugins is the host side of smind's plugin system (ADR-0042): the
// manifest, the on-disk layout under $SMIND_HOME/plugins, staged installs from
// the four allowed sources, and the install state kept in SQLite.
//
// A plugin runs trusted local code later (Wave B), so this package is the trust
// boundary for how a plugin gets onto the machine: sources are given
// explicitly by the Human, pinned (a commit or a sha256), unpacked under hard
// limits, validated, and only then moved into place. Nothing here ever reads a
// workspace's repository as a plugin source (ADR-0024).
package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ManifestFile is the manifest's name at the plugin root.
const ManifestFile = "smind-plugin.json"

// MaxManifestBytes caps the manifest; a larger file is rejected before parsing.
const MaxManifestBytes = 256 << 10

// SupportedAPIs is the set of plugin API majors this daemon loads.
var SupportedAPIs = []int{1}

// KnownCapabilities are the host-API groups a plugin may declare (ADR-0042
// section 4). The list is closed on purpose: a misspelled capability must not
// silently grant or skip anything.
var KnownCapabilities = []string{
	"events.observe", "files.read", "hooks.beforeRun", "mcp.read",
	"profiles.read", "tasks.read", "tasks.write", "ui.notify",
}

// KnownHookEvents are the hook events a manifest may name.
var KnownHookEvents = []string{"run.beforeStart", "run.finished"}

var (
	idRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	settingRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	driveRe   = regexp.MustCompile(`^[A-Za-z]:`)
)

// windowsReserved are device names Windows refuses as file names; an id or an
// entry named like one cannot be created there.
var windowsReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// Manifest is the parsed smind-plugin.json. Unknown top-level keys are
// ignored (forward compatibility); unknown keys inside engines are rejected.
type Manifest struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Version      string      `json:"version"`
	API          int         `json:"api"`
	Engines      Engines     `json:"engines"`
	Description  string      `json:"description,omitempty"`
	Process      *Process    `json:"process,omitempty"`
	Capabilities []string    `json:"capabilities,omitempty"`
	Settings     []Setting   `json:"settings,omitempty"`
	Contributes  Contributes `json:"contributes"`
	raw          []byte      // the bytes parsed, for hashing a linked source

	// Loaded by loadContributions once the plugin's files are at hand.
	skills   []SkillSpec
	profiles []ProfileSpec
	prompts  map[string]string // command name -> prompt text
	themes   []ThemeSpec
}

// Engines holds the version gates besides api.
type Engines struct {
	Smind string `json:"smind"`
}

// Process declares the long-lived plugin process (Wave B runs it).
type Process struct {
	Command []string `json:"command"`
}

// Setting is one declared plugin setting (ADR-0042 PB.2). Type is string,
// secret, boolean, number or enum. A secret is write-only over every RPC, never
// has a default (a manifest is not a place for one) and reaches only the
// plugin's own initialize and ${setting.KEY} in MCP env and headers.
type Setting struct {
	Key         string          `json:"key"`
	Type        string          `json:"type"`
	Label       string          `json:"label"`
	Description string          `json:"description,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	// Options are the allowed values of an enum.
	Options []string `json:"options,omitempty"`
	// MaxLength caps a string or secret in bytes (default MaxSettingString,
	// at most MaxSettingStringCap).
	MaxLength int `json:"maxLength,omitempty"`
}

// Setting value limits.
const (
	MaxSettingString    = 1024
	MaxSettingStringCap = 8192
	MaxSettingOptions   = 64
)

// Contributes lists what a plugin adds. Wave A consumes the data kinds; the
// manifest accepts the later waves' keys so a plugin is written once.
type Contributes struct {
	McpServers []McpContribution      `json:"mcpServers,omitempty"`
	Skills     []string               `json:"skills,omitempty"`
	Profiles   []string               `json:"profiles,omitempty"`
	Commands   []CommandContribution  `json:"commands,omitempty"`
	Themes     []ThemeContribution    `json:"themes,omitempty"`
	Hooks      []HookContribution     `json:"hooks,omitempty"`
	RPC        []RPCContribution      `json:"rpc,omitempty"`
	Providers  []ProviderContribution `json:"providers,omitempty"`
	Panels     []json.RawMessage      `json:"panels,omitempty"`
	Sidebar    []json.RawMessage      `json:"sidebar,omitempty"`
}

// McpContribution is an MCP server a plugin contributes.
type McpContribution struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Command   []string `json:"command,omitempty"`
	URL       string   `json:"url,omitempty"`
	// Env (stdio) and Headers (http, sse) values may hold ${setting.KEY}
	// placeholders, substituted from the plugin's settings when a run starts.
	// Placeholders are accepted nowhere else.
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// CommandContribution is a slash command: a prompt file or an RPC.
type CommandContribution struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	RPC         string `json:"rpc,omitempty"`
}

// RPCContribution declares a method the plugin process answers as
// `rpc.<name>` for plugin.call (ADR-0042 section 4). Only declared names are
// ever forwarded to the process.
type RPCContribution struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// TimeoutMs lowers the per-call timeout (default and hard max: MaxCallTimeout).
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// ThemeContribution is a theme token file.
type ThemeContribution struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	File  string `json:"file"`
}

// HookContribution subscribes the plugin process to a host event.
type HookContribution struct {
	Event   string `json:"event"`
	OnError string `json:"onError,omitempty"`
}

// ProviderContribution is an ACP provider (Wave D).
type ProviderContribution struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	ACP   struct {
		Command []string `json:"command"`
	} `json:"acp"`
}

// FieldError is one manifest problem: the field, what is wrong and the fix.
type FieldError struct {
	Field, Problem, Fix string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("manifest field %s: %s (fix: %s)", e.Field, e.Problem, e.Fix)
}

// ManifestError carries every problem found, in field order.
type ManifestError struct{ Errs []*FieldError }

func (e *ManifestError) Error() string {
	parts := make([]string, len(e.Errs))
	for i, fe := range e.Errs {
		parts[i] = fe.Error()
	}
	return strings.Join(parts, "; ")
}

// Fields returns the names of the offending fields.
func (e *ManifestError) Fields() []string {
	out := make([]string, len(e.Errs))
	for i, fe := range e.Errs {
		out[i] = fe.Field
	}
	return out
}

type manifestVal struct{ errs []*FieldError }

func (v *manifestVal) bad(field, problem, fix string) {
	v.errs = append(v.errs, &FieldError{Field: field, Problem: problem, Fix: fix})
}

// ParseManifest parses and validates a manifest against the running daemon
// version ("dev" and unparseable build versions skip the engines check, so a
// development build can load what it builds). Every problem is reported with
// its field and the fix.
func ParseManifest(data []byte, daemonVersion string) (*Manifest, error) {
	v := &manifestVal{}
	if len(data) > MaxManifestBytes {
		v.bad("(file)", fmt.Sprintf("%s is %d bytes, over the %d byte cap", ManifestFile, len(data), MaxManifestBytes), "shrink the manifest; move bulk data into files it references")
		return nil, &ManifestError{Errs: v.errs}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		v.bad("(file)", "not a JSON object: "+err.Error(), "write "+ManifestFile+" as one JSON object")
		return nil, &ManifestError{Errs: v.errs}
	}
	m := &Manifest{raw: append([]byte(nil), data...)}

	get := func(key string, dst any, required bool, fix string) bool {
		raw, ok := top[key]
		if !ok {
			if required {
				v.bad(key, "is required", fix)
			}
			return false
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			v.bad(key, "has the wrong type ("+shortErr(err)+")", fix)
			return false
		}
		return true
	}

	if get("id", &m.ID, true, "set id to a lowercase name like \"acme-review\"") && !validID(m.ID) {
		v.bad("id", fmt.Sprintf("%q is not a valid plugin id", m.ID), "use 1-64 characters from a-z, 0-9 and -, starting with a letter, and not a Windows device name")
	}
	if get("name", &m.Name, true, "set name to a short display name") {
		if strings.TrimSpace(m.Name) == "" || len(m.Name) > 100 || strings.ContainsRune(m.Name, 0) {
			v.bad("name", "must be 1-100 characters", "shorten name")
		}
	}
	get("description", &m.Description, false, "make description a string")
	if len(m.Description) > 500 {
		v.bad("description", "is over 500 characters", "shorten description")
	}
	if get("version", &m.Version, true, "set version to MAJOR.MINOR.PATCH, for example \"1.2.0\"") {
		if _, err := parseSemver(m.Version); err != nil {
			v.bad("version", err.Error(), "use semantic versioning, for example \"1.2.0\" or \"1.2.0-rc.1\"")
		}
	}

	if get("api", &m.API, true, fmt.Sprintf("set api to one of %v", SupportedAPIs)) && !apiSupported(m.API) {
		v.bad("api", fmt.Sprintf("%d is not supported by this daemon (supports %v)", m.API, SupportedAPIs), "target a supported plugin API or upgrade smind")
	}

	if raw, ok := top["engines"]; !ok {
		v.bad("engines", "is required", `add "engines": {"smind": ">=0.15.0"}`)
	} else {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m.Engines); err != nil {
			v.bad("engines", shortErr(err), `engines only accepts "smind"; remove or correct the key`)
		} else if rng, err := parseRange(m.Engines.Smind); err != nil {
			v.bad("engines.smind", err.Error(), `use a semver range such as ">=0.15.0" or "^0.15.0"`)
		} else if dv, perr := parseSemver(daemonVersion); perr == nil && !rng.satisfies(dv) {
			v.bad("engines.smind", fmt.Sprintf("%q is not satisfied by smind %s", m.Engines.Smind, daemonVersion), "upgrade smind, or install a plugin version that supports it")
		}
	}

	if get("capabilities", &m.Capabilities, false, "make capabilities an array of capability names") {
		seen := map[string]bool{}
		for i, c := range m.Capabilities {
			f := fmt.Sprintf("capabilities[%d]", i)
			switch {
			case !contains(KnownCapabilities, c):
				v.bad(f, fmt.Sprintf("unknown capability %q", c), "use one of: "+strings.Join(KnownCapabilities, ", "))
			case seen[c]:
				v.bad(f, fmt.Sprintf("%q is listed twice", c), "remove the duplicate")
			}
			seen[c] = true
		}
	}

	if get("process", &m.Process, false, `make process {"command": ["node", "server.js"]}`) && m.Process != nil {
		validateCommand(v, "process.command", m.Process.Command)
	}
	if get("settings", &m.Settings, false, "make settings an array of {key,type,label}") {
		seen := map[string]bool{}
		for i, s := range m.Settings {
			f := fmt.Sprintf("settings[%d]", i)
			if !settingRe.MatchString(s.Key) || seen[s.Key] {
				v.bad(f+".key", fmt.Sprintf("%q is not a unique setting key", s.Key), "use letters, digits and _ starting with a letter, once per plugin")
			}
			seen[s.Key] = true
			if !contains([]string{"string", "secret", "boolean", "number", "enum"}, s.Type) {
				v.bad(f+".type", fmt.Sprintf("unknown setting type %q", s.Type), "use string, secret, boolean, number or enum")
				continue
			}
			validateSettingDecl(v, f, s)
		}
	}
	if get("contributes", &m.Contributes, false, "make contributes an object of contribution lists") {
		validateContributes(v, m)
	}

	if len(v.errs) > 0 {
		return nil, &ManifestError{Errs: v.errs}
	}
	return m, nil
}

func validateContributes(v *manifestVal, m *Manifest) {
	c := m.Contributes
	for i, s := range c.McpServers {
		f := fmt.Sprintf("contributes.mcpServers[%d]", i)
		checkName(v, f+".name", s.Name)
		switch s.Transport {
		case "stdio":
			validateCommand(v, f+".command", s.Command)
		case "http", "sse":
			if !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://") {
				v.bad(f+".url", "must be an http(s) URL for transport "+s.Transport, "set url to the server address")
			}
		default:
			v.bad(f+".transport", fmt.Sprintf("unknown transport %q", s.Transport), "use stdio, http or sse")
		}
		validateMcpSettings(v, f, s, m.Settings)
	}
	for i, p := range c.Skills {
		checkPath(v, fmt.Sprintf("contributes.skills[%d]", i), p)
	}
	for i, p := range c.Profiles {
		checkPath(v, fmt.Sprintf("contributes.profiles[%d]", i), p)
	}
	for i, cmd := range c.Commands {
		f := fmt.Sprintf("contributes.commands[%d]", i)
		checkName(v, f+".name", cmd.Name)
		if (cmd.Prompt == "") == (cmd.RPC == "") {
			v.bad(f, "must set exactly one of prompt and rpc", "give the command a prompt file or an rpc name, not both")
		}
		if cmd.Prompt != "" {
			checkPath(v, f+".prompt", cmd.Prompt)
		}
		if cmd.RPC != "" && m.Process == nil {
			v.bad(f+".rpc", "an rpc command needs a plugin process", "declare process or use a prompt command")
		}
		if cmd.RPC != "" && m.Process != nil && !declaresRPC(c.RPC, cmd.RPC) {
			v.bad(f+".rpc", fmt.Sprintf("%q is not declared in contributes.rpc", cmd.RPC), "add {\"name\": \""+cmd.RPC+"\"} to contributes.rpc")
		}
	}
	seenRPC := map[string]bool{}
	for i, r := range c.RPC {
		f := fmt.Sprintf("contributes.rpc[%d]", i)
		checkName(v, f+".name", r.Name)
		if seenRPC[r.Name] {
			v.bad(f+".name", fmt.Sprintf("duplicate rpc %q", r.Name), "declare each rpc once")
		}
		seenRPC[r.Name] = true
		if r.TimeoutMs < 0 || r.TimeoutMs > int(MaxCallTimeout/time.Millisecond) {
			v.bad(f+".timeoutMs", fmt.Sprintf("%d is out of range", r.TimeoutMs), fmt.Sprintf("use 1 to %d milliseconds, or omit it for the default", MaxCallTimeout/time.Millisecond))
		}
		if m.Process == nil {
			v.bad(f, "an rpc needs a plugin process", "declare process or remove the rpc")
		}
	}
	for i, t := range c.Themes {
		f := fmt.Sprintf("contributes.themes[%d]", i)
		checkName(v, f+".id", t.ID)
		checkPath(v, f+".file", t.File)
	}
	for i, h := range c.Hooks {
		f := fmt.Sprintf("contributes.hooks[%d]", i)
		if !contains(KnownHookEvents, h.Event) {
			v.bad(f+".event", fmt.Sprintf("unknown hook event %q", h.Event), "use one of: "+strings.Join(KnownHookEvents, ", "))
		}
		if h.OnError != "" && h.OnError != "open" && h.OnError != "closed" {
			v.bad(f+".onError", fmt.Sprintf("%q is not open or closed", h.OnError), "use \"open\" or \"closed\"")
		}
		if m.Process == nil {
			v.bad(f, "a hook needs a plugin process", "declare process or remove the hook")
		}
	}
	for i, p := range c.Providers {
		f := fmt.Sprintf("contributes.providers[%d]", i)
		checkName(v, f+".id", p.ID)
		validateCommand(v, f+".acp.command", p.ACP.Command)
	}
}

// settingRefRe matches a ${setting.KEY} placeholder.
var settingRefRe = regexp.MustCompile(`\$\{setting\.([A-Za-z][A-Za-z0-9_]{0,63})\}`)

// settingRefs returns the setting keys referenced by s.
func settingRefs(s string) []string {
	var out []string
	for _, m := range settingRefRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// validateMcpSettings checks an MCP contribution's env and headers: they fit
// the transport, placeholders name declared settings, and no placeholder sits
// anywhere else (a secret in a command line or URL would leak through argv,
// process listings and logs).
func validateMcpSettings(v *manifestVal, f string, s McpContribution, settings []Setting) {
	declared := map[string]bool{}
	for _, st := range settings {
		declared[st.Key] = true
	}
	for i, a := range s.Command {
		if strings.Contains(a, "${setting.") {
			v.bad(fmt.Sprintf("%s.command[%d]", f, i), "a setting placeholder is only allowed in env and headers", "move the value into env (stdio) or headers (http, sse)")
		}
	}
	if strings.Contains(s.URL, "${setting.") {
		v.bad(f+".url", "a setting placeholder is only allowed in env and headers", "move the value into headers")
	}
	check := func(field string, m map[string]string) {
		for k, val := range m {
			if k == "" || len(k) > 128 || strings.ContainsAny(k, "\x00=") || len(val) > 4096 || strings.ContainsRune(val, 0) {
				v.bad(field, fmt.Sprintf("entry %q is not a valid name and value", k), "use a non-empty name without = and a value under 4096 characters")
				continue
			}
			for _, ref := range settingRefs(val) {
				if !declared[ref] {
					v.bad(field+"."+k, fmt.Sprintf("refers to setting %q, which is not declared", ref), "declare it in settings or remove the placeholder")
				}
			}
			if strings.Contains(settingRefRe.ReplaceAllString(val, ""), "${setting.") {
				v.bad(field+"."+k, "has a malformed ${setting.KEY} placeholder", "use ${setting.NAME} with a declared setting key")
			}
		}
	}
	switch s.Transport {
	case "stdio":
		if len(s.Headers) > 0 {
			v.bad(f+".headers", "a stdio server has no headers", "use env, or switch to http or sse")
		}
	case "http", "sse":
		if len(s.Env) > 0 {
			v.bad(f+".env", "an http or sse server has no env", "use headers, or switch to stdio")
		}
	}
	check(f+".env", s.Env)
	check(f+".headers", s.Headers)
}

func validateCommand(v *manifestVal, field string, cmd []string) {
	if len(cmd) == 0 || len(cmd) > 32 {
		v.bad(field, "must be a non-empty argv array of at most 32 entries", `write it as ["program", "arg", ...]`)
		return
	}
	for i, a := range cmd {
		if (i == 0 && strings.TrimSpace(a) == "") || strings.ContainsRune(a, 0) || len(a) > 1024 {
			v.bad(fmt.Sprintf("%s[%d]", field, i), "is empty, too long or contains a NUL", "give each argument as a plain string")
		}
	}
}

func declaresRPC(rs []RPCContribution, name string) bool {
	for _, r := range rs {
		if r.Name == name {
			return true
		}
	}
	return false
}

func checkName(v *manifestVal, field, name string) {
	if !nameRe.MatchString(name) {
		v.bad(field, fmt.Sprintf("%q is not a valid name", name), "use 1-64 letters, digits, . _ - starting with a letter or digit")
	}
}

func checkPath(v *manifestVal, field, p string) {
	if err := ConfinedPath(p); err != nil {
		v.bad(field, err.Error(), "use a relative, forward-slashed path inside the plugin, without \"..\"")
	}
}

// ConfinedPath checks that p is a relative, forward-slashed path that stays
// inside the plugin directory and does not name the reserved data/ directory.
func ConfinedPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("path is empty")
	case len(p) > 255:
		return fmt.Errorf("path is over 255 characters")
	case strings.ContainsRune(p, 0):
		return fmt.Errorf("path contains a NUL")
	case strings.Contains(p, `\`):
		return fmt.Errorf("path %q contains a backslash", p)
	case strings.HasPrefix(p, "/") || driveRe.MatchString(p):
		return fmt.Errorf("path %q is absolute", p)
	}
	segs := strings.Split(p, "/")
	for _, s := range segs {
		if s == "" || s == "." || s == ".." || strings.Contains(s, ":") {
			return fmt.Errorf("path %q escapes or is not clean", p)
		}
	}
	if segs[0] == "data" {
		return fmt.Errorf("path %q is inside the reserved data/ directory", p)
	}
	return nil
}

func validID(id string) bool { return idRe.MatchString(id) && !windowsReserved[id] }

func apiSupported(api int) bool {
	for _, a := range SupportedAPIs {
		if a == api {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// DeclaredPaths returns every manifest path that must exist in the plugin.
func (m *Manifest) DeclaredPaths() []string {
	var out []string
	out = append(out, m.Contributes.Skills...)
	out = append(out, m.Contributes.Profiles...)
	for _, c := range m.Contributes.Commands {
		if c.Prompt != "" {
			out = append(out, c.Prompt)
		}
	}
	for _, t := range m.Contributes.Themes {
		out = append(out, t.File)
	}
	return out
}

// validateSettingDecl checks the type-specific parts of a setting declaration:
// enum options, the size cap, and that a default fits its type. A secret may
// not carry a default.
func validateSettingDecl(v *manifestVal, f string, s Setting) {
	if s.Type == "enum" {
		switch {
		case len(s.Options) == 0 || len(s.Options) > MaxSettingOptions:
			v.bad(f+".options", "an enum needs 1 to 64 options", "list the allowed values")
		default:
			seen := map[string]bool{}
			for _, o := range s.Options {
				if o == "" || len(o) > 128 || seen[o] {
					v.bad(f+".options", fmt.Sprintf("option %q is empty, over 128 bytes or repeated", o), "use distinct, short options")
					break
				}
				seen[o] = true
			}
		}
	} else if len(s.Options) > 0 {
		v.bad(f+".options", "options belong to an enum", "remove options or make the type enum")
	}
	if s.MaxLength != 0 && (s.Type != "string" && s.Type != "secret" || s.MaxLength < 1 || s.MaxLength > MaxSettingStringCap) {
		v.bad(f+".maxLength", "maxLength applies to string and secret, from 1 to 8192", "remove it or use a size in range")
	}
	if len(s.Default) == 0 {
		return
	}
	if s.Type == "secret" {
		v.bad(f+".default", "a secret cannot have a default", "remove it: the Human sets a secret, the manifest never carries one")
		return
	}
	if _, err := ValidateSettingValue(s, s.Default); err != nil {
		v.bad(f+".default", err.Error(), "use a value that fits the declared type")
	}
}
