package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Consent is what the Human is shown before enabling a plugin (ADR-0042
// section 2): what would run, what it may call, where it came from.
type Consent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	SourceKind  string `json:"sourceKind"`
	Hash        string `json:"hash"`
	// DataOnly is true when nothing the plugin declares runs code: no
	// process, no MCP/ACP command and no hook.
	DataOnly     bool        `json:"dataOnly"`
	Process      []string    `json:"process,omitempty"`
	Capabilities []string    `json:"capabilities,omitempty"`
	HookEvents   []HookEvent `json:"hookEvents,omitempty"`
	McpServers   []McpEntry  `json:"mcpServers,omitempty"`
	AcpProviders []AcpEntry  `json:"acpProviders,omitempty"`
	// Skills and Profiles list the data contributions (ADR-0042 section 6).
	// A profile that raises the permission mode, bypasses prompts, enables
	// orchestration tools or may launch children carries Escalations, and
	// only those profiles are part of the fingerprint.
	Skills   []string       `json:"skills,omitempty"`
	Profiles []ProfileEntry `json:"profiles,omitempty"`
	// LinkWarning is set for a linked source: its files are read live from the
	// developer's directory, so changes after consent are not re-reviewed.
	LinkWarning string `json:"linkWarning,omitempty"`
	// Fingerprint covers exactly the fields above that grant or run
	// something (process, capabilities, hooks, MCP and ACP commands). An
	// update that changes it needs re-consent; one that does not keeps the
	// existing consent.
	Fingerprint string `json:"fingerprint"`
}

// HookEvent is a hook a plugin registers.
type HookEvent struct {
	Event   string `json:"event"`
	OnError string `json:"onError,omitempty"`
}

// McpEntry is an MCP server a plugin contributes.
type McpEntry struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   []string          `json:"command,omitempty"`
	URL       string            `json:"url,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// ProfileEntry summarizes a profile a plugin contributes.
type ProfileEntry struct {
	Name               string   `json:"name"`
	Provider           string   `json:"provider"`
	PermissionMode     string   `json:"permissionMode,omitempty"`
	AutoAccept         bool     `json:"autoAccept,omitempty"`
	OrchestrationTools bool     `json:"orchestrationTools,omitempty"`
	ChildPolicy        string   `json:"childPolicy,omitempty"`
	Escalations        []string `json:"escalations,omitempty"`
}

// LinkWarningText is shown wherever a linked plugin is enabled.
const LinkWarningText = "live directory: changes after consent are not re-reviewed"

// calmModes are permission modes that do not raise the user's default; any
// other mode (including an ACP agent's own ids) is flagged.
var calmModes = map[string]bool{"": true, "default": true, "plan": true, "read-only": true}

// bypassHints mark a mode id that reads as skipping permission prompts.
var bypassHints = []string{"bypass", "yolo", "full", "auto", "dangerous", "skip"}

// profileEscalations lists why a contributed profile needs the Human's
// attention: a permission mode above the default, prompts bypassed,
// orchestration tools, or the right to launch child agents.
func profileEscalations(p ProfileSpec) []string {
	var out []string
	if !calmModes[p.PermissionMode] {
		why := fmt.Sprintf("permission mode %q is above the default", p.PermissionMode)
		lower := strings.ToLower(p.PermissionMode)
		for _, h := range bypassHints {
			if strings.Contains(lower, h) {
				why = fmt.Sprintf("permission mode %q skips permission prompts", p.PermissionMode)
				break
			}
		}
		out = append(out, why)
	}
	if p.AutoAccept {
		out = append(out, "auto-accepts every permission request")
	}
	if p.OrchestrationTools != nil && *p.OrchestrationTools {
		out = append(out, "has orchestration tools (can start and message other agents)")
	}
	if p.ChildPolicy == "leaf" || p.ChildPolicy == "profiles" {
		out = append(out, "may launch child agents (childPolicy "+p.ChildPolicy+")")
	}
	return out
}

// AcpEntry is an ACP provider command a plugin contributes.
type AcpEntry struct {
	ID      string   `json:"id"`
	Command []string `json:"command"`
}

// BuildConsent derives the consent summary of a validated manifest.
func BuildConsent(m *Manifest, source Source, hash string) Consent {
	c := Consent{
		ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description,
		Source: source.String(), SourceKind: string(source.Kind), Hash: hash,
	}
	if m.Process != nil {
		c.Process = append([]string(nil), m.Process.Command...)
	}
	c.Capabilities = append([]string(nil), m.Capabilities...)
	sort.Strings(c.Capabilities)
	for _, h := range m.Contributes.Hooks {
		oe := h.OnError
		if oe == "" {
			oe = "open"
		}
		c.HookEvents = append(c.HookEvents, HookEvent{Event: h.Event, OnError: oe})
	}
	sort.Slice(c.HookEvents, func(i, j int) bool { return c.HookEvents[i].Event < c.HookEvents[j].Event })
	for _, s := range m.Contributes.McpServers {
		c.McpServers = append(c.McpServers, McpEntry{Name: s.Name, Transport: s.Transport, Command: s.Command, URL: s.URL, Env: s.Env, Headers: s.Headers})
	}
	sort.Slice(c.McpServers, func(i, j int) bool { return c.McpServers[i].Name < c.McpServers[j].Name })
	for _, p := range m.Contributes.Providers {
		c.AcpProviders = append(c.AcpProviders, AcpEntry{ID: p.ID, Command: p.ACP.Command})
	}
	sort.Slice(c.AcpProviders, func(i, j int) bool { return c.AcpProviders[i].ID < c.AcpProviders[j].ID })

	for _, sk := range m.skills {
		c.Skills = append(c.Skills, sk.Name)
	}
	for _, p := range m.profiles {
		e := ProfileEntry{Name: p.Name, Provider: p.Provider, PermissionMode: p.PermissionMode, AutoAccept: p.AutoAccept, ChildPolicy: p.ChildPolicy, Escalations: profileEscalations(p)}
		e.OrchestrationTools = p.OrchestrationTools != nil && *p.OrchestrationTools
		c.Profiles = append(c.Profiles, e)
	}
	if source.Kind == SourceLink {
		c.LinkWarning = LinkWarningText
	}

	c.DataOnly = c.Process == nil && len(c.HookEvents) == 0 && len(c.AcpProviders) == 0
	for _, s := range c.McpServers {
		if s.Transport == "stdio" {
			c.DataOnly = false
		}
	}
	c.Fingerprint = fingerprint(c)
	return c
}

func fingerprint(c Consent) string {
	var flagged []ProfileEntry
	for _, p := range c.Profiles {
		if len(p.Escalations) > 0 {
			flagged = append(flagged, p)
		}
	}
	b, _ := json.Marshal(struct {
		Process      []string
		Capabilities []string
		HookEvents   []HookEvent
		McpServers   []McpEntry
		AcpProviders []AcpEntry
		Escalating   []ProfileEntry `json:",omitempty"`
	}{c.Process, c.Capabilities, c.HookEvents, c.McpServers, c.AcpProviders, flagged})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
