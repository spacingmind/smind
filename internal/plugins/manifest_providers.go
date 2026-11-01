package plugins

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/spacingmind/smind/internal/providers"
)

// Wave D: a plugin contributes ACP providers, ids "<plugin>/<provider>"
// (ADR-0042 section 7, docs/plans/active/plugins-wave-d.md).

// Caps of one provider contribution.
const (
	MaxProviderEnv       = 16
	MaxProviderSecrets   = 8
	MaxProviderModels    = 64
	MaxProviderModes     = 16
	MaxProviderArgv      = 32
	MaxProviderArgBytes  = 4 << 10
	maxProviderLabel     = 80
	maxProviderDesc      = 300
	maxProviderHealthTxt = 300
)

var (
	// providerPartRe is the <provider> part of the id; same grammar as a plugin id.
	providerPartRe  = idRe
	bareCommandRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	providerModelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@\[\]-]{0,99}$`)
	healthArgRe     = regexp.MustCompile(`^--?[a-z][a-z-]{0,30}$`)
	minVersionRe    = regexp.MustCompile(`^[0-9][0-9.]{0,31}$`)
)

// ReservedPluginIDs may not be plugin ids: "<id>/..." would match the
// reserved provider id prefixes (providers.ReservedID).
var ReservedPluginIDs = []string{"smind", "plugin"}

// ProviderContribution is an ACP provider a plugin adds. The registered id is
// "<plugin>/<ID>".
type ProviderContribution struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	Description string          `json:"description,omitempty"`
	Icon        string          `json:"icon,omitempty"` // Lucide name
	Harness     string          `json:"harness,omitempty"`
	ACP         ProviderACP     `json:"acp"`
	Models      []ProviderModel `json:"models,omitempty"`
	Modes       []ProviderMode  `json:"modes,omitempty"`
	DefaultMode string          `json:"defaultMode,omitempty"`
	Vendor      string          `json:"vendor,omitempty"`
	Health      ProviderHealth  `json:"health,omitempty"`

	// unknown lists the JSON paths of keys this daemon does not know,
	// relative to the entry; validation reports each one.
	unknown []string
}

// ProviderACP is how the agent is launched.
type ProviderACP struct {
	// Command is the argv: a path inside the plugin, or a bare program name.
	Command []string `json:"command"`
	// Env holds literal, non-secret variables.
	Env map[string]string `json:"env,omitempty"`
	// SecretEnv names the variables whose values the Human stores (never in
	// the manifest).
	SecretEnv []ProviderSecretSlot `json:"secretEnv,omitempty"`
	// InheritHome gives the agent the daemon user's real HOME instead of a
	// private one (consent shows it).
	InheritHome        bool  `json:"inheritHome,omitempty"`
	SupportsMcpServers *bool `json:"supportsMcpServers,omitempty"`
}

// ProviderSecretSlot is one secret environment variable of a provider.
type ProviderSecretSlot struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// ProviderModel is one entry of a provider's static model list.
type ProviderModel struct {
	ID          string `json:"id"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

// ProviderMode is one entry of a provider's static permission modes.
type ProviderMode struct {
	ID           string `json:"id"`
	Label        string `json:"label,omitempty"`
	Description  string `json:"description,omitempty"`
	AutoApproves bool   `json:"autoApproves,omitempty"`
}

// ProviderHealth is what provider.test says; text and a version check only.
type ProviderHealth struct {
	Install    string `json:"install,omitempty"`
	Login      string `json:"login,omitempty"`
	VersionArg string `json:"versionArg,omitempty"`
	MinVersion string `json:"minVersion,omitempty"`
}

// providerKeys are the keys each level accepts; anything else is reported.
var providerKeys = map[string][]string{
	"":       {"id", "label", "description", "icon", "harness", "acp", "models", "modes", "defaultMode", "vendor", "health"},
	"acp":    {"command", "env", "secretEnv", "inheritHome", "supportsMcpServers"},
	"health": {"install", "login", "versionArg", "minVersion"},
	"model":  {"id", "label", "description", "default"},
	"mode":   {"id", "label", "description", "autoApproves"},
	"secret": {"name", "label", "required"},
}

