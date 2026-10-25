package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
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
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Command   []string `json:"command,omitempty"`
	URL       string   `json:"url,omitempty"`
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
		c.McpServers = append(c.McpServers, McpEntry{Name: s.Name, Transport: s.Transport, Command: s.Command, URL: s.URL})
	}
	sort.Slice(c.McpServers, func(i, j int) bool { return c.McpServers[i].Name < c.McpServers[j].Name })
	for _, p := range m.Contributes.Providers {
		c.AcpProviders = append(c.AcpProviders, AcpEntry{ID: p.ID, Command: p.ACP.Command})
	}
	sort.Slice(c.AcpProviders, func(i, j int) bool { return c.AcpProviders[i].ID < c.AcpProviders[j].ID })

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
	b, _ := json.Marshal(struct {
		Process      []string
		Capabilities []string
		HookEvents   []HookEvent
		McpServers   []McpEntry
		AcpProviders []AcpEntry
	}{c.Process, c.Capabilities, c.HookEvents, c.McpServers, c.AcpProviders})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