// notInV1 are keys with a specific "not in v1" explanation.
var notInV1 = map[string]string{
	"extends": "a plugin provider cannot extend another provider", "endpoint": "plugin providers have no endpoint overlay",
	"recipe": "plugin providers have no recipes", "usage": "plugin providers have no usage meter", "account": "plugin providers have no login flow (use health.login for text)",
	"args": "put arguments in acp.command", "cwd": "an agent always runs in the task worktree", "passthrough": "a plugin cannot read the daemon's environment",
}

func unknownKeys(raw map[string]json.RawMessage, level, prefix string) []string {
	known := map[string]bool{}
	for _, k := range providerKeys[level] {
		known[k] = true
	}
	var out []string
	for k := range raw {
		if !known[k] {
			out = append(out, prefix+k)
		}
	}
	sort.Strings(out)
	return out
}

// UnmarshalJSON decodes an entry and records unknown keys for validation
// (strictness inside provider objects without a decode error that would
// lose the field name).
func (p *ProviderContribution) UnmarshalJSON(data []byte) error {
	type plain ProviderContribution
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(data, &top)
	unk := unknownKeys(top, "", "")
	sub := func(key, level string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(top[key], &m)
		unk = append(unk, unknownKeys(m, level, key+".")...)
		return m
	}
	sub("acp", "acp")
	sub("health", "health")
	var accRaw map[string]json.RawMessage
	_ = json.Unmarshal(top["acp"], &accRaw)
	for _, kv := range []struct{ key, level string }{{"secretEnv", "secret"}} {
		var list []map[string]json.RawMessage
		_ = json.Unmarshal(accRaw[kv.key], &list)
		for i, m := range list {
			unk = append(unk, unknownKeys(m, kv.level, fmt.Sprintf("acp.%s[%d].", kv.key, i))...)
		}
	}
	for _, kv := range []struct{ key, level string }{{"models", "model"}, {"modes", "mode"}} {
		var list []map[string]json.RawMessage
		_ = json.Unmarshal(top[kv.key], &list)
		for i, m := range list {
			unk = append(unk, unknownKeys(m, kv.level, fmt.Sprintf("%s[%d].", kv.key, i))...)
		}
	}
	*p = ProviderContribution(v)
	p.unknown = unk
	return nil
}

// ProviderID is the registered id of a plugin's provider.
func ProviderID(plugin, provider string) string { return plugin + "/" + provider }

// commandForm classifies argv[0]: "path" (a path inside the plugin) or
// "bare" (a name looked up on the fixed PATH); problem is non-empty when it
// is neither.
func commandForm(argv0 string) (form, problem string) {
	switch {
	case strings.TrimSpace(argv0) == "":
		return "", "is empty"
	case strings.ContainsAny(argv0, "/\\") || driveRe.MatchString(argv0):
		if strings.HasPrefix(argv0, "/") || strings.HasPrefix(argv0, `\`) || driveRe.MatchString(argv0) {
			return "", "command_not_allowed: an absolute path outside the plugin is refused"
		}
		if err := ConfinedPath(argv0); err != nil {
			return "", "command_not_allowed: " + err.Error()
		}
		return "path", ""
	case !bareCommandRe.MatchString(argv0):
		return "", "is not a plain program name"
	}
	return "bare", ""
}

func validateProvider(v *manifestVal, f string, p ProviderContribution, earlier []ProviderContribution) {
	if !providerPartRe.MatchString(p.ID) || windowsReserved[p.ID] {
		v.bad(f+".id", fmt.Sprintf("%q is not a valid provider id", p.ID), "use 1-64 characters from a-z, 0-9 and -, starting with a letter; the registered id is \"<plugin>/<id>\"")
	}
	for _, e := range earlier {
		if e.ID == p.ID {
			v.bad(f+".id", fmt.Sprintf("duplicate provider %q", p.ID), "declare each provider once")
		}
	}
	for _, k := range p.unknown {
		first := strings.SplitN(k, ".", 2)[0]
		why, ok := notInV1[k]
		if !ok && strings.HasPrefix(k, "acp.") {
			why, ok = notInV1[strings.TrimPrefix(k, "acp.")]
		}
		if ok {
			v.bad(f+"."+k, "is not supported: "+why, "remove "+k)
		} else {
			v.bad(f+"."+first, fmt.Sprintf("unknown key %q", k), "remove it or fix the spelling")
		}
	}
	if strings.TrimSpace(p.Label) == "" || len([]rune(p.Label)) > maxProviderLabel || hasControl(p.Label) {
		v.bad(f+".label", "must be 1 to 80 characters of plain text", "set a short label")
	}
	if len([]rune(p.Description)) > maxProviderDesc || hasControl(p.Description) {
		v.bad(f+".description", "must be at most 300 characters of plain text", "shorten description")
	}
	if p.Icon != "" && !iconRe.MatchString(p.Icon) {
		v.bad(f+".icon", fmt.Sprintf("%q is not a Lucide icon name", p.Icon), "use a PascalCase name from the client's Lucide registry, like \"Bot\"")
	}
	if p.Harness != "" && p.Harness != "acp" {
		v.bad(f+".harness", fmt.Sprintf("%q is not supported", p.Harness), `plugin providers run through ACP: use "acp" or omit harness`)
	}
	if !providers.ValidVendor(p.Vendor) {
		v.bad(f+".vendor", fmt.Sprintf("%q is not a vendor profile", p.Vendor), "use generic, zai, anthropic or openai")
	}
	validateProviderCommand(v, f+".acp.command", p.ACP.Command)

	if len(p.ACP.Env) > MaxProviderEnv {
		v.bad(f+".acp.env", fmt.Sprintf("has %d entries (the limit is %d)", len(p.ACP.Env), MaxProviderEnv), "keep only the variables the agent needs")
	}
	envKeys := make([]string, 0, len(p.ACP.Env))
	for k := range p.ACP.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		val := p.ACP.Env[k]
		if why := refuseEnvName(k, false); why != "" {
			v.bad(f+".acp.env."+k, why, "use a plain name; credentials go in acp.secretEnv")
		} else if len(val) > MaxHookEnvValueBytes || strings.IndexFunc(val, func(r rune) bool { return r < 0x20 && r != '\t' }) >= 0 {
			v.bad(f+".acp.env."+k, "value is over 4 KiB or holds a control character", "shorten the value")
		}
	}
	if len(p.ACP.SecretEnv) > MaxProviderSecrets {
		v.bad(f+".acp.secretEnv", fmt.Sprintf("has %d entries (the limit is %d)", len(p.ACP.SecretEnv), MaxProviderSecrets), "keep only the secrets the agent needs")
	}
	seen := map[string]bool{}
	for i, s := range p.ACP.SecretEnv {
		sf := fmt.Sprintf("%s.acp.secretEnv[%d]", f, i)
		if why := refuseEnvName(s.Name, true); why != "" {
			v.bad(sf+".name", why, "use a plain upper-case variable name")
		}
		if seen[s.Name] {
			v.bad(sf+".name", fmt.Sprintf("duplicate secret %q", s.Name), "declare each secret once")
		}
		seen[s.Name] = true
		if _, dup := p.ACP.Env[s.Name]; dup {
			v.bad(sf+".name", fmt.Sprintf("%s is also in acp.env", s.Name), "a variable is either a literal or a secret")
		}
		if len([]rune(s.Label)) > maxProviderLabel || hasControl(s.Label) {
			v.bad(sf+".label", "must be at most 80 characters of plain text", "shorten the label")
		}
	}

	if len(p.Models) > MaxProviderModels {
		v.bad(f+".models", fmt.Sprintf("has %d entries (the limit is %d)", len(p.Models), MaxProviderModels), "trim the static list; ACP discovery adds the agent's own")
	}
	mids, defaults := map[string]bool{}, 0
	for i, m := range p.Models {
		mf := fmt.Sprintf("%s.models[%d]", f, i)
		if !providerModelRe.MatchString(m.ID) {
			v.bad(mf+".id", fmt.Sprintf("%q is not a valid model id", m.ID), "use 1-100 characters from letters, digits and . _ : / @ [ ] -, not starting with -")
		}
		if mids[m.ID] {
			v.bad(mf+".id", fmt.Sprintf("duplicate model %q", m.ID), "declare each model once")
		}
		mids[m.ID] = true
		if len([]rune(m.Label)) > maxProviderLabel || len([]rune(m.Description)) > maxProviderDesc || hasControl(m.Label) || hasControl(m.Description) {
			v.bad(mf, "label or description is too long or has a control character", "keep label at most 80 and description at most 300 characters")
		}
		if m.Default {
			defaults++
		}
	}
	if defaults > 1 {
		v.bad(f+".models", "more than one model is marked default", "mark at most one")
	}
	if len(p.Modes) > MaxProviderModes {
		v.bad(f+".modes", fmt.Sprintf("has %d entries (the limit is %d)", len(p.Modes), MaxProviderModes), "trim the list")
	}
	modeIDs := map[string]bool{}
	for i, m := range p.Modes {
		mf := fmt.Sprintf("%s.modes[%d]", f, i)
		if strings.TrimSpace(m.ID) == "" || len(m.ID) > 64 || hasControl(m.ID) {
			v.bad(mf+".id", "must be 1 to 64 characters", "set the agent's mode id")
		}
		if modeIDs[m.ID] {
			v.bad(mf+".id", fmt.Sprintf("duplicate mode %q", m.ID), "declare each mode once")
		}
		modeIDs[m.ID] = true
		if len([]rune(m.Label)) > maxProviderLabel || len([]rune(m.Description)) > maxProviderDesc || hasControl(m.Label) || hasControl(m.Description) {
			v.bad(mf, "label or description is too long or has a control character", "keep label at most 80 and description at most 300 characters")
		}
	}
	if p.DefaultMode != "" && !modeIDs[p.DefaultMode] {
		v.bad(f+".defaultMode", fmt.Sprintf("%q is not one of the declared modes", p.DefaultMode), "name a declared mode or remove defaultMode")
	}

	h := p.Health
	if len([]rune(h.Install)) > maxProviderHealthTxt || len([]rune(h.Login)) > maxProviderHealthTxt || hasControl(h.Install) || hasControl(h.Login) {
		v.bad(f+".health", "install and login must be at most 300 characters of plain text", "shorten the text")
	}
	if h.VersionArg != "" && !healthArgRe.MatchString(h.VersionArg) {
		v.bad(f+".health.versionArg", fmt.Sprintf("%q is not a flag", h.VersionArg), `use a flag like "--version"`)
	}
	if h.MinVersion != "" && (!minVersionRe.MatchString(h.MinVersion) || h.VersionArg == "") {
		v.bad(f+".health.minVersion", "needs a dotted number and a versionArg", `set both, for example "1.2.0" with "--version"`)
	}
}

func validateProviderCommand(v *manifestVal, field string, cmd []string) {
	if len(cmd) == 0 || len(cmd) > MaxProviderArgv {
		v.bad(field, "must be a non-empty argv array of at most 32 entries", `write it as ["program", "arg", ...], never as a shell string`)
		return
	}
	for i, a := range cmd {
		if strings.ContainsAny(a, "\x00\n\r") || len(a) > MaxProviderArgBytes {
			v.bad(fmt.Sprintf("%s[%d]", field, i), "is too long or contains a NUL or newline", "give each argument as a plain string")
		}
	}
	if _, why := commandForm(cmd[0]); why != "" {
		v.bad(field+"[0]", why, "use a file inside the plugin (\"bin/agent\") or a program name looked up on the system PATH (\"opencode\")")
	}
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
